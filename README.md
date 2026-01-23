# notifylm

Unified notification interceptor that listens to your personal communication channels and pushes urgent messages to your phone via Pushover.

## Supported Channels

- **Gmail** - OAuth2 polling
- **WhatsApp** - via whatsmeow (multi-device)
- **Telegram** - via gotd/td (userbot)
- **Slack** - Socket Mode

## How It Works

```
Gmail/WhatsApp/Telegram/Slack → Message Queue → LLM Classifier → Pushover → iPhone
```

Messages are classified using keyword matching (or optionally an LLM) to detect urgency. Only urgent messages trigger push notifications.

## Quick Start

```bash
# Clone and build
git clone https://github.com/yourusername/notifylm.git
cd notifylm
make build

# Configure
cp config.example.yaml config.yaml
# Edit config.yaml with your credentials

# Run
make run-debug
```

## Setup

### Gmail

1. Create a project in [Google Cloud Console](https://console.cloud.google.com/)
2. Enable the Gmail API
3. Configure OAuth consent screen and add yourself as a test user
4. Create OAuth credentials (Desktop App)
5. Download as `gmail-credentials.json`

### Pushover

1. Create account at [pushover.net](https://pushover.net/)
2. Install Pushover app on your phone
3. Create an application to get an App Token
4. Add tokens to `config.yaml`

### Other Listeners

See `config.example.yaml` for WhatsApp, Telegram, and Slack configuration.

## Configuration

```yaml
gmail:
  enabled: true
  credentials_path: "./gmail-credentials.json"
  token_path: "./token.json"
  poll_interval_seconds: 30

pushover:
  app_token: "your-app-token"
  user_token: "your-user-key"
```

## License

MIT
