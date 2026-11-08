package brcap

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	_ KV        = (*MemoryKV)(nil)
	_ Scheduler = (*MemoryScheduler)(nil)
)

func TestPrincipal(t *testing.T) {
	t.Parallel()
	if _, ok := PrincipalFrom(t.Context()); ok {
		t.Error("empty context has a principal")
	}
	ctx := WithPrincipal(t.Context(), Principal{ID: "u1", Roles: []string{"admin"}, Attrs: map[string]string{"tenant": "t9"}})
	p, ok := PrincipalFrom(ctx)
	if !ok || p.ID != "u1" || p.Attrs["tenant"] != "t9" {
		t.Errorf("PrincipalFrom = %+v, %v", p, ok)
	}
	if !p.HasRole("admin") || p.HasRole("owner") {
		t.Error("HasRole wrong")
	}
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newKV() (*MemoryKV, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	kv := NewMemoryKV()
	kv.now = clock.Now
	return kv, clock
}

func TestMemoryKV(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	kv, clock := newKV()

	if _, ok, err := kv.Get(ctx, "missing"); ok || err != nil {
		t.Errorf("missing key: ok=%v err=%v", ok, err)
	}

	value := []byte("v1")
	_ = kv.Set(ctx, "k", value, 0)
	value[0] = 'X' // the store must have copied it
	got, ok, _ := kv.Get(ctx, "k")
	if !ok || string(got) != "v1" {
		t.Fatalf("Get = %q, %v", got, ok)
	}
	got[0] = 'Y' // and return a copy
	if again, _, _ := kv.Get(ctx, "k"); string(again) != "v1" {
		t.Error("Get returned the stored slice instead of a copy")
	}

	_ = kv.Set(ctx, "short", []byte("x"), time.Minute)
	clock.Advance(59 * time.Second)
	if _, ok, _ := kv.Get(ctx, "short"); !ok {
		t.Error("key expired early")
	}
	clock.Advance(time.Second)
	if _, ok, _ := kv.Get(ctx, "short"); ok {
		t.Error("key outlived its ttl")
	}
	if _, ok, _ := kv.Get(ctx, "k"); !ok {
		t.Error("ttl 0 key expired")
	}

	_ = kv.Delete(ctx, "k")
	_ = kv.Delete(ctx, "never-existed")
	if _, ok, _ := kv.Get(ctx, "k"); ok {
		t.Error("Delete didn't remove the key")
	}
}

func TestMemoryKVIncr(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	kv, clock := newKV()

	for i, want := range []int64{1, 2, 7} {
		delta := int64(1)
		if i == 2 {
			delta = 5
		}
		if n, err := kv.Incr(ctx, "hits", delta, time.Minute); err != nil || n != want {
			t.Fatalf("Incr #%d = %d, %v; want %d", i, n, err, want)
		}
	}
	clock.Advance(30 * time.Second)
	if n, _ := kv.Incr(ctx, "hits", 1, time.Hour); n != 8 {
		t.Errorf("Incr = %d, want 8", n)
	}
	clock.Advance(31 * time.Second) // the ttl set on creation still applies
	if n, _ := kv.Incr(ctx, "hits", 1, time.Minute); n != 1 {
		t.Errorf("after expiry Incr = %d, want a fresh counter at 1", n)
	}
	if n, _ := kv.Incr(ctx, "neg", -3, 0); n != -3 {
		t.Errorf("negative delta = %d", n)
	}

	_ = kv.Set(ctx, "text", []byte("abc"), 0)
	if _, err := kv.Incr(ctx, "text", 1, 0); !errors.Is(err, ErrNotCounter) {
		t.Errorf("Incr on a Set value: %v", err)
	}
}

func TestMemoryKVConcurrentIncr(t *testing.T) {
	t.Parallel()
	kv := NewMemoryKV()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { _, _ = kv.Incr(t.Context(), "n", 1, 0) })
	}
	wg.Wait()
	if n, _ := kv.Incr(t.Context(), "n", 0, 0); n != 100 {
		t.Errorf("counter = %d after 100 concurrent increments", n)
	}
}

func TestMemoryKVCancelledContext(t *testing.T) {
	t.Parallel()
	kv := NewMemoryKV()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := kv.Get(ctx, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get: %v", err)
	}
	if err := kv.Set(ctx, "k", nil, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("Set: %v", err)
	}
	if _, err := kv.Incr(ctx, "k", 1, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("Incr: %v", err)
	}
}

func TestMemorySchedulerRunsJobs(t *testing.T) {
	t.Parallel()
	s := NewMemoryScheduler()
	defer s.Stop(context.Background()) //nolint:errcheck // test cleanup

	got := make(chan string, 2)
	s.Handle("greet", func(_ context.Context, payload []byte) error {
		got <- string(payload)
		return nil
	})
	payload := []byte("now")
	if err := s.Enqueue(t.Context(), "greet", payload, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X' // the scheduler copied it
	start := time.Now()
	if err := s.Enqueue(t.Context(), "greet", []byte("later"), EnqueueOptions{At: start.Add(50 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}

	first, second := <-got, <-got
	if first != "now" || second != "later" {
		t.Errorf("runs = %q, %q", first, second)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("delayed job ran after %s, before its At", elapsed)
	}
}

func TestMemorySchedulerRetries(t *testing.T) {
	t.Parallel()
	s := NewMemoryScheduler()
	defer s.Stop(context.Background()) //nolint:errcheck // test cleanup

	var attempts atomic.Int64
	var mu sync.Mutex
	var times []time.Time
	done := make(chan error, 1)
	s.OnError = func(name string, err error) { done <- errors.New(name + ": " + err.Error()) }
	s.Handle("flaky", func(context.Context, []byte) error {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		if attempts.Add(1) < 3 {
			return errors.New("try again")
		}
		done <- nil
		return nil
	})
	s.Handle("broken", func(context.Context, []byte) error { return errors.New("always") })

	_ = s.Enqueue(t.Context(), "flaky", nil, EnqueueOptions{MaxAttempts: 5, Backoff: 20 * time.Millisecond})
	if err := <-done; err != nil {
		t.Fatalf("flaky job: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
	mu.Lock()
	gap1, gap2 := times[1].Sub(times[0]), times[2].Sub(times[1])
	mu.Unlock()
	if gap1 < 20*time.Millisecond || gap2 < 40*time.Millisecond {
		t.Errorf("backoff gaps %s, %s; want at least 20ms then 40ms", gap1, gap2)
	}

	_ = s.Enqueue(t.Context(), "broken", nil, EnqueueOptions{MaxAttempts: 2, Backoff: time.Millisecond})
	if err := <-done; err == nil || err.Error() != "broken: after 2 attempts: always" {
		t.Errorf("OnError = %v", err)
	}
}

func TestMemorySchedulerErrorsAndStop(t *testing.T) {
	t.Parallel()
	s := NewMemoryScheduler()
	if err := s.Enqueue(t.Context(), "nobody", nil, EnqueueOptions{}); !errors.Is(err, ErrNoHandler) {
		t.Errorf("unknown handler: %v", err)
	}

	var ran atomic.Int64
	running := make(chan struct{})
	s.Handle("pending", func(context.Context, []byte) error { ran.Add(1); return nil })
	s.Handle("slow", func(ctx context.Context, _ []byte) error {
		close(running)
		<-ctx.Done() // Stop cancels the context of running jobs
		ran.Add(10)
		return ctx.Err()
	})
	_ = s.Enqueue(t.Context(), "pending", nil, EnqueueOptions{At: time.Now().Add(time.Hour)})
	_ = s.Enqueue(t.Context(), "slow", nil, EnqueueOptions{})
	<-running

	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := ran.Load(); got != 10 {
		t.Errorf("ran = %d: the pending job must be cancelled and the running one finished", got)
	}
	if err := s.Enqueue(t.Context(), "pending", nil, EnqueueOptions{}); !errors.Is(err, ErrStopped) {
		t.Errorf("Enqueue after Stop: %v", err)
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

func TestMemorySchedulerStopTimeout(t *testing.T) {
	t.Parallel()
	s := NewMemoryScheduler()
	release := make(chan struct{})
	started := make(chan struct{})
	s.Handle("stuck", func(context.Context, []byte) error {
		close(started)
		<-release // ignores cancellation
		return nil
	})
	_ = s.Enqueue(t.Context(), "stuck", nil, EnqueueOptions{})
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Stop = %v, want a deadline error", err)
	}
	close(release)
}

func TestSchedulerHandlerReplacement(t *testing.T) {
	t.Parallel()
	s := NewMemoryScheduler()
	defer s.Stop(context.Background()) //nolint:errcheck // test cleanup
	got := make(chan string, 1)
	s.Handle("job", func(context.Context, []byte) error { got <- "old"; return nil })
	s.Handle("job", func(context.Context, []byte) error { got <- "new"; return nil })
	_ = s.Enqueue(t.Context(), "job", nil, EnqueueOptions{})
	if v := <-got; v != "new" {
		t.Errorf("ran %q handler", v)
	}
}
