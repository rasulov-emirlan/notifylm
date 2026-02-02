//go:build integration

package classifier

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/message"
)

func setupClassifier(t *testing.T) *LLMClassifier {
	t.Helper()
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}
	return NewLLMClassifier(config.LLMConfig{
		Provider: "openai",
		APIKey:   apiKey,
		Model:    "gpt-5-nano",
	})
}

func newMessage(text string) *message.Message {
	return &message.Message{
		ID:        "integration-test",
		Source:    message.SourceSlack,
		Sender:    "test-user",
		Text:      text,
		Timestamp: time.Now(),
		Metadata:  map[string]string{},
	}
}

// classifyWithRetry calls callOpenAI directly (bypassing keyword fallback) and
// retries up to maxAttempts times to handle occasional empty LLM responses.
func classifyWithRetry(t *testing.T, c *LLMClassifier, msg *message.Message, maxAttempts int) *ClassificationResult {
	t.Helper()
	for i := 0; i < maxAttempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result, err := c.callOpenAI(ctx, msg)
		cancel()
		if err != nil {
			t.Logf("attempt %d/%d: API error: %v", i+1, maxAttempts, err)
			continue
		}
		return result
	}
	t.Fatalf("callOpenAI failed after %d attempts", maxAttempts)
	return nil
}

func TestIntegration_UrgentMessage(t *testing.T) {
	t.Parallel()
	c := setupClassifier(t)

	result := classifyWithRetry(t, c, newMessage("EMERGENCY: server is down, need help NOW"), 3)

	if !result.IsUrgent {
		t.Error("expected message to be classified as urgent")
	}
}

func TestIntegration_NotUrgentMessage(t *testing.T) {
	t.Parallel()
	c := setupClassifier(t)

	result := classifyWithRetry(t, c, newMessage("Hey, just wanted to share this funny meme"), 3)

	if result.IsUrgent {
		t.Error("expected message to be classified as not urgent")
	}
}

func TestIntegration_ActionItemExtraction(t *testing.T) {
	t.Parallel()
	c := setupClassifier(t)

	result := classifyWithRetry(t, c, newMessage(
		"Let's schedule a meeting on March 15, 2025 at 2pm to discuss the roadmap",
	), 3)

	if len(result.ActionItems) < 1 {
		t.Fatal("expected at least 1 action item")
	}

	item := result.ActionItems[0]
	if item.Title == "" {
		t.Error("expected action item Title to be non-empty")
	}
	if item.DateTime.IsZero() {
		t.Error("expected action item DateTime to be non-zero")
	}
}

func TestIntegration_UrgentWithActionItems(t *testing.T) {
	t.Parallel()
	c := setupClassifier(t)

	result := classifyWithRetry(t, c, newMessage(
		"URGENT: client demo moved to tomorrow March 20, 2025 at 10am, we're not ready",
	), 3)

	if !result.IsUrgent {
		t.Error("expected message to be classified as urgent")
	}
	if len(result.ActionItems) < 1 {
		t.Error("expected at least 1 action item")
	}
}
