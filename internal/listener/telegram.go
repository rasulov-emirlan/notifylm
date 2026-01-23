package listener

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/message"
)

// TelegramListener implements the Listener interface for Telegram userbot.
type TelegramListener struct {
	BaseListener
	cfg    config.TelegramConfig
	client *telegram.Client
	out    chan<- *message.Message
}

// NewTelegramListener creates a new Telegram listener.
func NewTelegramListener(cfg config.TelegramConfig) *TelegramListener {
	return &TelegramListener{
		BaseListener: NewBaseListener("telegram"),
		cfg:          cfg,
	}
}

func (t *TelegramListener) Start(ctx context.Context, out chan<- *message.Message) error {
	t.out = out

	// Ensure data directory exists
	if err := os.MkdirAll(t.cfg.DataPath, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create update dispatcher
	dispatcher := tg.NewUpdateDispatcher()

	// Create gaps handler for proper update handling
	gaps := updates.New(updates.Config{
		Handler: dispatcher,
	})

	// Handle new messages
	dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg.Out {
			return nil // Skip non-messages and outgoing
		}

		t.handleMessage(ctx, e, msg)
		return nil
	})

	// Create client
	t.client = telegram.NewClient(t.cfg.AppID, t.cfg.AppHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: fmt.Sprintf("%s/session.json", t.cfg.DataPath),
		},
		UpdateHandler: gaps,
	})

	// Run client
	return t.client.Run(ctx, func(ctx context.Context) error {
		// Check auth status
		status, err := t.client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("failed to get auth status: %w", err)
		}

		if !status.Authorized {
			// Need to authenticate
			if err := t.authenticate(ctx); err != nil {
				return fmt.Errorf("failed to authenticate: %w", err)
			}
		}

		slog.Info("Telegram listener started", "user", status.User.Username)

		// Run gaps handler to receive updates
		return gaps.Run(ctx, t.client.API(), status.User.ID, updates.AuthOptions{
			IsBot: status.User.Bot,
		})
	})
}

func (t *TelegramListener) authenticate(ctx context.Context) error {
	flow := auth.NewFlow(
		terminalAuth{phone: t.cfg.Phone},
		auth.SendCodeOptions{},
	)
	return t.client.Auth().IfNecessary(ctx, flow)
}

func (t *TelegramListener) handleMessage(ctx context.Context, e tg.Entities, msg *tg.Message) {
	if msg.Message == "" {
		return
	}

	sender := "Unknown"
	if msg.FromID != nil {
		if peerUser, ok := msg.FromID.(*tg.PeerUser); ok {
			if user, ok := e.Users[peerUser.UserID]; ok {
				sender = formatTelegramUser(user)
			}
		}
	}

	m := message.NewMessage(message.SourceTelegram, sender, msg.Message)
	m.ID = fmt.Sprintf("%d", msg.ID)
	m.Metadata["peer_id"] = fmt.Sprintf("%v", msg.PeerID)

	t.out <- m
}

func formatTelegramUser(user *tg.User) string {
	if user.FirstName != "" {
		name := user.FirstName
		if user.LastName != "" {
			name += " " + user.LastName
		}
		return name
	}
	if user.Username != "" {
		return "@" + user.Username
	}
	return fmt.Sprintf("User#%d", user.ID)
}

func (t *TelegramListener) Stop() error {
	// Client cleanup is handled by context cancellation
	return nil
}

// terminalAuth implements auth.UserAuthenticator for terminal-based auth.
type terminalAuth struct {
	phone string
}

func (a terminalAuth) Phone(_ context.Context) (string, error) {
	return a.phone, nil
}

func (a terminalAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Enter 2FA password: ")
	return readLine()
}

func (a terminalAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("Enter Telegram code: ")
	return readLine()
}

func (a terminalAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("sign up not supported")
}

func (a terminalAuth) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func readLine() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
