package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHitCounterPrunesStaleKeysAtCapacity(t *testing.T) {
	counter := NewHitCounter()
	stale := time.Now().Add(-2 * time.Minute)
	for index := 0; index < maxTrackedHitKeys; index++ {
		counter.events[fmt.Sprintf("client-%d", index)] = []time.Time{stale}
	}
	if !counter.Allow("fresh-client", 3, time.Minute) {
		t.Fatal("fresh client was denied after stale entries should have been removed")
	}
	if len(counter.events) != 1 {
		t.Fatalf("tracked key count = %d, want 1 after cleanup", len(counter.events))
	}
}

func TestHitCounterAppliesQuotaAtomicallyAcrossConcurrentRequests(t *testing.T) {
	counter := NewHitCounter()
	var allowed atomic.Int32
	var workers sync.WaitGroup
	for range 100 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if counter.Allow("one-client", 5, time.Minute) {
				allowed.Add(1)
			}
		}()
	}
	workers.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("allowed concurrent requests = %d, want 5", got)
	}
}

func TestHitCounterRejectsNewKeysWhenAllSlotsAreActive(t *testing.T) {
	counter := NewHitCounter()
	now := time.Now()
	for index := 0; index < maxTrackedHitKeys; index++ {
		counter.events[fmt.Sprintf("client-%d", index)] = []time.Time{now}
	}
	if counter.Allow("overflow-client", 3, time.Minute) {
		t.Fatal("accepted a new key after reaching the bounded key limit")
	}
	if len(counter.events) != maxTrackedHitKeys {
		t.Fatalf("tracked key count = %d, want %d", len(counter.events), maxTrackedHitKeys)
	}
}
