package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/emirlan/notifylm/internal/calendar"
	"github.com/emirlan/notifylm/internal/classifier"
	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/listener"
	"github.com/emirlan/notifylm/internal/message"
	"github.com/emirlan/notifylm/internal/notifier"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	debug := flag.Bool("debug", false, "Enable debug logging")
	dryRun := flag.Bool("dry-run", false, "Don't send actual notifications")
	flag.Parse()

	// Setup logging
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Warn("Failed to load config, using defaults", "error", err)
		cfg = config.DefaultConfig()
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		slog.Info("Received shutdown signal", "signal", sig)
		cancel()
	}()

	// Create central message channel
	messageChan := make(chan *message.Message, 100)

	// Initialize listeners
	listeners := initializeListeners(cfg)

	// Initialize classifier
	msgClassifier := classifier.NewLLMClassifier(cfg.LLM)

	// Initialize notifier
	var msgNotifier notifier.Notifier
	if *dryRun {
		msgNotifier = notifier.NewMockNotifier()
		slog.Info("Running in dry-run mode - notifications will be logged only")
	} else {
		msgNotifier = notifier.NewPushoverNotifier(cfg.Pushover)
	}

	// Initialize calendar event creator
	var calendarCreator calendar.EventCreator
	if cfg.Calendar.Enabled {
		if *dryRun {
			calendarCreator = calendar.NewMockCalendarCreator()
			slog.Info("Calendar: using mock creator (dry-run mode)")
		} else {
			gc, err := calendar.NewGoogleCalendarCreator(ctx, cfg.Calendar)
			if err != nil {
				slog.Error("Failed to initialize Google Calendar, disabling", "error", err)
			} else {
				calendarCreator = gc
				slog.Info("Google Calendar integration enabled")
			}
		}
	}

	// Start message processor
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		processMessages(ctx, messageChan, msgClassifier, msgNotifier, calendarCreator)
	}()

	// Start all listeners concurrently
	var listenerWg sync.WaitGroup
	for _, l := range listeners {
		listenerWg.Add(1)
		go func(lst listener.Listener) {
			defer listenerWg.Done()
			slog.Info("Starting listener", "name", lst.Name())
			if err := lst.Start(ctx, messageChan); err != nil {
				if ctx.Err() == nil {
					slog.Error("Listener failed", "name", lst.Name(), "error", err)
				}
			}
			slog.Info("Listener stopped", "name", lst.Name())
		}(l)
	}

	slog.Info("Unified Notification Interceptor started",
		"listeners", len(listeners))

	// Wait for all listeners to stop
	listenerWg.Wait()

	// Close message channel and wait for processor
	close(messageChan)
	wg.Wait()

	// Stop all listeners
	for _, l := range listeners {
		if err := l.Stop(); err != nil {
			slog.Warn("Failed to stop listener", "name", l.Name(), "error", err)
		}
	}

	slog.Info("Shutdown complete")
}

func initializeListeners(cfg *config.Config) []listener.Listener {
	var listeners []listener.Listener

	if cfg.WhatsApp.Enabled {
		listeners = append(listeners, listener.NewWhatsAppListener(cfg.WhatsApp))
	}

	if cfg.Telegram.Enabled {
		listeners = append(listeners, listener.NewTelegramListener(cfg.Telegram))
	}

	if cfg.Slack.Enabled {
		listeners = append(listeners, listener.NewSlackListener(cfg.Slack))
	}

	if cfg.Gmail.Enabled {
		listeners = append(listeners, listener.NewGmailListener(cfg.Gmail))
	}

	return listeners
}

func processMessages(
	ctx context.Context,
	messages <-chan *message.Message,
	cls classifier.Classifier,
	notify notifier.Notifier,
	cal calendar.EventCreator,
) {
	for {
		select {
		case <-ctx.Done():
			// Drain remaining messages
			for msg := range messages {
				handleMessage(ctx, msg, cls, notify, cal)
			}
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			handleMessage(ctx, msg, cls, notify, cal)
		}
	}
}

func handleMessage(
	ctx context.Context,
	msg *message.Message,
	cls classifier.Classifier,
	notify notifier.Notifier,
	cal calendar.EventCreator,
) {
	slog.Debug("Received message",
		"source", msg.Source,
		"sender", msg.Sender,
		"text_length", len(msg.Text))

	// Classify message urgency and extract action items
	result, err := cls.ClassifyMessage(ctx, msg)
	if err != nil {
		slog.Error("Classification failed",
			"source", msg.Source,
			"error", err)
		return
	}

	// Handle urgency notification
	if result.IsUrgent {
		slog.Info("Urgent message detected",
			"source", msg.Source,
			"sender", msg.Sender)

		if err := notify.Notify(msg); err != nil {
			slog.Error("Failed to send urgency notification",
				"source", msg.Source,
				"error", err)
		}
	}

	// Handle action items
	for _, item := range result.ActionItems {
		slog.Info("Action item detected",
			"title", item.Title,
			"datetime", item.DateTime.Format(time.RFC3339),
			"source", msg.Source,
			"sender", msg.Sender)

		// Send action item notification via Pushover
		actionMsg := &message.Message{
			ID:        msg.ID,
			Source:    msg.Source,
			Sender:    msg.Sender,
			Text:      fmt.Sprintf("Action: %s\nDue: %s\n\n%s", item.Title, item.DateTime.Format("Jan 2, 2006 3:04 PM"), item.Description),
			Timestamp: msg.Timestamp,
			Metadata:  msg.Metadata,
		}
		if err := notify.Notify(actionMsg); err != nil {
			slog.Error("Failed to send action item notification",
				"title", item.Title,
				"error", err)
		}

		// Create calendar event
		if cal != nil {
			if err := cal.CreateEvent(ctx, &item, msg); err != nil {
				slog.Error("Failed to create calendar event",
					"title", item.Title,
					"error", err)
			}
		}
	}

	if !result.IsUrgent && len(result.ActionItems) == 0 {
		slog.Debug("Message classified as not urgent, no action items",
			"source", msg.Source,
			"sender", msg.Sender)
	}
}
