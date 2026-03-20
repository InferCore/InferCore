package slo

import (
	"fmt"
	"testing"
	"time"

	"github.com/infercore/infercore/internal/config"
)

func TestMemoryEngine_SnapshotIncludesTimings(t *testing.T) {
	engine := NewMemoryEngine()
	requestID := "req_1"

	engine.RecordStart(requestID)
	start := time.Now()
	engine.RecordFirstToken(requestID, start.Add(50*time.Millisecond))
	engine.RecordCompletion(requestID, start.Add(450*time.Millisecond))
	engine.RecordFallback(requestID, "timeout")

	snapshot := engine.Snapshot(requestID)
	if snapshot.TTFTMs < 0 {
		t.Fatalf("ttft must be non-negative")
	}
	if snapshot.CompletionLatencyMs <= 0 {
		t.Fatalf("completion latency must be positive")
	}
	if !snapshot.FallbackTriggered {
		t.Fatalf("expected fallback triggered flag")
	}
}

func TestMemoryEngine_ScalingAggregates(t *testing.T) {
	e := NewMemoryEngineFromConfig(config.SLOStoreConfig{MaxRecords: 100, MaxAgeMS: 3600000})
	now := time.Now()
	// Older half: low TTFT
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("old_%d", i)
		e.RecordStart(id)
		t0 := now.Add(time.Duration(i) * time.Millisecond)
		e.RecordFirstToken(id, t0.Add(2*time.Millisecond))
		e.RecordCompletion(id, t0.Add(10*time.Millisecond))
	}
	// Newer half: higher TTFT
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("new_%d", i)
		e.RecordStart(id)
		t0 := now.Add(time.Duration(100+i) * time.Millisecond)
		e.RecordFirstToken(id, t0.Add(50*time.Millisecond))
		e.RecordCompletion(id, t0.Add(80*time.Millisecond))
	}
	ratio, fb := e.ScalingAggregates()
	if ratio < 2.0 {
		t.Fatalf("expected ttft ratio >> 1 for slower recent half, got %v", ratio)
	}
	if fb != 0 {
		t.Fatalf("expected zero fallback rate, got %v", fb)
	}
}

func TestMemoryEngine_UnknownRequest(t *testing.T) {
	engine := NewMemoryEngine()
	snapshot := engine.Snapshot("missing")
	if snapshot.TTFTMs != 0 || snapshot.CompletionLatencyMs != 0 || snapshot.FallbackTriggered {
		t.Fatalf("expected zero-value snapshot for unknown request")
	}
}

func TestMemoryEngine_EvictsWhenOverMaxRecords(t *testing.T) {
	e := NewMemoryEngineFromConfig(config.SLOStoreConfig{MaxRecords: 2, MaxAgeMS: 3600000})
	mk := func(id string) {
		t.Helper()
		e.RecordStart(id)
		now := time.Now()
		e.RecordFirstToken(id, now)
		e.RecordCompletion(id, now.Add(time.Millisecond))
		time.Sleep(3 * time.Millisecond)
	}
	mk("r1")
	mk("r2")
	mk("r3")
	cnt := 0
	for _, id := range []string{"r1", "r2", "r3"} {
		if e.Snapshot(id).CompletionLatencyMs > 0 {
			cnt++
		}
	}
	if cnt != 2 {
		t.Fatalf("expected exactly 2 records after eviction, got %d", cnt)
	}
}
