package httpapi

import (
	"sync"
	"time"
)

type failureLogLimit struct {
	lastLogged time.Time
	suppressed uint64
}

type failureLogLimiter struct {
	mu      sync.Mutex
	entries map[string]failureLogLimit
}

func newFailureLogLimiter() *failureLogLimiter {
	return &failureLogLimiter{entries: make(map[string]failureLogLimit)}
}

func (l *failureLogLimiter) allow(key string, interval time.Duration) (uint64, bool) {
	if l == nil || key == "" || interval <= 0 {
		return 0, true
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]failureLogLimit)
	}

	now := time.Now()
	entry, exists := l.entries[key]
	if exists && now.Sub(entry.lastLogged) < interval {
		entry.suppressed++
		l.entries[key] = entry
		return 0, false
	}

	l.entries[key] = failureLogLimit{lastLogged: now}
	return entry.suppressed, true
}
