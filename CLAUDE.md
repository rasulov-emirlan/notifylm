# CLAUDE.md

This file provides context for Claude Code when working on this codebase.

## Project Overview

**notifylm** is a Unified Notification Interceptor service written in Go. It listens to multiple personal communication channels in real-time, filters messages using an LLM classifier, and pushes only high-priority alerts to an iPhone via Pushover.

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
                    │   classifyMessage()   │
                    └───────────┬───────────┘
                                │
                         ┌──────┴──────┐
                         │ is urgent?  │
                         └──────┬──────┘
                           yes  │
                    ┌───────────▼───────────┐
                    │   Pushover Notifier   │
                    └───────────────────────┘
```

## Directory Structure

```
cmd/notifylm/main.go     - Entry point, wires everything together
internal/
  message/message.go     - Unified Message struct used across all listeners
  config/config.go       - YAML config loading with env var expansion
  listener/
    listener.go          - Listener interface definition
    whatsapp.go          - WhatsApp via whatsmeow (linked device)
    telegram.go          - Telegram via gotd/td (userbot, not bot API)
    slack.go             - Slack via Socket Mode
    gmail.go             - Gmail via OAuth2 polling
  classifier/classifier.go - LLM-based urgency classification
  notifier/pushover.go   - Pushover push notification sender
```

## Key Libraries

| Integration | Library | Auth Method |
|-------------|---------|-------------|
| WhatsApp | `go.mau.fi/whatsmeow` | QR code linking (multi-device) |
| Telegram | `github.com/gotd/td` | Phone number + code (userbot) |
| Slack | `github.com/slack-go/slack` | Socket Mode (app + bot tokens) |
| Gmail | `google.golang.org/api/gmail/v1` | OAuth2 |
| Notifications | `github.com/gregdel/pushover` | App + user tokens |

## Build & Run

```bash
make build      # Build binary
make run        # Run with config.yaml
make run-dry    # Dry run (no actual notifications)
make run-debug  # With debug logging
make test       # Run tests
```

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

### Implementing real LLM classification

The mock classifier is in `internal/classifier/classifier.go`. To integrate real LLM:

1. Uncomment `RealLLMClassifier` struct
2. Implement `callOpenAI()` or `callGemini()` methods
3. Replace `NewLLMClassifier` with `NewRealLLMClassifier` in main.go

### Adding notification channels

Implement the `Notifier` interface in `internal/notifier/`:
```go
type Notifier interface {
    Notify(msg *message.Message) error
}
```

## Code Patterns

- **Context propagation**: All listeners receive a context for graceful shutdown
- **Channel-based concurrency**: Messages flow through a buffered channel (`chan *message.Message`)
- **Interface-driven design**: Listeners, classifiers, and notifiers are all interfaces
- **Structured logging**: Uses `log/slog` throughout

## Configuration

Config is loaded from `config.yaml` with environment variable expansion (`${VAR_NAME}`). See `config.example.yaml` for all options.

Sensitive values that should never be committed:
- `config.yaml`
- `gmail-credentials.json` (Gmail OAuth client credentials)
- `token.json` (Gmail OAuth token, auto-generated after first auth)
- `data/` directory (session data)

## Gmail OAuth Setup

1. Create a project in [Google Cloud Console](https://console.cloud.google.com/)
2. Enable the **Gmail API**
3. Configure OAuth consent screen (External)
4. Add your email as a **Test User** (required while app is unverified)
5. Create OAuth credentials (Desktop App) and download as `gmail-credentials.json`
6. On first run, open the OAuth URL in browser and paste the authorization code

## Pushover Setup

1. Create account at [pushover.net](https://pushover.net/)
2. Install Pushover app on iPhone and log in
3. Note your **User Key** from the dashboard
4. Create an Application at [pushover.net/apps/build](https://pushover.net/apps/build) to get an **App Token**

## Testing Guidelines

- Mock external services using interfaces
- Test classification logic with various message inputs
- Listeners are difficult to unit test; prefer integration tests

## Error Handling

- Listener failures are logged but don't crash the service
- Classification failures skip the message (logged as warning)
- Notification failures are logged as errors but don't retry

## Shutdown Behavior

1. SIGINT/SIGTERM triggers context cancellation
2. All listeners stop accepting new messages
3. Message channel is drained
4. Listeners call `Stop()` for cleanup
