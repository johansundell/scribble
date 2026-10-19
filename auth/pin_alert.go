package auth

import (
	"sync"
	"time"

	"github.com/johansundell/scribble/logging"
)

// PINAlert tells logged-in users that many wrong PINs have been entered and
// WEB_PIN should be changed.
type PINAlert struct {
	// Count is the number of wrong PINs: the ones that triggered the alert
	// and every one since.
	Count int
	// First and Last are when the first and the latest of them were entered.
	First, Last time.Time
}

// pinAlerter counts checked wrong PINs from all addresses. Once max of them
// fall within window, the alert triggers and PIN login is locked for window.
// The alert stays, in memory, until the service restarts; changing WEB_PIN
// requires a restart, so it clears exactly when the fix is applied. After
// the lock lifts, another max wrong PINs within window lock it again.
type pinAlerter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	log    logging.Logger
	// recent holds the times of wrong PINs within window, oldest first,
	// since the last lock.
	recent      []time.Time
	alert       *PINAlert
	lockedUntil time.Time
}

func newPINAlerter(max int, window time.Duration, l logging.Logger) *pinAlerter {
	return &pinAlerter{max: max, window: window, log: logging.OrStd(l)}
}

// record counts a wrong PIN entered at now.
func (p *pinAlerter) record(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.alert != nil {
		p.alert.Count++
		p.alert.Last = now
	}
	keep := 0
	for keep < len(p.recent) && !now.Before(p.recent[keep].Add(p.window)) {
		keep++
	}
	p.recent = append(p.recent[keep:], now)
	if len(p.recent) < p.max {
		return
	}
	if p.alert == nil {
		p.alert = &PINAlert{Count: len(p.recent), First: p.recent[0], Last: now}
	}
	p.recent = nil
	p.lockedUntil = now.Add(p.window)
	p.log.Warningf("PIN Alert: %d wrong PINs since %s; web login is locked until %s. If that wasn't you, change WEB_PIN",
		p.alert.Count, p.alert.First.Format(time.RFC3339), p.lockedUntil.Format(time.RFC3339))
}

// locked reports whether PIN login is locked at now.
func (p *pinAlerter) locked(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return now.Before(p.lockedUntil)
}

// current returns the alert, if it has triggered.
func (p *pinAlerter) current() (PINAlert, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.alert == nil {
		return PINAlert{}, false
	}
	return *p.alert, true
}
