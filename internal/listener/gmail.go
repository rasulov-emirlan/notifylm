package listener

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/googleauth"
	"github.com/emirlan/notifylm/internal/message"
)

// GmailListener implements the Listener interface for Gmail.
type GmailListener struct {
	BaseListener
	cfg           config.GmailConfig
	service       *gmail.Service
	out           chan<- *message.Message
	lastHistoryID uint64
}

// NewGmailListener creates a new Gmail listener.
func NewGmailListener(cfg config.GmailConfig) *GmailListener {
	return &GmailListener{
		BaseListener: NewBaseListener("gmail"),
		cfg:          cfg,
	}
}

func (g *GmailListener) Start(ctx context.Context, out chan<- *message.Message) error {
	g.out = out

	// Get authenticated client via shared OAuth2 helper
	client, err := googleauth.GetOAuth2Client(ctx, g.cfg.CredentialsPath, g.cfg.TokenPath, gmail.GmailReadonlyScope)
	if err != nil {
		return fmt.Errorf("failed to get Gmail OAuth2 client: %w", err)
	}

	// Create Gmail service
	g.service, err = gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("failed to create Gmail service: %w", err)
	}

	// Get initial history ID
	profile, err := g.service.Users.GetProfile("me").Do()
	if err != nil {
		return fmt.Errorf("failed to get profile: %w", err)
	}
	g.lastHistoryID = profile.HistoryId

	slog.Info("Gmail listener started", "email", profile.EmailAddress)

	// Poll for new messages
	pollInterval := time.Duration(g.cfg.PollInterval) * time.Second
	if pollInterval == 0 {
		pollInterval = 60 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := g.pollNewMessages(ctx); err != nil {
				slog.Warn("Failed to poll Gmail", "error", err)
			}
		}
	}
}

func (g *GmailListener) pollNewMessages(ctx context.Context) error {
	// Get history since last check
	history, err := g.service.Users.History.List("me").
		StartHistoryId(g.lastHistoryID).
		HistoryTypes("messageAdded").
		Do()
	if err != nil {
		return fmt.Errorf("failed to list history: %w", err)
	}

	// Update history ID
	if history.HistoryId > g.lastHistoryID {
		g.lastHistoryID = history.HistoryId
	}

	// Process new messages
	for _, h := range history.History {
		for _, added := range h.MessagesAdded {
			if err := g.processMessage(ctx, added.Message.Id); err != nil {
				slog.Warn("Failed to process Gmail message",
					"message_id", added.Message.Id,
					"error", err)
			}
		}
	}

	return nil
}

func (g *GmailListener) processMessage(ctx context.Context, messageID string) error {
	// Fetch full message
	msg, err := g.service.Users.Messages.Get("me", messageID).
		Format("full").
		Do()
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}

	// Skip sent messages
	for _, label := range msg.LabelIds {
		if label == "SENT" {
			return nil
		}
	}

	// Extract headers
	var from, subject string
	for _, header := range msg.Payload.Headers {
		switch header.Name {
		case "From":
			from = header.Value
		case "Subject":
			subject = header.Value
		}
	}

	// Extract body
	body := extractGmailBody(msg.Payload)

	// Create unified message
	text := subject
	if body != "" {
		text = fmt.Sprintf("Subject: %s\n\n%s", subject, body)
	}

	m := message.NewMessage(message.SourceGmail, from, text)
	m.ID = messageID
	m.Timestamp = time.UnixMilli(msg.InternalDate)
	m.Metadata["subject"] = subject
	m.Metadata["labels"] = fmt.Sprintf("%v", msg.LabelIds)

	g.out <- m
	return nil
}

func extractGmailBody(payload *gmail.MessagePart) string {
	// Try to get plain text body
	if payload.MimeType == "text/plain" && payload.Body.Data != "" {
		data, err := base64.URLEncoding.DecodeString(payload.Body.Data)
		if err == nil {
			return string(data)
		}
	}

	// Check parts recursively
	for _, part := range payload.Parts {
		if body := extractGmailBody(part); body != "" {
			return body
		}
	}

	return ""
}

func (g *GmailListener) Stop() error {
	// Polling cleanup is handled by context cancellation
	return nil
}
