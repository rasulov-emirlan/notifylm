# CLAUDE.md

This file provides context for Claude Code when working on this codebase.

## Project Overview

**notifylm** is a Unified Notification Interceptor service written in Go. It listens to multiple personal communication channels in real-time, classifies messages using OpenAI (gpt-5-nano), extracts action items with dates, creates Google Calendar events, and pushes only high-priority alerts to an iPhone via Pushover.

## Architecture

```
┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│  WhatsApp   │  │  Telegram   │  │    Slack    │  │    Gmail    │
│  Listener   │  │  Listener   │  │  Listener   │  │  Listener   │
└──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘
       │                │                │                │
       └────────────────┴────────────────┴────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │   Central Message     │
                    │   Channel (buffered)  │
                    └───────────┬───────────┘
                                │
                    ┌───────────▼───────────┐
                    │   Message Processor   │
                    │   (single goroutine)  │
                    └───────────┬───────────┘
                                │
                    ┌───────────▼───────────┐
                    │   LLM Classifier      │
                    │   (OpenAI gpt-5-nano) │
                    └───────────┬───────────┘
                                │
                    ┌───────────┴───────────┐
                    │                       │
              ┌─────▼─────┐        ┌───────▼────────┐
              │ is urgent? │        │ action items?  │
              └─────┬─────┘        └───────┬────────┘
                yes │                      │ yes
              ┌─────▼──────┐       ┌───────▼────────┐
              │  Pushover  │       │ Google Calendar│
              │  Notifier  │       │ Event Creator  │
              └────────────┘       └────────────────┘
```

## Directory Structure

```
cmd/notifylm/main.go                  - Entry point, wires everything together
internal/
  message/message.go                  - Unified Message struct used across all listeners
  config/config.go                    - YAML config loading with env var expansion
  googleauth/googleauth.go            - Shared OAuth2 auth flow for Google services
  listener/
    listener.go                       - Listener interface definition
    whatsapp.go                       - WhatsApp via whatsmeow (linked device)
    telegram.go                       - Telegram via gotd/td (userbot, not bot API)
    slack.go                          - Slack via Socket Mode
    gmail.go                          - Gmail via OAuth2 polling
  classifier/
    classifier.go                     - LLM-based urgency classification + action item extraction
    classifier_integration_test.go    - Integration tests (requires OPENAI_API_KEY)
  calendar/calendar.go                - Google Calendar event creation from action items
  notifier/pushover.go                - Pushover push notification sender
```

## Key Libraries

| Integration | Library | Auth Method |
|-------------|---------|-------------|
| WhatsApp | `go.mau.fi/whatsmeow` | QR code linking (multi-device) |
| Telegram | `github.com/gotd/td` | Phone number + code (userbot) |
| Slack | `github.com/slack-go/slack` | Socket Mode (app + bot tokens) |
| Gmail | `google.golang.org/api/gmail/v1` | OAuth2 (via `googleauth` package) |
| Google Calendar | `google.golang.org/api/calendar/v3` | OAuth2 (via `googleauth` package) |
| LLM | `github.com/openai/openai-go` | API key |
| Notifications | `github.com/gregdel/pushover` | App + user tokens |

## Build & Run

```bash
make build            # Build binary
make run              # Run with config.yaml
make run-dry          # Dry run (no actual notifications)
make run-debug        # With debug logging
make test             # Run unit tests
make test-integration # Run integration tests (requires OPENAI_API_KEY)
make lint             # Run golangci-lint
make deps             # go mod tidy && go mod download
make setup            # Create data dirs, copy config.example.yaml
```

## Testing

### Unit tests

```bash
make test
```

### Integration tests

Integration tests are behind `//go:build integration` so `go test ./...` skips them.

They call the real OpenAI API (gpt-5-nano) to verify classification and action item extraction. 4 test cases in `internal/classifier/classifier_integration_test.go`:

- `TestIntegration_UrgentMessage` — urgent classification
- `TestIntegration_NotUrgentMessage` — non-urgent classification
- `TestIntegration_ActionItemExtraction` — action item with date extraction
- `TestIntegration_UrgentWithActionItems` — urgent + action items combined

```bash
export OPENAI_API_KEY=sk-...
make test-integration
```

Tests use a retry helper (`classifyWithRetry`) that calls `callOpenAI` directly and retries up to 3 times to handle occasional empty model responses.

## Common Tasks

### Adding a new listener

1. Create `internal/listener/newservice.go`
2. Implement the `Listener` interface:
   ```go
   type Listener interface {
       Name() string
       Start(ctx context.Context, out chan<- *message.Message) error
       Stop() error
   }
   ```
3. Add config struct to `internal/config/config.go`
4. Register in `initializeListeners()` in `cmd/notifylm/main.go`

### Adding notification channels

Implement the `Notifier` interface in `internal/notifier/`:
```go
type Notifier interface {
    Notify(msg *message.Message) error
}
```

### Adding calendar integrations

Implement the `EventCreator` interface in `internal/calendar/`:
```go
type EventCreator interface {
    CreateEvent(ctx context.Context, item *classifier.ActionItem, msg *message.Message) error
}
```

## LLM Classifier

The classifier (`internal/classifier/classifier.go`) uses OpenAI with `response_format: json_object` to classify messages and extract action items.

- **Model**: `gpt-5-nano` (configurable via `config.yaml`)
- **MaxCompletionTokens**: 4096 (gpt-5-nano uses reasoning tokens that count against this limit)
- **Fallback**: If no API key is configured, falls back to keyword-based classification
- **Empty response handling**: Returns error on empty content (with `finish_reason` in message) instead of silently falling back

The classifier returns a `ClassificationResult` with:
- `IsUrgent bool` — whether the message needs immediate attention
- `ActionItems []ActionItem` — extracted action items with `Title`, `Description`, `DateTime`, and `DurationMinutes`

## Code Patterns

- **Context propagation**: All listeners receive a context for graceful shutdown
- **Channel-based concurrency**: Messages flow through a buffered channel (`chan *message.Message`, capacity 100)
- **Interface-driven design**: Listeners, classifiers, notifiers, and calendar creators are all interfaces
- **Structured logging**: Uses `log/slog` throughout
- **Shared Google OAuth**: `internal/googleauth` handles OAuth2 for both Gmail and Google Calendar

## Configuration

Config is loaded from `config.yaml` with environment variable expansion (`${VAR_NAME}`). See `config.example.yaml` for all options.

Sensitive values that should never be committed:
- `config.yaml`
- `gmail-credentials.json` / `credentials.json` (Google OAuth client credentials)
- `token.json` (Gmail OAuth token, auto-generated after first auth)
- `calendar-token.json` (Calendar OAuth token, auto-generated after first auth)
- `data/` directory (session data)

## Gmail OAuth Setup

1. Create a project in [Google Cloud Console](https://console.cloud.google.com/)
2. Enable the **Gmail API**
3. Configure OAuth consent screen (External)
4. Add your email as a **Test User** (required while app is unverified)
5. Create OAuth credentials (Desktop App) and download as `gmail-credentials.json`
6. On first run, open the OAuth URL in browser and paste the authorization code

## Google Calendar Setup

1. In the same Google Cloud project, enable the **Google Calendar API**
2. The same OAuth credentials file is reused
3. On first run with calendar enabled, a separate OAuth flow generates `calendar-token.json`
4. Configure `calendar_id` in config (defaults to `"primary"`)

## Pushover Setup

1. Create account at [pushover.net](https://pushover.net/)
2. Install Pushover app on iPhone and log in
3. Note your **User Key** from the dashboard
4. Create an Application at [pushover.net/apps/build](https://pushover.net/apps/build) to get an **App Token**

## Error Handling

- Listener failures are logged but don't crash the service
- Classification failures skip the message (logged as warning)
- Notification failures are logged as errors but don't retry
- Calendar event creation failures are logged but don't stop message processing

## Shutdown Behavior

1. SIGINT/SIGTERM triggers context cancellation
2. All listeners stop accepting new messages
3. Message channel is drained
4. Listeners call `Stop()` for cleanup
