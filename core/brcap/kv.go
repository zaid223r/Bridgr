package brcap

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// KV is a key-value store with expiry, shared by modules such as caching and
// rate limiting. A Redis-backed module can provide it so every instance of an
// app sees the same data. A ttl of 0 means the key doesn't expire.
type KV interface {
	// Get returns the value of key and whether it exists.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Set stores value under key.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes key; deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// Incr adds delta to the counter at key, creating it at 0, and returns the
	// new value. ttl applies only when the counter is created, which is what
	// fixed-window rate limits need.
	Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
}

// ErrNotCounter is returned by Incr when the key holds a value Set wrote.
var ErrNotCounter = errors.New("brcap: key does not hold a counter")

// MemoryKV is an in-process KV. Expired keys are removed when accessed.
type MemoryKV struct {
	mu   sync.Mutex
	data map[string]kvEntry
	now  func() time.Time
}

type kvEntry struct {
	value   []byte
	counter bool
	expires time.Time // zero: never
}

// NewMemoryKV returns an empty MemoryKV.
func NewMemoryKV() *MemoryKV {
	return &MemoryKV{data: map[string]kvEntry{}, now: time.Now}
}

func (m *MemoryKV) live(key string) (kvEntry, bool) {
	e, ok := m.data[key]
	if ok && !e.expires.IsZero() && !m.now().Before(e.expires) {
		delete(m.data, key)
		return kvEntry{}, false
	}
	return e, ok
}

func (m *MemoryKV) expiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return m.now().Add(ttl)
}

// Get returns a copy of the value of key.
func (m *MemoryKV) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.live(key)
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), e.value...), true, nil
}

// Set stores a copy of value.
func (m *MemoryKV) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = kvEntry{value: append([]byte(nil), value...), expires: m.expiry(ttl)}
	return nil
}

// Delete removes key.
func (m *MemoryKV) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// Incr adds delta to the counter at key. Counters read through Get return
// their value as 8 big-endian bytes.
func (m *MemoryKV) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.live(key)
	if ok && !e.counter {
		return 0, ErrNotCounter
	}
	var n int64
	if ok {
		n = int64(binary.BigEndian.Uint64(e.value)) //nolint:gosec // round-trips the int64 written below
	} else {
		e = kvEntry{counter: true, expires: m.expiry(ttl)}
	}
	n += delta
	e.value = binary.BigEndian.AppendUint64(nil, uint64(n)) //nolint:gosec // stored as raw bits, read back as int64
	m.data[key] = e
	return n, nil
}
