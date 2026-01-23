package classifier

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

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
	cfg    config.LLMConfig
	client openai.Client
	hasLLM bool
}

// NewLLMClassifier creates a new LLM-based classifier.
func NewLLMClassifier(cfg config.LLMConfig) *LLMClassifier {
	c := &LLMClassifier{cfg: cfg}
	if cfg.Provider == "openai" && cfg.APIKey != "" {
		c.client = openai.NewClient(option.WithAPIKey(cfg.APIKey))
		c.hasLLM = true
	}
	return c
}

// ClassifyMessage sends the message to an LLM for classification.
func (c *LLMClassifier) ClassifyMessage(ctx context.Context, msg *message.Message) (bool, error) {
	slog.Debug("Classifying message",
		"source", msg.Source,
		"sender", msg.Sender,
		"text_preview", truncate(msg.Text, 50))

	// Use OpenAI if configured, otherwise fall back to keyword matching
	if c.hasLLM {
		return c.callOpenAI(ctx, msg)
	}

	// Fallback to keyword-based classification
	return c.keywordClassify(msg), nil
}

func (c *LLMClassifier) callOpenAI(ctx context.Context, msg *message.Message) (bool, error) {
	model := c.cfg.Model
	if model == "" {
		model = "gpt-5-nano"
	}

	systemPrompt := `You are a message urgency classifier. Analyze messages and determine if they require immediate attention.

Respond with ONLY "URGENT" or "NOT_URGENT".

A message is URGENT if it contains:
- Emergency situations or safety concerns
- Time-sensitive requests with immediate deadlines
- Financial emergencies or security alerts
- Health-related concerns
- Explicit urgency indicators (ASAP, urgent, critical, emergency)
- Someone asking for immediate help

A message is NOT_URGENT if it's:
- General conversation or greetings
- Marketing or promotional content
- Newsletters or automated notifications
- Non-time-sensitive questions
- Social media updates
- Routine status updates`

	userPrompt := fmt.Sprintf("Source: %s\nFrom: %s\nTime: %s\n\nMessage:\n%s",
		msg.Source,
		msg.Sender,
		msg.Timestamp.Format(time.RFC3339),
		msg.Text,
	)

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
		MaxCompletionTokens: openai.Int(50),
	})
	if err != nil {
		return false, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return false, fmt.Errorf("no response from OpenAI")
	}

	// Log full response for debugging
	slog.Debug("OpenAI raw response",
		"finish_reason", resp.Choices[0].FinishReason,
		"content", resp.Choices[0].Message.Content,
		"refusal", resp.Choices[0].Message.Refusal)

	result := strings.TrimSpace(strings.ToUpper(resp.Choices[0].Message.Content))
	isUrgent := strings.Contains(result, "URGENT") && !strings.Contains(result, "NOT_URGENT")

	slog.Info("OpenAI classification result",
		"result", result,
		"is_urgent", isUrgent,
		"model", model)

	return isUrgent, nil
}

func (c *LLMClassifier) keywordClassify(msg *message.Message) bool {
	text := strings.ToLower(msg.Text + " " + msg.Sender)

	urgentKeywords := []string{
		"urgent", "asap", "emergency", "critical",
		"help", "immediately", "deadline",
		"security", "breach", "down", "broken", "failed",
		"payment due", "transfer", "call me", "call asap",
	}

	for _, keyword := range urgentKeywords {
		if strings.Contains(text, keyword) {
			slog.Info("Message classified as URGENT (keyword)",
				"keyword_matched", keyword)
			return true
		}
	}

	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
