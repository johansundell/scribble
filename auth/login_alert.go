package auth

import (
	"sync"
	"time"

	"github.com/johansundell/scribble/logging"
)

// LoginAlert tells logged-in users that many wrong login codes have been
// entered.
type LoginAlert struct {
	// Count is the number of wrong codes: the ones that triggered the alert
	// and every one since.
	Count int
	// First and Last are when the first and the latest of them were entered.
	First, Last time.Time
}

// loginAlerter counts checked wrong codes from all addresses. Once max of
// them fall within window, the alert triggers and web login is locked for
// window. The alert stays, in memory, until the service restarts. After the
// lock lifts, another max wrong codes within window lock it again.
type loginAlerter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	log    logging.Logger
	// recent holds the times of wrong codes within window, oldest first,
	// since the last lock.
	recent      []time.Time
	alert       *LoginAlert
	lockedUntil time.Time
}

func newLoginAlerter(max int, window time.Duration, l logging.Logger) *loginAlerter {
	return &loginAlerter{max: max, window: window, log: logging.OrStd(l)}
}

// record counts a wrong code entered at now.
func (p *loginAlerter) record(now time.Time) {
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
		p.alert = &LoginAlert{Count: len(p.recent), First: p.recent[0], Last: now}
	}
	p.recent = nil
	p.lockedUntil = now.Add(p.window)
	p.log.Warningf("Login Alert: %d wrong codes since %s; web login is locked until %s. If that wasn't you, someone is guessing codes",
		p.alert.Count, p.alert.First.Format(time.RFC3339), p.lockedUntil.Format(time.RFC3339))
}

// locked reports whether web login is locked at now.
func (p *loginAlerter) locked(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return now.Before(p.lockedUntil)
}

// current returns the alert, if it has triggered.
func (p *loginAlerter) current() (LoginAlert, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.alert == nil {
		return LoginAlert{}, false
	}
	return *p.alert, true
}
