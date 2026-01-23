package listener

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/message"

	_ "github.com/mattn/go-sqlite3"
)

// WhatsAppListener implements the Listener interface for WhatsApp.
type WhatsAppListener struct {
	BaseListener
	cfg    config.WhatsAppConfig
	client *whatsmeow.Client
	out    chan<- *message.Message
}

// NewWhatsAppListener creates a new WhatsApp listener.
func NewWhatsAppListener(cfg config.WhatsAppConfig) *WhatsAppListener {
	return &WhatsAppListener{
		BaseListener: NewBaseListener("whatsapp"),
		cfg:          cfg,
	}
}

func (w *WhatsAppListener) Start(ctx context.Context, out chan<- *message.Message) error {
	w.out = out

	// Ensure storage directory exists
	if err := os.MkdirAll(w.cfg.StoragePath, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Initialize SQLite store for session data
	dbPath := fmt.Sprintf("file:%s/whatsapp.db?_foreign_keys=on", w.cfg.StoragePath)
	container, err := sqlstore.New(ctx, "sqlite3", dbPath, waLog.Noop)
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	// Get or create device store
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get device: %w", err)
	}

	// Create WhatsApp client
	w.client = whatsmeow.NewClient(device, waLog.Noop)

	// Register event handler
	w.client.AddEventHandler(w.handleEvent)

	// Connect (or show QR code for linking)
	if w.client.Store.ID == nil {
		// Not logged in, need to link as new device
		qrChan, _ := w.client.GetQRChannel(ctx)
		if err := w.client.Connect(); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				slog.Info("WhatsApp QR code (scan with phone)", "qr", evt.Code)
				fmt.Println("WhatsApp QR Code:")
				fmt.Println(evt.Code)
			} else {
				slog.Info("WhatsApp login event", "event", evt.Event)
			}
		}
	} else {
		if err := w.client.Connect(); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	slog.Info("WhatsApp listener started")

	// Block until context is cancelled
	<-ctx.Done()
	return ctx.Err()
}

func (w *WhatsAppListener) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		w.handleMessage(v)
	}
}

func (w *WhatsAppListener) handleMessage(evt *events.Message) {
	var text string
	if msg := evt.Message; msg != nil {
		text = extractWhatsAppText(msg)
	}

	if text == "" {
		return
	}

	sender := evt.Info.Sender.User
	if evt.Info.PushName != "" {
		sender = evt.Info.PushName
	}

	msg := message.NewMessage(message.SourceWhatsApp, sender, text)
	msg.ID = evt.Info.ID
	msg.Timestamp = evt.Info.Timestamp
	msg.Metadata["chat_id"] = evt.Info.Chat.String()
	msg.Metadata["is_group"] = fmt.Sprintf("%v", evt.Info.IsGroup)

	w.out <- msg
}

func extractWhatsAppText(msg *waE2E.Message) string {
	if msg.Conversation != nil {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	return ""
}

func (w *WhatsAppListener) Stop() error {
	if w.client != nil {
		w.client.Disconnect()
	}
	return nil
}
