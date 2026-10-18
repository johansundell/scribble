package logqueue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// fakeStore records batches. write, when set, decides each call's result.
type fakeStore struct {
	store.Store
	mu      sync.Mutex
	batches [][]types.UsageLog
	calls   int
	write   func(ctx context.Context, call int) error
}

func (f *fakeStore) LogRequests(ctx context.Context, entries []types.UsageLog) error {
	f.mu.Lock()
	f.calls++
	call := f.calls
	write := f.write
	f.mu.Unlock()
	if write != nil {
		if err := write(ctx, call); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.batches = append(f.batches, append([]types.UsageLog(nil), entries...))
	f.mu.Unlock()
	return nil
}

func (f *fakeStore) snapshot() (sizes []int, total, calls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.batches {
		sizes = append(sizes, len(b))
		total += len(b)
	}
	return sizes, total, f.calls
}

type logEntry struct{ level, msg string }

type testLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func (l *testLogger) add(level, format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level, fmt.Sprintf(format, v...)})
}
func (l *testLogger) Infof(f string, v ...interface{})    { l.add("INFO", f, v...) }
func (l *testLogger) Warningf(f string, v ...interface{}) { l.add("WARN", f, v...) }
func (l *testLogger) Errorf(f string, v ...interface{})   { l.add("ERROR", f, v...) }

func (l *testLogger) find(level, substr string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, e := range l.entries {
		if e.level == level && strings.Contains(e.msg, substr) {
			out = append(out, e.msg)
		}
	}
	return out
}

// startQueue builds a queue with fast timings; tweak adjusts it before start.
func startQueue(t *testing.T, s store.Store, l Logger, size int, tweak func(*Queue)) *Queue {
	t.Helper()
	q := newQueue(s, l, size)
	q.flushInterval = time.Hour
	q.retryStart = time.Millisecond
	q.retryBudget = 20 * time.Millisecond
	q.writeTimeout = time.Second
	if tweak != nil {
		tweak(q)
	}
	go q.run()
	return q
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func entry(i int) types.UsageLog {
	return types.UsageLog{Endpoint: fmt.Sprintf("/e%d", i), CreatedAt: time.Now()}
}

func TestQueue_WritesFullBatches(t *testing.T) {
	fs := &fakeStore{}
	q := startQueue(t, fs, &testLogger{}, 1000, nil)
	defer q.Close(context.Background())

	for i := 0; i < 2*batchSize; i++ {
		q.Enqueue(entry(i))
	}
	waitFor(t, "two full batches", func() bool { _, total, _ := fs.snapshot(); return total == 2*batchSize })
	if sizes, _, _ := fs.snapshot(); len(sizes) != 2 || sizes[0] != batchSize || sizes[1] != batchSize {
		t.Errorf("expected two batches of %d, got %v", batchSize, sizes)
	}
}

func TestQueue_FlushesPartialBatchOnInterval(t *testing.T) {
	fs := &fakeStore{}
	q := startQueue(t, fs, &testLogger{}, 1000, func(q *Queue) { q.flushInterval = 10 * time.Millisecond })
	defer q.Close(context.Background())

	for i := 0; i < 3; i++ {
		q.Enqueue(entry(i))
	}
	waitFor(t, "interval flush", func() bool { _, total, _ := fs.snapshot(); return total == 3 })
	if sizes, _, _ := fs.snapshot(); len(sizes) != 1 {
		t.Errorf("expected one batch of 3, got %v", sizes)
	}
}

func TestQueue_DropsWhenFullAndWarnsOncePerMinute(t *testing.T) {
	release := make(chan struct{})
	fs := &fakeStore{write: func(ctx context.Context, call int) error {
		if call == 1 {
			<-release // hold the worker so the queue fills up
		}
		return nil
	}}
	tl := &testLogger{}
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	q := startQueue(t, fs, tl, 2, func(q *Queue) {
		q.batchSize = 1
		q.now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock }
	})

	q.Enqueue(entry(0)) // taken by the worker, which blocks in the store
	waitFor(t, "worker busy", func() bool { _, _, calls := fs.snapshot(); return calls == 1 })
	q.Enqueue(entry(1)) // queued
	q.Enqueue(entry(2)) // queued, queue now full
	for i := 3; i < 6; i++ {
		q.Enqueue(entry(i)) // dropped
	}

	warns := tl.find("WARN", "queue full")
	if len(warns) != 1 || !strings.Contains(warns[0], "dropped 1 entries") {
		t.Fatalf("expected one warning for the first drop, got %v", warns)
	}

	clockMu.Lock()
	clock = clock.Add(61 * time.Second)
	clockMu.Unlock()
	q.Enqueue(entry(6)) // dropped; a minute has passed, so it warns with the count since

	warns = tl.find("WARN", "queue full")
	if len(warns) != 2 || !strings.Contains(warns[1], "dropped 3 entries") {
		t.Fatalf("expected a second warning with 3 drops, got %v", warns)
	}

	close(release)
	if err := q.Close(context.Background()); err != nil {
		t.Fatalf("expected Close to succeed, got %v", err)
	}
	if _, total, _ := fs.snapshot(); total != 3 {
		t.Errorf("expected the 3 accepted entries to be written, got %d", total)
	}
}

func TestQueue_RetriesThenDrops(t *testing.T) {
	fs := &fakeStore{write: func(context.Context, int) error { return errors.New("connection refused") }}
	tl := &testLogger{}
	q := startQueue(t, fs, tl, 1000, func(q *Queue) { q.batchSize = 1 })
	defer q.Close(context.Background())

	q.Enqueue(entry(0))
	waitFor(t, "batch dropped", func() bool { return len(tl.find("ERROR", "after retrying")) == 1 })

	// retryStart 1ms doubling within a 20ms budget: waits of 1, 2, 4, 8, and 5ms = 20ms total.
	// So 1 try + 5 retries = 6 attempts.
	if _, _, calls := fs.snapshot(); calls != 6 {
		t.Errorf("expected 6 attempts, got %d", calls)
	}
	if n := len(tl.find("WARN", "retrying")); n != 5 {
		t.Errorf("expected 5 retry warnings, got %d", n)
	}
}

func TestQueue_RetriedBatchSucceeds(t *testing.T) {
	fs := &fakeStore{write: func(_ context.Context, call int) error {
		if call < 3 {
			return errors.New("temporarily unavailable")
		}
		return nil
	}}
	q := startQueue(t, fs, &testLogger{}, 1000, func(q *Queue) { q.batchSize = 1 })
	defer q.Close(context.Background())

	q.Enqueue(entry(0))
	waitFor(t, "batch written after retries", func() bool { _, total, _ := fs.snapshot(); return total == 1 })
}

func TestQueue_PermanentErrorDropsAtOnce(t *testing.T) {
	fs := &fakeStore{write: func(context.Context, int) error { return store.Permanent(errors.New("400 bad record")) }}
	tl := &testLogger{}
	q := startQueue(t, fs, tl, 1000, func(q *Queue) { q.batchSize = 1 })
	defer q.Close(context.Background())

	q.Enqueue(entry(0))
	waitFor(t, "batch dropped", func() bool { return len(tl.find("ERROR", "400 bad record")) == 1 })
	if _, _, calls := fs.snapshot(); calls != 1 {
		t.Errorf("expected a single attempt, got %d", calls)
	}
}

func TestQueue_CloseDrainsQueuedEntries(t *testing.T) {
	fs := &fakeStore{}
	q := startQueue(t, fs, &testLogger{}, 1000, nil)

	for i := 0; i < 120; i++ {
		q.Enqueue(entry(i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil {
		t.Fatalf("expected Close to succeed, got %v", err)
	}
	if _, total, _ := fs.snapshot(); total != 120 {
		t.Errorf("expected all 120 entries written on Close, got %d", total)
	}
}

func TestQueue_CloseStopsAtDeadline(t *testing.T) {
	// A store that hangs until its context ends.
	fs := &fakeStore{write: func(ctx context.Context, _ int) error { <-ctx.Done(); return ctx.Err() }}
	tl := &testLogger{}
	q := startQueue(t, fs, tl, 1000, func(q *Queue) { q.retryBudget = time.Minute })

	for i := 0; i < 10; i++ {
		q.Enqueue(entry(i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := q.Close(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("expected Close to return near its 50ms deadline, took %v", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "dropped 10 entries") {
		t.Errorf("expected Close to report 10 dropped entries, got %v", err)
	}
	if len(tl.find("ERROR", "dropped 10 entries at shutdown")) != 1 {
		t.Errorf("expected the drop to be logged as an error, got %v", tl.entries)
	}
}

func TestQueue_CloseInterruptsRetryWait(t *testing.T) {
	// Always fails, with a long retry schedule: Close must not wait it out.
	fs := &fakeStore{write: func(context.Context, int) error { return errors.New("down") }}
	q := startQueue(t, fs, &testLogger{}, 1000, func(q *Queue) {
		q.batchSize = 1
		q.retryStart = time.Hour
		q.retryBudget = 2 * time.Hour
	})

	q.Enqueue(entry(0))
	waitFor(t, "first attempt", func() bool { _, _, calls := fs.snapshot(); return calls == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := q.Close(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("expected Close to return near its deadline, took %v", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "dropped 1 entries") {
		t.Errorf("expected Close to report the batch in retry as dropped, got %v", err)
	}
}

func TestQueue_EnqueueAfterCloseDoesNotPanic(t *testing.T) {
	q := startQueue(t, &fakeStore{}, &testLogger{}, 1000, nil)
	if err := q.Close(context.Background()); err != nil {
		t.Fatalf("expected Close to succeed, got %v", err)
	}
	q.Enqueue(entry(0))
	if err := q.Close(context.Background()); err != nil {
		t.Errorf("expected a second Close to succeed, got %v", err)
	}
}

func TestQueue_LogsBatchesOnlyWhenAsked(t *testing.T) {
	for _, logBatches := range []bool{false, true} {
		tl := &testLogger{}
		fs := &fakeStore{}
		q := startQueue(t, fs, tl, 1000, func(q *Queue) { q.batchSize = 1; q.logBatches = logBatches })
		q.Enqueue(entry(0))
		waitFor(t, "batch written", func() bool { _, total, _ := fs.snapshot(); return total == 1 })
		q.Close(context.Background())

		if got := len(tl.find("INFO", "persisted 1 request log entries")); got != map[bool]int{false: 0, true: 1}[logBatches] {
			t.Errorf("logBatches=%v: expected %d persisted lines, got %d", logBatches, map[bool]int{false: 0, true: 1}[logBatches], got)
		}
	}
}
