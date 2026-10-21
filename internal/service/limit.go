package service

import (
	"sync"
	"time"
)

const maxTrackedHitKeys = 10000

// HitCounter counts recent events for one process. Each server keeps its own counter.
type HitCounter struct {
	mu     sync.Mutex
	events map[string][]time.Time
	calls  uint64
}

func NewHitCounter() *HitCounter {
	return &HitCounter{events: map[string][]time.Time{}}
}

func (h *HitCounter) Allow(key string, limit int, window time.Duration) bool {
	if h == nil || limit < 1 || key == "" {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if !h.makeRoom(key, now, window) {
		return false
	}
	kept := pruneTimes(h.events[key], now, window)
	if len(kept) >= limit {
		h.events[key] = kept
		return false
	}
	h.events[key] = append(kept, now)
	return true
}

func (h *HitCounter) makeRoom(key string, now time.Time, window time.Duration) bool {
	h.calls++
	_, exists := h.events[key]
	if h.calls%128 == 0 || (!exists && len(h.events) >= maxTrackedHitKeys) {
		for trackedKey, times := range h.events {
			kept := pruneTimes(times, now, window)
			if len(kept) == 0 {
				delete(h.events, trackedKey)
			} else {
				h.events[trackedKey] = kept
			}
		}
	}
	_, exists = h.events[key]
	return exists || len(h.events) < maxTrackedHitKeys
}

func (h *HitCounter) Reset(key string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.events, key)
}

func pruneTimes(times []time.Time, now time.Time, window time.Duration) []time.Time {
	cutoff := now.Add(-window)
	kept := make([]time.Time, 0, len(times))
	for _, stamp := range times {
		if stamp.After(cutoff) {
			kept = append(kept, stamp)
		}
	}
	return kept
}
