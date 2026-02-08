package server

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/emirlan/notifylm/internal/message"
	"github.com/emirlan/notifylm/internal/store"
)

//go:embed templates/dashboard.html
var templateFS embed.FS

// DashboardData holds all data passed to the main dashboard template.
type DashboardData struct {
	Messages      []store.ProcessedMessage
	Listeners     []store.ListenerStatus
	Stats         store.Stats
	ActionItems   []store.ActionItemWithContext
	Notifications []store.Notification
	Uptime        string
}

// Server serves the HTMX dashboard and provides API endpoints for live updates.
type Server struct {
	store     *store.Store
	srv       *http.Server
	tmpl      *template.Template
	startedAt time.Time

	// HTMX partial templates
	messagesTmpl      *template.Template
	statsTmpl         *template.Template
	listenersTmpl     *template.Template
	actionsTmpl       *template.Template
	notificationsTmpl *template.Template
}

// Template helper functions.
var funcMap = template.FuncMap{
	"timeAgo":      timeAgo,
	"truncateText": truncateText,
	"sourceIcon":   sourceIcon,
	"sourceColor":  sourceColor,
}

// New creates a new Server with the given store and port.
// If port is 0, it defaults to 8080.
func New(st *store.Store, port int) *Server {
	if port == 0 {
		port = 8080
	}

	s := &Server{
		store:     st,
		startedAt: time.Now(),
	}

	// Parse the embedded dashboard template.
	s.tmpl = template.Must(
		template.New("dashboard.html").Funcs(funcMap).ParseFS(templateFS, "templates/dashboard.html"),
	)

	// Parse HTMX partial templates.
	s.messagesTmpl = template.Must(template.New("messages").Funcs(funcMap).Parse(messagesPartial))
	s.statsTmpl = template.Must(template.New("stats").Funcs(funcMap).Parse(statsPartial))
	s.listenersTmpl = template.Must(template.New("listeners").Funcs(funcMap).Parse(listenersPartial))
	s.actionsTmpl = template.Must(template.New("actions").Funcs(funcMap).Parse(actionsPartial))
	s.notificationsTmpl = template.Must(template.New("notifications").Funcs(funcMap).Parse(notificationsPartial))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleDashboard)
	mux.HandleFunc("GET /api/messages", s.handleMessages)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/listeners", s.handleListeners)
	mux.HandleFunc("GET /api/actions", s.handleActions)
	mux.HandleFunc("GET /api/notifications", s.handleNotifications)
	mux.HandleFunc("GET /sse", s.handleSSE)

	s.srv = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the HTTP server in a background goroutine.
func (s *Server) Start() error {
	slog.Info("Starting dashboard server", "addr", s.srv.Addr)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Dashboard server error", "error", err)
		}
	}()
	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down dashboard server")
	return s.srv.Shutdown(ctx)
}

// --- HTTP Handlers ---

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := DashboardData{
		Messages:      s.store.GetRecentMessages(50),
		Listeners:     s.store.GetListenerStatuses(),
		Stats:         s.store.GetStats(),
		ActionItems:   s.store.GetActionItems(20),
		Notifications: s.store.GetRecentNotifications(20),
		Uptime:        timeAgo(s.startedAt),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		slog.Error("Failed to render dashboard template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	messages := s.store.GetRecentMessages(50)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.messagesTmpl.Execute(w, messages); err != nil {
		slog.Error("Failed to render messages partial", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.store.GetStats()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.statsTmpl.Execute(w, stats); err != nil {
		slog.Error("Failed to render stats partial", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleListeners(w http.ResponseWriter, r *http.Request) {
	listeners := s.store.GetListenerStatuses()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.listenersTmpl.Execute(w, listeners); err != nil {
		slog.Error("Failed to render listeners partial", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	actions := s.store.GetActionItems(20)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.actionsTmpl.Execute(w, actions); err != nil {
		slog.Error("Failed to render actions partial", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	notifications := s.store.GetRecentNotifications(20)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.notificationsTmpl.Execute(w, notifications); err != nil {
		slog.Error("Failed to render notifications partial", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Disable write deadline for this long-lived SSE connection.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.store.Subscribe()
	defer s.store.Unsubscribe(ch)

	// Send an initial comment to establish the connection.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		}
	}
}

// --- Template Helper Functions ---

// timeAgo returns a human-readable relative time string.
func timeAgo(v any) string {
	var t time.Time
	switch val := v.(type) {
	case time.Time:
		t = val
	case *time.Time:
		if val == nil {
			return "never"
		}
		t = *val
	default:
		return "unknown"
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// truncateText truncates a string to max characters and appends "..." if truncated.
func truncateText(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// sourceIcon returns an emoji icon for the given message source.
func sourceIcon(s message.Source) string {
	switch s {
	case message.SourceWhatsApp:
		return "\U0001F7E2" // green circle
	case message.SourceTelegram:
		return "\u2708\uFE0F" // airplane
	case message.SourceSlack:
		return "\U0001F4AC" // speech balloon
	case message.SourceGmail:
		return "\U0001F4E7" // e-mail
	default:
		return "\U0001F4E8" // incoming envelope
	}
}

// sourceColor returns a CSS class name for the given message source.
func sourceColor(s message.Source) string {
	switch s {
	case message.SourceWhatsApp:
		return "whatsapp"
	case message.SourceTelegram:
		return "telegram"
	case message.SourceSlack:
		return "slack"
	case message.SourceGmail:
		return "gmail"
	default:
		return "gmail"
	}
}

// --- HTMX Partial Templates (match dashboard.html CSS classes) ---

const messagesPartial = `{{range .}}
<div class="message-item{{if .Classification}}{{if .Classification.IsUrgent}} urgent{{end}}{{end}}">
  <div class="message-header">
    <span class="source-badge {{sourceColor .Message.Source}}">{{sourceIcon .Message.Source}} {{.Message.Source}}</span>
    <span class="sender">{{.Message.Sender}}</span>
    <span class="timestamp">{{timeAgo .Message.Timestamp}}</span>
  </div>
  <div class="message-body">{{truncateText .Message.Text 120}}</div>
  <div class="message-tags">
    {{if .Classification}}{{if .Classification.IsUrgent}}<span class="tag urgent">urgent</span>{{end}}{{end}}
    {{if .Classification}}{{if .Classification.ActionItems}}<span class="tag action">{{len .Classification.ActionItems}} action{{if gt (len .Classification.ActionItems) 1}}s{{end}}</span>{{end}}{{end}}
  </div>
</div>
{{else}}
<div class="empty-state">
  <div class="empty-state-icon">&#x1f4e1;</div>
  <div class="empty-state-text">Waiting for messages...</div>
</div>
{{end}}`

const statsPartial = `<div class="stat-card">
  <div class="stat-value">{{.TotalMessages}}</div>
  <div class="stat-label">Messages</div>
  <div class="source-breakdown">
    {{range $source, $count := .BySource}}
    <span class="source-mini"><span class="dot {{$source}}"></span>{{$count}}</span>
    {{end}}
  </div>
</div>
<div class="stat-card urgent">
  <div class="stat-value">{{.UrgentMessages}}</div>
  <div class="stat-label">Urgent</div>
</div>
<div class="stat-card">
  <div class="stat-value">{{.TotalActionItems}}</div>
  <div class="stat-label">Action Items</div>
</div>
<div class="stat-card">
  <div class="stat-value">{{.NotificationsSent}}</div>
  <div class="stat-label">Notified</div>
</div>
<div class="stat-card">
  <div class="stat-value">{{.EventsCreated}}</div>
  <div class="stat-label">Events</div>
</div>`

const listenersPartial = `{{range .}}
<div class="listener-item">
  <span class="status-dot {{if .Connected}}connected{{else}}disconnected{{end}}"></span>
  <div class="listener-info">
    <div class="listener-name">{{.Name}}</div>
    <div class="listener-meta">
      {{if .LastMessage}}Last: {{timeAgo .LastMessage}}{{else}}No messages yet{{end}}
    </div>
  </div>
  <div class="listener-count">{{.MessageCount}}</div>
</div>
{{else}}
<div class="empty-state">
  <div class="empty-state-icon">&#x1f50c;</div>
  <div class="empty-state-text">No listeners configured</div>
</div>
{{end}}`

const actionsPartial = `{{range .}}
<div class="action-item">
  <div class="action-header">
    <span class="action-title">{{.Item.Title}}</span>
    <span class="action-check {{if .EventCreated}}created{{else}}pending{{end}}">
      {{if .EventCreated}}&#x2713;{{else}}&#x2026;{{end}}
    </span>
  </div>
  {{if .Item.Description}}<div class="action-description">{{truncateText .Item.Description 80}}</div>{{end}}
  <div class="action-meta">
    {{if not .Item.DateTime.IsZero}}<span>&#x1f4c5; {{.Item.DateTime.Format "Jan 2, 15:04"}}</span>{{end}}
    {{if gt .Item.DurationMinutes 0}}<span>&#x23f1; {{.Item.DurationMinutes}}min</span>{{end}}
    <span>via {{.SourceMsg.Sender}}</span>
  </div>
</div>
{{else}}
<div class="empty-state">
  <div class="empty-state-icon">&#x2705;</div>
  <div class="empty-state-text">No action items yet</div>
</div>
{{end}}`

const notificationsPartial = `{{range .}}
<div class="notif-item">
  <span class="notif-reason {{.Reason}}">
    {{if eq .Reason "urgent"}}&#x1f6a8;{{else}}&#x1f4cb;{{end}}
    {{.Reason}}
  </span>
  <span class="notif-body">{{.Message.Sender}}: {{truncateText .Message.Text 60}}</span>
  <span class="notif-time">{{timeAgo .SentAt}}</span>
</div>
{{else}}
<div class="empty-state">
  <div class="empty-state-icon">&#x1f514;</div>
  <div class="empty-state-text">No notifications sent yet</div>
</div>
{{end}}`
