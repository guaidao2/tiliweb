package security

import (
	"sync"
	"time"
)

type lockoutEntry struct {
	failures int
	until    time.Time
}

type rateEntry struct {
	count int
	start time.Time
}

// Limiter provides per-account lockout and per-IP failed-attempt throttling.
// It is in-memory and single-instance, matching the SQLite deployment model.
type Limiter struct {
	mu        sync.Mutex
	threshold int
	lockout   time.Duration
	rate      int
	window    time.Duration
	now       func() time.Time

	lockouts map[string]lockoutEntry
	rates    map[string]rateEntry
}

func NewLimiter(threshold int, lockout time.Duration, ratePerMinute int) *Limiter {
	if threshold < 0 {
		threshold = 0
	}
	if lockout < 0 {
		lockout = 0
	}
	if ratePerMinute < 0 {
		ratePerMinute = 0
	}
	return &Limiter{
		threshold: threshold,
		lockout:   lockout,
		rate:      ratePerMinute,
		window:    time.Minute,
		now:       time.Now,
		lockouts:  map[string]lockoutEntry{},
		rates:     map[string]rateEntry{},
	}
}

// Allow reports whether an attempt may proceed. It does not count the attempt;
// call Fail on a failed attempt and Success on a successful one.
func (l *Limiter) Allow(key, ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	if e, ok := l.lockouts[key]; ok && e.until.After(now) {
		return false, e.until.Sub(now)
	}
	if l.rate > 0 && ip != "" {
		r := l.rates[ip]
		if r.start.IsZero() || now.Sub(r.start) >= l.window {
			r = rateEntry{start: now}
		}
		if r.count >= l.rate {
			remaining := l.window - now.Sub(r.start)
			if remaining < 0 {
				remaining = 0
			}
			return false, remaining
		}
	}
	return true, 0
}

func (l *Limiter) Fail(key, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	if l.rate > 0 && ip != "" {
		r := l.rates[ip]
		if r.start.IsZero() || now.Sub(r.start) >= l.window {
			r = rateEntry{start: now}
		}
		r.count++
		l.rates[ip] = r
	}

	if l.threshold > 0 && l.lockout > 0 {
		e := l.lockouts[key]
		e.failures++
		if e.failures >= l.threshold {
			e.until = now.Add(l.lockout)
			e.failures = 0
		}
		l.lockouts[key] = e
	}
}

func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.lockouts, key)
}

func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, e := range l.lockouts {
		if !e.until.After(now) && e.failures == 0 {
			delete(l.lockouts, k)
		}
	}
	for k, r := range l.rates {
		if now.Sub(r.start) >= l.window {
			delete(l.rates, k)
		}
	}
}
