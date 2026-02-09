package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/pitabwire/frame/queue"
	"github.com/rs/xid"
)

// Publisher wraps frame's queue manager to emit typed events.
// It also supports local in-process subscriptions for event streaming.
type Publisher struct {
	queueMgr queue.Manager
	source   string
	queueRef string

	subMu       sync.RWMutex
	subscribers map[string]chan Envelope
}

// NewPublisher creates a publisher that emits events to the given queue reference.
func NewPublisher(queueMgr queue.Manager, source string, queueRef string) *Publisher {
	return &Publisher{
		queueMgr:    queueMgr,
		source:      source,
		queueRef:    queueRef,
		subscribers: make(map[string]chan Envelope),
	}
}

// Emit publishes a typed event to the event bus and fans out to local subscribers.
func (p *Publisher) Emit(ctx context.Context, eventType EventType, sessionID string, data interface{}) error {
	envelope := Envelope{
		ID:        xid.New().String(),
		Type:      eventType,
		Source:    p.source,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	envelope.Data = raw

	// Fan out to local subscribers (non-blocking).
	p.subMu.RLock()
	for id, ch := range p.subscribers {
		select {
		case ch <- envelope:
		default:
			slog.Warn("event dropped: subscriber buffer full",
				slog.String("subscriber", id), slog.String("event_type", string(eventType)))
		}
	}
	p.subMu.RUnlock()

	return p.queueMgr.Publish(ctx, p.queueRef, envelope)
}

// Subscribe creates a local in-process subscription for events.
// Returns a channel that receives Envelope values.
// The caller must call Unsubscribe with the same id to clean up.
func (p *Publisher) Subscribe(id string, bufSize int) <-chan Envelope {
	if bufSize <= 0 {
		bufSize = 64
	}
	ch := make(chan Envelope, bufSize)
	p.subMu.Lock()
	p.subscribers[id] = ch
	p.subMu.Unlock()
	return ch
}

// Unsubscribe removes a local subscription and closes its channel.
func (p *Publisher) Unsubscribe(id string) {
	p.subMu.Lock()
	if ch, ok := p.subscribers[id]; ok {
		close(ch)
		delete(p.subscribers, id)
	}
	p.subMu.Unlock()
}
