// Package logqueue writes request logs to a store in the background, so a
// slow or unavailable store never slows down or fails a request.
package logqueue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johansundell/scribble/logging"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

const (
	queueSize     = 1000             // entries waiting to be written; more are dropped
	batchSize     = 50               // entries per write
	flushInterval = time.Second      // longest an entry waits for a batch to fill
	retryStart    = time.Second      // first retry delay, doubled per attempt
	retryBudget   = 60 * time.Second // total retry delay before a batch is dropped
	writeTimeout  = 10 * time.Second // deadline for a single write attempt
	dropWarnEvery = time.Minute      // at most one queue-full warning per interval
)

// Logger is the leveled logger the queue reports to.
type Logger = logging.Logger

// Queue buffers request log entries and writes them to a store in batches
// from a single worker goroutine.
type Queue struct {
	store      store.Store
	log        Logger
	logBatches bool // log each successfully written batch (Info)
	entries    chan types.UsageLog

	stopping chan struct{} // closed by Close: drain and exit
	drainCtx context.Context
	done     chan struct{} // closed when the worker has exited
	stopOnce sync.Once
	lost     int // entries dropped while draining; read after done

	dropped  atomic.Int64 // queue-full drops since the last warning
	lastWarn atomic.Int64 // unix nanos of the last queue-full warning

	// Tunables, overridden by tests.
	batchSize     int
	flushInterval time.Duration
	retryStart    time.Duration
	retryBudget   time.Duration
	writeTimeout  time.Duration
	now           func() time.Time
}

// New starts a queue that writes to s and reports through l. Drops and
// failures are always logged; each successfully written batch only when
// logBatches is true (the service passes DEBUG).
func New(s store.Store, l Logger, logBatches bool) *Queue {
	q := newQueue(s, l, queueSize)
	q.logBatches = logBatches
	go q.run()
	return q
}

func newQueue(s store.Store, l Logger, size int) *Queue {
	return &Queue{
		store:         s,
		log:           l,
		entries:       make(chan types.UsageLog, size),
		stopping:      make(chan struct{}),
		done:          make(chan struct{}),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		retryStart:    retryStart,
		retryBudget:   retryBudget,
		writeTimeout:  writeTimeout,
		now:           time.Now,
	}
}

// Enqueue adds an entry without blocking. When the queue is full, or the
// queue is closing, the entry is dropped and counted.
func (q *Queue) Enqueue(e types.UsageLog) {
	select {
	case <-q.stopping:
		q.drop()
		return
	default:
	}
	select {
	case q.entries <- e:
	default:
		q.drop()
	}
}

func (q *Queue) drop() {
	q.dropped.Add(1)
	q.warnDrops()
}

// warnDrops logs the queue-full drops since the last warning, at most once
// per dropWarnEvery.
func (q *Queue) warnDrops() {
	now := q.now().UnixNano()
	last := q.lastWarn.Load()
	if last != 0 && time.Duration(now-last) < dropWarnEvery {
		return
	}
	if !q.lastWarn.CompareAndSwap(last, now) {
		return
	}
	if n := q.dropped.Swap(0); n > 0 {
		q.log.Warningf("request log queue full: dropped %d entries", n)
	}
}

// Close stops accepting entries and writes what is queued until ctx is done.
// Call it after the HTTP server has shut down, so no requests still enqueue.
// Entries that could not be written in time are dropped; Close logs and
// returns an error with their count.
func (q *Queue) Close(ctx context.Context) error {
	q.stopOnce.Do(func() {
		q.drainCtx = ctx
		close(q.stopping)
	})
	<-q.done
	if q.lost > 0 {
		err := fmt.Errorf("request log queue: dropped %d entries at shutdown", q.lost)
		q.log.Errorf("%v", err)
		return err
	}
	return nil
}

func (q *Queue) run() {
	defer close(q.done)

	// ctx is cancelled when the drain deadline passes, so no write or retry
	// outlives it; until Close it never ends.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-q.stopping:
			select {
			case <-q.drainCtx.Done():
				cancel()
			case <-ctx.Done():
			}
		case <-ctx.Done():
		}
	}()

	ticker := time.NewTicker(q.flushInterval)
	defer ticker.Stop()

	batch := make([]types.UsageLog, 0, q.batchSize)
	flush := func() {
		if len(batch) > 0 {
			if !q.write(ctx, batch) && ctx.Err() != nil {
				q.lost += len(batch)
			}
			batch = batch[:0]
		}
	}

	for {
		select {
		case e := <-q.entries:
			batch = append(batch, e)
			if len(batch) >= q.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
			q.warnDrops() // report drops even when no new entries arrive
		case <-q.stopping:
			q.drain(ctx, batch)
			return
		}
	}
}

// drain writes the pending batch and everything still queued until ctx ends,
// then counts whatever is left as lost.
func (q *Queue) drain(ctx context.Context, batch []types.UsageLog) {
	for {
	fill:
		for len(batch) < q.batchSize {
			select {
			case e := <-q.entries:
				batch = append(batch, e)
			default:
				break fill
			}
		}
		if len(batch) == 0 {
			return
		}
		if ctx.Err() != nil || (!q.write(ctx, batch) && ctx.Err() != nil) {
			q.lost += len(batch) + len(q.entries)
			return
		}
		batch = batch[:0]
	}
}

// write persists a batch, retrying temporary failures with backoff until the
// retry budget or ctx runs out. It reports whether the batch was written. A
// batch dropped for its error is logged here; when ctx ended, the caller
// counts it instead.
func (q *Queue) write(ctx context.Context, batch []types.UsageLog) bool {
	delay := q.retryStart
	var waited time.Duration
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, q.writeTimeout)
		err := q.store.LogRequests(attemptCtx, batch)
		cancel()
		if err == nil {
			if q.logBatches {
				q.log.Infof("persisted %d request log entries", len(batch))
			}
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		if store.IsPermanent(err) {
			if len(batch) > 1 {
				q.log.Warningf("batch failed permanently, retrying %d entries one by one: %v", len(batch), err)
				for _, e := range batch {
					q.write(ctx, []types.UsageLog{e})
				}
				return true
			}
			q.log.Errorf("dropped 1 request log entry: %v", err)
			return false
		}
		if waited >= q.retryBudget {
			q.log.Errorf("dropped %d request log entries after retrying for %v: %v", len(batch), waited, err)
			return false
		}
		sleep := delay
		if waited+sleep > q.retryBudget {
			sleep = q.retryBudget - waited
		}
		q.log.Warningf("failed to persist %d request log entries, retrying in %v: %v", len(batch), sleep, err)
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return false
		}
		waited += sleep
		delay *= 2
	}
}
