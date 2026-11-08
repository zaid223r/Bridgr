package brcap

import (
	"context"
	"slices"
	"sync"
	"time"
)

// Event actions published for CRUD resources.
const (
	Created = "created"
	Updated = "updated"
	Deleted = "deleted"
)

// Event describes a change to a record. CRUD resources publish one after each
// successful create, replace, update and delete; modules such as webhooks or
// audit logs subscribe.
type Event struct {
	// Resource is the resource path, e.g. "todos".
	Resource string
	// Action is Created, Updated or Deleted.
	Action string
	// ID is the record's primary key as it appears in URLs.
	ID string
	// Data is the record after the change; nil for Deleted.
	Data any
	// At is when the change was committed.
	At time.Time
	// RequestID is the ID of the request that made the change.
	RequestID string
}

// EventBus delivers events to subscribers. Publish is called on the request
// path after the change is committed, so subscribers must return quickly: hand
// slow work (sending webhooks, emails) to a Scheduler.
type EventBus interface {
	Publish(ctx context.Context, e Event)
	// Subscribe registers fn for every published event and returns a function
	// that removes it.
	Subscribe(fn func(ctx context.Context, e Event)) (unsubscribe func())
}

// MemoryEventBus delivers events synchronously, in subscription order, within
// the current process.
type MemoryEventBus struct {
	mu   sync.RWMutex
	subs map[int]func(context.Context, Event)
	next int
	// OnPanic, if set, receives the value of a subscriber's panic. Panics are
	// always recovered: the change is already committed and the request must
	// not fail because of a subscriber.
	OnPanic func(e Event, v any)
}

// NewMemoryEventBus returns a bus with no subscribers.
func NewMemoryEventBus() *MemoryEventBus {
	return &MemoryEventBus{subs: map[int]func(context.Context, Event){}}
}

// Publish calls every subscriber with e.
func (b *MemoryEventBus) Publish(ctx context.Context, e Event) {
	b.mu.RLock()
	ids := make([]int, 0, len(b.subs))
	for id := range b.subs {
		ids = append(ids, id)
	}
	b.mu.RUnlock()
	slices.Sort(ids) // subscription order: ids only grow
	for _, id := range ids {
		b.mu.RLock()
		fn, ok := b.subs[id]
		b.mu.RUnlock()
		if ok {
			b.deliver(ctx, fn, e)
		}
	}
}

func (b *MemoryEventBus) deliver(ctx context.Context, fn func(context.Context, Event), e Event) {
	defer func() {
		if v := recover(); v != nil && b.OnPanic != nil {
			b.OnPanic(e, v)
		}
	}()
	fn(ctx, e)
}

// Subscribe registers fn and returns a function that removes it.
func (b *MemoryEventBus) Subscribe(fn func(ctx context.Context, e Event)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	b.subs[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
	}
}
