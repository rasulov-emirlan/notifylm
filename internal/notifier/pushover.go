package notifier

import (
	"fmt"
	"log/slog"

	"github.com/gregdel/pushover"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/message"
)

// Notifier sends push notifications.
type Notifier interface {
	// Notify sends a push notification for the given message.
	Notify(msg *message.Message) error
}

// PushoverNotifier sends notifications via Pushover.
type PushoverNotifier struct {
	app       *pushover.Pushover
	recipient *pushover.Recipient
}

// NewPushoverNotifier creates a new Pushover notifier.
func NewPushoverNotifier(cfg config.PushoverConfig) *PushoverNotifier {
	return &PushoverNotifier{
		app:       pushover.New(cfg.AppToken),
		recipient: pushover.NewRecipient(cfg.UserToken),
	}
}

// Notify sends a push notification for an urgent message.
func (p *PushoverNotifier) Notify(msg *message.Message) error {
	title := formatTitle(msg)
	body := formatBody(msg)

	notification := &pushover.Message{
		Title:    title,
		Message:  body,
		Priority: pushover.PriorityHigh,
		Sound:    pushover.SoundPersistent,
	}

	// Add URL for context if applicable
	if url := getMessageURL(msg); url != "" {
		notification.URL = url
		notification.URLTitle = "Open in app"
	}

	response, err := p.app.SendMessage(notification, p.recipient)
	if err != nil {
		return fmt.Errorf("failed to send pushover notification: %w", err)
	}

	slog.Info("Pushover notification sent",
		"source", msg.Source,
		"sender", msg.Sender,
		"status", response.Status)

	return nil
}

func formatTitle(msg *message.Message) string {
	icon := getSourceIcon(msg.Source)
	return fmt.Sprintf("%s %s: %s", icon, msg.Source, msg.Sender)
}

func formatBody(msg *message.Message) string {
	text := msg.Text
	if len(text) > 500 {
		text = text[:497] + "..."
	}
	return text
}

func getSourceIcon(source message.Source) string {
	switch source {
	case message.SourceWhatsApp:
		return "💬"
	case message.SourceTelegram:
		return "✈️"
	case message.SourceSlack:
		return "🔔"
	case message.SourceGmail:
		return "📧"
	default:
		return "📨"
	}
}

func getMessageURL(msg *message.Message) string {
	switch msg.Source {
	case message.SourceSlack:
		// Could construct a Slack deep link
		return ""
	case message.SourceGmail:
		if msg.ID != "" {
			return fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", msg.ID)
		}
	}
	return ""
}

// MockNotifier is a notifier that just logs instead of sending.
type MockNotifier struct{}

func NewMockNotifier() *MockNotifier {
	return &MockNotifier{}
}

func (m *MockNotifier) Notify(msg *message.Message) error {
	slog.Info("MOCK NOTIFICATION",
		"source", msg.Source,
		"sender", msg.Sender,
		"text", truncate(msg.Text, 100))
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
