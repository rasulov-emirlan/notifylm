package store

import (
	"sync"
	"time"

	"github.com/emirlan/notifylm/internal/classifier"
	"github.com/emirlan/notifylm/internal/message"
)

// ProcessedMessage holds a message along with its classification results and processing metadata.
type ProcessedMessage struct {
	Message        *message.Message
	Classification *classifier.ClassificationResult
	NotifiedAt     *time.Time // nil if notification wasn't sent
	EventsCreated  int        // number of calendar events created
	ProcessedAt    time.Time
}

// ListenerStatus tracks the state of each message listener.
type ListenerStatus struct {
	Name         string
	Source       message.Source
	Connected    bool
	MessageCount int
	LastMessage  *time.Time
}

// Notification records a sent push notification.
type Notification struct {
	Message *message.Message
	Reason  string // "urgent", "action_item"
	SentAt  time.Time
}

// ActionItemWithContext pairs an action item with the message it was extracted from.
type ActionItemWithContext struct {
	Item         classifier.ActionItem
	SourceMsg    *message.Message
	EventCreated bool
	ProcessedAt  time.Time
}

// Stats holds aggregate statistics.
type Stats struct {
	TotalMessages     int
	UrgentMessages    int
	TotalActionItems  int
	NotificationsSent int
	EventsCreated     int
	BySource          map[message.Source]int
}

const maxNotifications = 100

// Store is a thread-safe in-memory store with a ring buffer for messages.
type Store struct {
	mu       sync.RWMutex
	messages []ProcessedMessage // ring buffer
	capacity int
	writeIdx int
	count    int

	listeners     map[string]*ListenerStatus // keyed by listener name
	notifications []Notification             // capped at maxNotifications

	stats Stats

	// SSE subscribers
	ssemu       sync.Mutex
	subscribers map[chan string]struct{}
}

// NewStore creates a new store with the given ring buffer capacity.
// If capacity is <= 0, it defaults to 500.
func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = 500
	}
	return &Store{
		messages:    make([]ProcessedMessage, capacity),
		capacity:    capacity,
		listeners:   make(map[string]*ListenerStatus),
		subscribers: make(map[chan string]struct{}),
		stats: Stats{
			BySource: make(map[message.Source]int),
		},
	}
}

// AddProcessedMessage adds a message to the ring buffer, updates stats, and
// notifies SSE subscribers.
func (s *Store) AddProcessedMessage(pm ProcessedMessage) {
	s.mu.Lock()

	// Write into the ring buffer.
	s.messages[s.writeIdx] = pm
	s.writeIdx = (s.writeIdx + 1) % s.capacity
	if s.count < s.capacity {
		s.count++
	}

	// Update stats.
	s.stats.TotalMessages++
	if pm.Message != nil {
		s.stats.BySource[pm.Message.Source]++
	}
	if pm.Classification != nil {
		if pm.Classification.IsUrgent {
			s.stats.UrgentMessages++
		}
		s.stats.TotalActionItems += len(pm.Classification.ActionItems)
	}
	if pm.NotifiedAt != nil {
		s.stats.NotificationsSent++
	}
	s.stats.EventsCreated += pm.EventsCreated

	s.mu.Unlock()

	s.notifySubscribers("refresh")
}

// GetRecentMessages returns the most recent N messages in reverse chronological order.
func (s *Store) GetRecentMessages(limit int) []ProcessedMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > s.count {
		limit = s.count
	}

	result := make([]ProcessedMessage, 0, limit)
	for i := 0; i < limit; i++ {
		// Walk backwards from the most recently written position.
		idx := (s.writeIdx - 1 - i + s.capacity) % s.capacity
		result = append(result, s.messages[idx])
	}
	return result
}

// GetRecentMessagesBySource returns the most recent N messages from a specific source,
// in reverse chronological order.
func (s *Store) GetRecentMessagesBySource(source message.Source, limit int) []ProcessedMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = s.count
	}

	result := make([]ProcessedMessage, 0, limit)
	for i := 0; i < s.count && len(result) < limit; i++ {
		idx := (s.writeIdx - 1 - i + s.capacity) % s.capacity
		pm := s.messages[idx]
		if pm.Message != nil && pm.Message.Source == source {
			result = append(result, pm)
		}
	}
	return result
}

// UpdateListenerStatus updates the connection status of a listener.
func (s *Store) UpdateListenerStatus(name string, source message.Source, connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ls, ok := s.listeners[name]
	if !ok {
		ls = &ListenerStatus{
			Name:   name,
			Source: source,
		}
		s.listeners[name] = ls
	}
	ls.Connected = connected
}

// IncrementListenerMessageCount increments the message count and updates the last
// message time for the listener matching the given source.
func (s *Store) IncrementListenerMessageCount(source message.Source) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, ls := range s.listeners {
		if ls.Source == source {
			ls.MessageCount++
			ls.LastMessage = &now
			return
		}
	}
}

// GetListenerStatuses returns a snapshot of all listener statuses.
func (s *Store) GetListenerStatuses() []ListenerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ListenerStatus, 0, len(s.listeners))
	for _, ls := range s.listeners {
		cp := *ls
		result = append(result, cp)
	}
	return result
}

// AddNotification adds a notification to the log, keeping at most the last 100 entries.
func (s *Store) AddNotification(n Notification) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.notifications) >= maxNotifications {
		// Drop the oldest entry.
		s.notifications = s.notifications[1:]
	}
	s.notifications = append(s.notifications, n)
}

// GetRecentNotifications returns the most recent N notifications in reverse chronological order.
func (s *Store) GetRecentNotifications(limit int) []Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.notifications)
	if limit <= 0 || limit > total {
		limit = total
	}

	result := make([]Notification, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, s.notifications[total-1-i])
	}
	return result
}

// GetStats returns a copy of the current aggregate statistics.
func (s *Store) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := s.stats
	cp.BySource = make(map[message.Source]int, len(s.stats.BySource))
	for k, v := range s.stats.BySource {
		cp.BySource[k] = v
	}
	return cp
}

// GetActionItems returns the most recent action items along with their source message context.
func (s *Store) GetActionItems(limit int) []ActionItemWithContext {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = s.count
	}

	var result []ActionItemWithContext
	for i := 0; i < s.count && len(result) < limit; i++ {
		idx := (s.writeIdx - 1 - i + s.capacity) % s.capacity
		pm := s.messages[idx]
		if pm.Classification == nil {
			continue
		}
		for j, item := range pm.Classification.ActionItems {
			if len(result) >= limit {
				break
			}
			result = append(result, ActionItemWithContext{
				Item:         item,
				SourceMsg:    pm.Message,
				EventCreated: j < pm.EventsCreated,
				ProcessedAt:  pm.ProcessedAt,
			})
		}
	}
	return result
}

// Subscribe registers a new SSE subscriber and returns a channel that will receive
// event strings. The caller must eventually call Unsubscribe to avoid leaking resources.
func (s *Store) Subscribe() chan string {
	ch := make(chan string, 16)
	s.ssemu.Lock()
	s.subscribers[ch] = struct{}{}
	s.ssemu.Unlock()
	return ch
}

// Unsubscribe removes an SSE subscriber and closes its channel.
func (s *Store) Unsubscribe(ch chan string) {
	s.ssemu.Lock()
	delete(s.subscribers, ch)
	s.ssemu.Unlock()
	close(ch)
}

// notifySubscribers sends an event string to all SSE subscribers. Slow subscribers
// that have a full channel buffer are skipped to avoid blocking.
func (s *Store) notifySubscribers(event string) {
	s.ssemu.Lock()
	defer s.ssemu.Unlock()

	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			// Skip slow subscribers to avoid blocking.
		}
	}
}
