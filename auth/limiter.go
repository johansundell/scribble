package auth

import (
	"sync"
	"time"
)

// limiter counts wrong PINs per client address in a fixed window that starts
// at the first one. An address with max failures is refused until its
// window ends.
type limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[string]*failureWindow
}

type failureWindow struct {
	start time.Time
	count int
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, failures: make(map[string]*failureWindow)}
}

// reserve counts an attempt from addr as a failure up front and reports
// whether it may go ahead. Checking and counting under one lock means a
// burst of parallel requests can't all slip in before the first failure is
// recorded; a right PIN calls reset.
func (l *limiter) reserve(addr string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	f, ok := l.failures[addr]
	if !ok {
		l.failures[addr] = &failureWindow{start: now, count: 1}
		return true
	}
	if f.count >= l.max {
		return false
	}
	f.count++
	return true
}

func (l *limiter) reset(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, addr)
}

// pruneLocked drops finished windows so the map stays bounded by the number
// of addresses that failed recently.
func (l *limiter) pruneLocked(now time.Time) {
	for addr, f := range l.failures {
		if !now.Before(f.start.Add(l.window)) {
			delete(l.failures, addr)
		}
	}
}
