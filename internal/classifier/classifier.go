package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/message"
)

// ActionItem represents a detected action item with an optional date/time.
type ActionItem struct {
	Title           string
	Description     string
	DateTime        time.Time
	DurationMinutes int
}

// ClassificationResult holds the outcome of classifying a message.
type ClassificationResult struct {
	IsUrgent    bool
	ActionItems []ActionItem
}

// Classifier determines if messages are urgent/important and extracts action items.
type Classifier interface {
	ClassifyMessage(ctx context.Context, msg *message.Message) (*ClassificationResult, error)
}

// LLMClassifier uses an LLM to classify message urgency and extract action items.
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
func (c *LLMClassifier) ClassifyMessage(ctx context.Context, msg *message.Message) (*ClassificationResult, error) {
	slog.Debug("Classifying message",
		"source", msg.Source,
		"sender", msg.Sender,
		"text_preview", truncate(msg.Text, 50))

	if c.hasLLM {
		return c.callOpenAI(ctx, msg)
	}

	return c.keywordClassify(msg), nil
}

// llmResponse is the expected JSON structure from the LLM.
type llmResponse struct {
	Urgent      bool              `json:"urgent"`
	ActionItems []llmActionItem   `json:"action_items"`
}

type llmActionItem struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	Datetime        string `json:"datetime"`
	DurationMinutes int    `json:"duration_minutes"`
}

func (c *LLMClassifier) callOpenAI(ctx context.Context, msg *message.Message) (*ClassificationResult, error) {
	model := c.cfg.Model
	if model == "" {
		model = "gpt-5-nano"
	}

	systemPrompt := `You are a message analysis assistant. Analyze the message and return a JSON object with two fields:

1. "urgent" (boolean): true if the message requires immediate attention.
   Urgent criteria: emergencies, safety concerns, immediate deadlines, financial/security alerts, health concerns, explicit urgency (ASAP, urgent, critical).
   Not urgent: general conversation, marketing, newsletters, routine updates.

2. "action_items" (array): extract any action items that have a specific date or deadline. Each item has:
   - "title": short summary of the action
   - "description": fuller context
   - "datetime": ISO 8601 / RFC 3339 datetime string (e.g. "2025-03-15T14:00:00Z"). Only include if a specific date/time is mentioned or can be inferred.
   - "duration_minutes": estimated duration in minutes (default 30 if unclear)

   If there are no action items with dates, return an empty array.

Respond with ONLY valid JSON, no markdown fences or extra text. Example:
{"urgent": false, "action_items": [{"title": "Team meeting", "description": "Weekly sync with engineering", "datetime": "2025-03-15T14:00:00Z", "duration_minutes": 60}]}`

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
		MaxCompletionTokens: openai.Int(4096),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{
				Type: "json_object",
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("OpenAI returned empty response (finish_reason=%s)",
			resp.Choices[0].FinishReason)
	}

	slog.Debug("OpenAI raw response",
		"finish_reason", resp.Choices[0].FinishReason,
		"content", content,
		"refusal", resp.Choices[0].Message.Refusal)

	// Try to parse as JSON
	result, err := parseJSONResponse(content)
	if err != nil {
		slog.Warn("Failed to parse LLM JSON response, falling back to string matching",
			"error", err,
			"content", content)
		return fallbackStringMatch(content), nil
	}

	slog.Info("OpenAI classification result",
		"is_urgent", result.IsUrgent,
		"action_items", len(result.ActionItems),
		"model", model)

	return result, nil
}

func parseJSONResponse(content string) (*ClassificationResult, error) {
	// Strip markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var resp llmResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}

	result := &ClassificationResult{
		IsUrgent: resp.Urgent,
	}

	for _, item := range resp.ActionItems {
		ai := ActionItem{
			Title:           item.Title,
			Description:     item.Description,
			DurationMinutes: item.DurationMinutes,
		}
		if ai.DurationMinutes <= 0 {
			ai.DurationMinutes = 30
		}
		if item.Datetime != "" {
			t, err := time.Parse(time.RFC3339, item.Datetime)
			if err != nil {
				slog.Warn("Failed to parse action item datetime",
					"datetime", item.Datetime,
					"error", err)
				continue
			}
			ai.DateTime = t
		} else {
			// Skip action items without a datetime
			continue
		}
		result.ActionItems = append(result.ActionItems, ai)
	}

	return result, nil
}

// fallbackStringMatch handles the case where the LLM returns plain text instead of JSON.
func fallbackStringMatch(content string) *ClassificationResult {
	upper := strings.ToUpper(content)
	isUrgent := strings.Contains(upper, "URGENT") && !strings.Contains(upper, "NOT_URGENT")

	slog.Info("Fallback string classification",
		"is_urgent", isUrgent)

	return &ClassificationResult{
		IsUrgent: isUrgent,
	}
}

func (c *LLMClassifier) keywordClassify(msg *message.Message) *ClassificationResult {
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
			return &ClassificationResult{IsUrgent: true}
		}
	}

	return &ClassificationResult{IsUrgent: false}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
