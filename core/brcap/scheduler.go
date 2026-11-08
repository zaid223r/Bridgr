package brcap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Scheduler runs work in the background, possibly later and with retries. Work
// is identified by a handler name plus a payload of bytes rather than a Go
// closure, so a database-backed jobs module can persist it and run it on
// another instance.
type Scheduler interface {
	// Handle registers the function that runs jobs named name. Registering the
	// same name twice replaces the handler.
	Handle(name string, fn func(ctx context.Context, payload []byte) error)
	// Enqueue schedules one run of the handler for name.
	Enqueue(ctx context.Context, name string, payload []byte, opts EnqueueOptions) error
}

// EnqueueOptions control when and how often a job runs.
type EnqueueOptions struct {
	// At is the earliest time to run; zero means now.
	At time.Time
	// MaxAttempts is how many times to try before giving up. Default 1.
	MaxAttempts int
	// Backoff is the wait before the first retry, doubled for each later one.
	// Default 1s.
	Backoff time.Duration
}

var (
	// ErrNoHandler is returned by Enqueue for a name with no handler.
	ErrNoHandler = errors.New("brcap: no handler registered for job")
	// ErrStopped is returned by Enqueue after Stop.
	ErrStopped = errors.New("brcap: scheduler stopped")
)

// MemoryScheduler runs jobs in goroutines of the current process. Jobs are lost
// if the process exits. It needs no Start; call Stop on shutdown to cancel
// pending jobs and wait for running ones.
type MemoryScheduler struct {
	mu       sync.Mutex
	handlers map[string]func(context.Context, []byte) error
	stopped  bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	// OnError, if set, is called when a job fails its last attempt.
	OnError func(name string, err error)
}

// NewMemoryScheduler returns a MemoryScheduler with no handlers.
func NewMemoryScheduler() *MemoryScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &MemoryScheduler{handlers: map[string]func(context.Context, []byte) error{}, ctx: ctx, cancel: cancel}
}

// Handle registers fn for jobs named name.
func (s *MemoryScheduler) Handle(name string, fn func(ctx context.Context, payload []byte) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[name] = fn
}

// Enqueue schedules a run of the handler for name. The payload is copied.
func (s *MemoryScheduler) Enqueue(ctx context.Context, name string, payload []byte, opts EnqueueOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return ErrStopped
	}
	fn, ok := s.handlers[name]
	if !ok {
		return fmt.Errorf("%w %q", ErrNoHandler, name)
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.Backoff <= 0 {
		opts.Backoff = time.Second
	}
	payload = append([]byte(nil), payload...)
	s.wg.Add(1)
	go s.run(name, fn, payload, opts)
	return nil
}

func (s *MemoryScheduler) run(name string, fn func(context.Context, []byte) error, payload []byte, opts EnqueueOptions) {
	defer s.wg.Done()
	if !s.sleep(time.Until(opts.At)) {
		return
	}
	wait := opts.Backoff
	var err error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err = fn(s.ctx, payload); err == nil {
			return
		}
		if attempt < opts.MaxAttempts {
			if !s.sleep(wait) {
				return
			}
			wait *= 2
		}
	}
	if s.OnError != nil {
		s.OnError(name, fmt.Errorf("after %d attempts: %w", opts.MaxAttempts, err))
	}
}

// sleep waits for d or until Stop; it reports whether the job should continue.
func (s *MemoryScheduler) sleep(d time.Duration) bool {
	if d <= 0 {
		return s.ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// Stop cancels jobs that haven't started, cancels the context passed to
// running ones and waits for them, until ctx is done. Calling it again is a
// no-op.
func (s *MemoryScheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.cancel()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("brcap: waiting for running jobs: %w", ctx.Err())
	}
}
