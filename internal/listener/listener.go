package listener

import (
	"context"

	"github.com/emirlan/notifylm/internal/message"
)

// Listener defines the interface for all message source listeners.
type Listener interface {
	// Name returns the name of the listener for logging.
	Name() string

	// Start begins listening for messages and sends them to the output channel.
	// It should block until the context is cancelled.
	Start(ctx context.Context, out chan<- *message.Message) error

	// Stop gracefully shuts down the listener.
	Stop() error
}

// BaseListener provides common functionality for listeners.
type BaseListener struct {
	name    string
	stopped bool
}

func NewBaseListener(name string) BaseListener {
	return BaseListener{name: name}
}

func (b *BaseListener) Name() string {
	return b.name
}
