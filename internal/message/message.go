package message

import "time"

// Source represents the origin platform of a message.
type Source string

const (
	SourceWhatsApp Source = "whatsapp"
	SourceTelegram Source = "telegram"
	SourceSlack    Source = "slack"
	SourceGmail    Source = "gmail"
)

// Message represents a unified message from any source.
type Message struct {
	ID        string
	Source    Source
	Sender    string
	Text      string
	Timestamp time.Time
	Metadata  map[string]string
}

// NewMessage creates a new message with the given parameters.
func NewMessage(source Source, sender, text string) *Message {
	return &Message{
		Source:    source,
		Sender:    sender,
		Text:      text,
		Timestamp: time.Now(),
		Metadata:  make(map[string]string),
	}
}
