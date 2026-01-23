package classifier

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/message"
)

// Classifier determines if messages are urgent/important.
type Classifier interface {
	// ClassifyMessage returns true if the message is urgent/important.
	ClassifyMessage(ctx context.Context, msg *message.Message) (bool, error)
}

// LLMClassifier uses an LLM to classify message urgency.
type LLMClassifier struct {
	cfg config.LLMConfig
}

// NewLLMClassifier creates a new LLM-based classifier.
func NewLLMClassifier(cfg config.LLMConfig) *LLMClassifier {
	return &LLMClassifier{cfg: cfg}
}

// ClassifyMessage sends the message to an LLM for classification.
// This is a mock implementation - replace with actual API calls.
func (c *LLMClassifier) ClassifyMessage(ctx context.Context, msg *message.Message) (bool, error) {
	// Build prompt for LLM
	prompt := buildClassificationPrompt(msg)

	slog.Debug("Classifying message",
		"source", msg.Source,
		"sender", msg.Sender,
		"text_preview", truncate(msg.Text, 50))

	// TODO: Replace with actual LLM API call
	// This mock implementation uses simple heuristics
	result, err := c.mockLLMCall(ctx, prompt)
	if err != nil {
		return false, fmt.Errorf("LLM classification failed: %w", err)
	}

	return result, nil
}

func buildClassificationPrompt(msg *message.Message) string {
	return fmt.Sprintf(`Analyze this message and determine if it requires immediate attention.

Source: %s
Sender: %s
Timestamp: %s
Message: %s

Respond with only "URGENT" or "NOT_URGENT".

Consider these factors:
- Emergency keywords (urgent, ASAP, emergency, critical, help)
- Time-sensitive requests
- Important people (based on context)
- Financial or security matters
- Health-related concerns

Classification:`,
		msg.Source,
		msg.Sender,
		msg.Timestamp.Format(time.RFC3339),
		msg.Text,
	)
}

// mockLLMCall simulates an LLM API call for classification.
// Replace this with actual OpenAI/Gemini API integration.
func (c *LLMClassifier) mockLLMCall(ctx context.Context, prompt string) (bool, error) {
	// Simulate API latency
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}

	// Simple heuristic-based mock classification
	text := strings.ToLower(prompt)

	urgentKeywords := []string{
		"urgent", "asap", "emergency", "critical", "important",
		"help", "immediately", "now", "deadline", "alert",
		"security", "breach", "down", "broken", "failed",
		"payment", "money", "transfer", "invoice",
		"call me", "call asap", "need you",
	}

	for _, keyword := range urgentKeywords {
		if strings.Contains(text, keyword) {
			slog.Info("Message classified as URGENT",
				"keyword_matched", keyword)
			return true, nil
		}
	}

	return false, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RealLLMClassifier provides actual LLM integration.
// Uncomment and implement when ready to use real API.
/*
type RealLLMClassifier struct {
	cfg    config.LLMConfig
	client *http.Client
}

func NewRealLLMClassifier(cfg config.LLMConfig) *RealLLMClassifier {
	return &RealLLMClassifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *RealLLMClassifier) ClassifyMessage(ctx context.Context, msg *message.Message) (bool, error) {
	prompt := buildClassificationPrompt(msg)

	switch c.cfg.Provider {
	case "openai":
		return c.callOpenAI(ctx, prompt)
	case "gemini":
		return c.callGemini(ctx, prompt)
	default:
		return false, fmt.Errorf("unknown LLM provider: %s", c.cfg.Provider)
	}
}

func (c *RealLLMClassifier) callOpenAI(ctx context.Context, prompt string) (bool, error) {
	// Implement OpenAI API call
	// POST https://api.openai.com/v1/chat/completions
	return false, nil
}

func (c *RealLLMClassifier) callGemini(ctx context.Context, prompt string) (bool, error) {
	// Implement Gemini API call
	// POST https://generativelanguage.googleapis.com/v1/models/{model}:generateContent
	return false, nil
}
*/
