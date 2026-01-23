package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

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

	// Start message processor
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		processMessages(ctx, messageChan, msgClassifier, msgNotifier)
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
) {
	for {
		select {
		case <-ctx.Done():
			// Drain remaining messages
			for msg := range messages {
				handleMessage(ctx, msg, cls, notify)
			}
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			handleMessage(ctx, msg, cls, notify)
		}
	}
}

func handleMessage(
	ctx context.Context,
	msg *message.Message,
	cls classifier.Classifier,
	notify notifier.Notifier,
) {
	slog.Debug("Received message",
		"source", msg.Source,
		"sender", msg.Sender,
		"text_length", len(msg.Text))

	// Classify message urgency
	isUrgent, err := cls.ClassifyMessage(ctx, msg)
	if err != nil {
		slog.Error("Classification failed",
			"source", msg.Source,
			"error", err)
		return
	}

	if !isUrgent {
		slog.Debug("Message classified as not urgent",
			"source", msg.Source,
			"sender", msg.Sender)
		return
	}

	slog.Info("Urgent message detected",
		"source", msg.Source,
		"sender", msg.Sender)

	// Send notification
	if err := notify.Notify(msg); err != nil {
		slog.Error("Failed to send notification",
			"source", msg.Source,
			"error", err)
	}
}
