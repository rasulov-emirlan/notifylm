package calendar

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/emirlan/notifylm/internal/classifier"
	"github.com/emirlan/notifylm/internal/config"
	"github.com/emirlan/notifylm/internal/googleauth"
	"github.com/emirlan/notifylm/internal/message"
)

// EventCreator creates calendar events from action items.
type EventCreator interface {
	CreateEvent(ctx context.Context, item *classifier.ActionItem, msg *message.Message) error
}

// GoogleCalendarCreator creates events in Google Calendar.
type GoogleCalendarCreator struct {
	service            *calendar.Service
	calendarID         string
	defaultDurationMin int
}

// NewGoogleCalendarCreator initializes a Google Calendar event creator.
func NewGoogleCalendarCreator(ctx context.Context, cfg config.CalendarConfig) (*GoogleCalendarCreator, error) {
	client, err := googleauth.GetOAuth2Client(ctx, cfg.CredentialsPath, cfg.TokenPath, calendar.CalendarEventsScope)
	if err != nil {
		return nil, fmt.Errorf("failed to get calendar OAuth2 client: %w", err)
	}

	svc, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create calendar service: %w", err)
	}

	calendarID := cfg.CalendarID
	if calendarID == "" {
		calendarID = "primary"
	}

	defaultDuration := cfg.DefaultDurationMinutes
	if defaultDuration <= 0 {
		defaultDuration = 30
	}

	return &GoogleCalendarCreator{
		service:            svc,
		calendarID:         calendarID,
		defaultDurationMin: defaultDuration,
	}, nil
}

func (g *GoogleCalendarCreator) CreateEvent(ctx context.Context, item *classifier.ActionItem, msg *message.Message) error {
	duration := item.DurationMinutes
	if duration <= 0 {
		duration = g.defaultDurationMin
	}

	start := item.DateTime
	end := start.Add(time.Duration(duration) * time.Minute)

	description := fmt.Sprintf("Source: %s\nFrom: %s\n\n%s",
		msg.Source, msg.Sender, item.Description)

	event := &calendar.Event{
		Summary:     item.Title,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: start.Format(time.RFC3339),
		},
		End: &calendar.EventDateTime{
			DateTime: end.Format(time.RFC3339),
		},
	}

	created, err := g.service.Events.Insert(g.calendarID, event).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to create calendar event: %w", err)
	}

	slog.Info("Calendar event created",
		"title", item.Title,
		"start", start.Format(time.RFC3339),
		"event_link", created.HtmlLink)

	return nil
}

// MockCalendarCreator logs events instead of creating them.
type MockCalendarCreator struct{}

func NewMockCalendarCreator() *MockCalendarCreator {
	return &MockCalendarCreator{}
}

func (m *MockCalendarCreator) CreateEvent(_ context.Context, item *classifier.ActionItem, msg *message.Message) error {
	slog.Info("MOCK CALENDAR EVENT",
		"title", item.Title,
		"description", item.Description,
		"datetime", item.DateTime.Format(time.RFC3339),
		"duration_minutes", item.DurationMinutes,
		"source", msg.Source,
		"sender", msg.Sender)
	return nil
}
