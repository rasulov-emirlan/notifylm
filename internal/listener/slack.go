package listener

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/message"
)

// SlackListener implements the Listener interface for Slack Socket Mode.
type SlackListener struct {
	BaseListener
	cfg       config.SlackConfig
	api       *slack.Client
	socket    *socketmode.Client
	out       chan<- *message.Message
	userCache map[string]string
}

// NewSlackListener creates a new Slack listener.
func NewSlackListener(cfg config.SlackConfig) *SlackListener {
	return &SlackListener{
		BaseListener: NewBaseListener("slack"),
		cfg:          cfg,
		userCache:    make(map[string]string),
	}
}

func (s *SlackListener) Start(ctx context.Context, out chan<- *message.Message) error {
	s.out = out

	// Create Slack API client
	s.api = slack.New(
		s.cfg.BotToken,
		slack.OptionAppLevelToken(s.cfg.AppToken),
	)

	// Create Socket Mode client
	s.socket = socketmode.New(
		s.api,
		socketmode.OptionDebug(false),
	)

	// Handle events in a goroutine
	go s.handleEvents(ctx)

	slog.Info("Slack listener started (Socket Mode)")

	// Run socket mode client (blocking)
	return s.socket.RunContext(ctx)
}

func (s *SlackListener) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-s.socket.Events:
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				s.handleEventsAPI(evt)
			}
		}
	}
}

func (s *SlackListener) handleEventsAPI(evt socketmode.Event) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	// Acknowledge the event
	s.socket.Ack(*evt.Request)

	switch eventsAPIEvent.Type {
	case slackevents.CallbackEvent:
		s.handleCallbackEvent(eventsAPIEvent.InnerEvent)
	}
}

func (s *SlackListener) handleCallbackEvent(innerEvent slackevents.EventsAPIInnerEvent) {
	switch ev := innerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		s.handleMessage(ev)
	}
}

func (s *SlackListener) handleMessage(ev *slackevents.MessageEvent) {
	// Skip bot messages and message edits
	if ev.BotID != "" || ev.SubType != "" {
		return
	}

	if ev.Text == "" {
		return
	}

	sender := s.resolveUser(ev.User)

	msg := message.NewMessage(message.SourceSlack, sender, ev.Text)
	msg.ID = ev.ClientMsgID
	msg.Metadata["channel"] = ev.Channel
	msg.Metadata["channel_type"] = ev.ChannelType
	msg.Metadata["thread_ts"] = ev.ThreadTimeStamp

	s.out <- msg
}

func (s *SlackListener) resolveUser(userID string) string {
	if name, ok := s.userCache[userID]; ok {
		return name
	}

	user, err := s.api.GetUserInfo(userID)
	if err != nil {
		slog.Warn("Failed to resolve Slack user", "user_id", userID, "error", err)
		return userID
	}

	name := user.RealName
	if name == "" {
		name = user.Name
	}

	s.userCache[userID] = name
	return name
}

func (s *SlackListener) Stop() error {
	// Socket mode client cleanup is handled by context cancellation
	return nil
}

// ChannelName resolves a channel ID to its name.
func (s *SlackListener) ChannelName(channelID string) string {
	channel, err := s.api.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		return channelID
	}
	return fmt.Sprintf("#%s", channel.Name)
}
