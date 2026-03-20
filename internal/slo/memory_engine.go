package slo

import (
	"sort"
	"sync"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

type requestTiming struct {
	insertedAt     time.Time
	startTime      time.Time
	firstTokenTime time.Time
	endTime        time.Time
	fallback       bool
}

type MemoryEngine struct {
	mu         sync.Mutex
	records    map[string]*requestTiming
	maxRecords int
	maxAge     time.Duration
}

func NewMemoryEngine() *MemoryEngine {
	return NewMemoryEngineFromConfig(config.SLOStoreConfig{})
}

func NewMemoryEngineFromConfig(c config.SLOStoreConfig) *MemoryEngine {
	max := c.MaxRecords
	if max <= 0 {
		max = 10000
	}
	maxAge := time.Duration(c.MaxAgeMS) * time.Millisecond
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	return &MemoryEngine{
		records:    make(map[string]*requestTiming),
		maxRecords: max,
		maxAge:     maxAge,
	}
}

func (e *MemoryEngine) RecordStart(requestID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.ensureRecord(requestID)
	rec.startTime = time.Now()
	e.evictLocked()
}

func (e *MemoryEngine) RecordFirstToken(requestID string, ts time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.ensureRecord(requestID)
	rec.firstTokenTime = ts
	e.evictLocked()
}

func (e *MemoryEngine) RecordCompletion(requestID string, ts time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.ensureRecord(requestID)
	rec.endTime = ts
	e.evictLocked()
}

func (e *MemoryEngine) RecordFallback(requestID string, _ string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.ensureRecord(requestID)
	rec.fallback = true
	e.evictLocked()
}

// ScalingAggregates derives rolling ratios from completed in-memory records for autoscaler hints.
// ttftRatio compares mean TTFT in the newer half vs the older half (by completion time); 1.0 means no skew.
// recentFallbackRate is the fraction of completed requests in the newer half that recorded a fallback.
func (e *MemoryEngine) ScalingAggregates() (ttftRatio float64, recentFallbackRate float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	type sample struct {
		end      time.Time
		ttftMs   int64
		fallback bool
	}
	samples := make([]sample, 0, len(e.records))
	for _, rec := range e.records {
		if rec.endTime.IsZero() || rec.startTime.IsZero() {
			continue
		}
		var ttft int64
		if !rec.firstTokenTime.IsZero() {
			ttft = rec.firstTokenTime.Sub(rec.startTime).Milliseconds()
		} else {
			ttft = rec.endTime.Sub(rec.startTime).Milliseconds()
		}
		if ttft < 0 {
			ttft = 0
		}
		samples = append(samples, sample{end: rec.endTime, ttftMs: ttft, fallback: rec.fallback})
	}
	if len(samples) < 4 {
		return 1.0, 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].end.Before(samples[j].end) })
	mid := len(samples) / 2
	var oldSum, newSum int64
	oldC := mid
	newC := len(samples) - mid
	for i := 0; i < mid; i++ {
		oldSum += samples[i].ttftMs
	}
	for i := mid; i < len(samples); i++ {
		newSum += samples[i].ttftMs
	}
	if oldC == 0 || newC == 0 {
		return 1.0, 0
	}
	oldAvg := float64(oldSum) / float64(oldC)
	newAvg := float64(newSum) / float64(newC)
	if oldAvg < 1 {
		oldAvg = 1
	}
	ttftRatio = newAvg / oldAvg

	var fbRecent int
	for i := mid; i < len(samples); i++ {
		if samples[i].fallback {
			fbRecent++
		}
	}
	recentFallbackRate = float64(fbRecent) / float64(newC)
	return ttftRatio, recentFallbackRate
}

func (e *MemoryEngine) Snapshot(requestID string) types.SLOSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	rec, ok := e.records[requestID]
	if !ok {
		return types.SLOSnapshot{}
	}

	var ttft int64
	if !rec.startTime.IsZero() && !rec.firstTokenTime.IsZero() {
		ttft = rec.firstTokenTime.Sub(rec.startTime).Milliseconds()
		if ttft < 0 {
			ttft = 0
		}
	}

	var completion int64
	var tpot int64
	if !rec.startTime.IsZero() && !rec.endTime.IsZero() {
		completion = rec.endTime.Sub(rec.startTime).Milliseconds()
		if completion < 0 {
			completion = 0
		}
	}
	if !rec.firstTokenTime.IsZero() && !rec.endTime.IsZero() {
		postFirstToken := rec.endTime.Sub(rec.firstTokenTime).Milliseconds()
		if postFirstToken > 0 {
			tpot = postFirstToken / 32
			if tpot <= 0 {
				tpot = 1
			}
		}
	}

	return types.SLOSnapshot{
		TTFTMs:              ttft,
		TPOTMs:              tpot,
		CompletionLatencyMs: completion,
		FallbackTriggered:   rec.fallback,
	}
}

func (e *MemoryEngine) ensureRecord(requestID string) *requestTiming {
	rec, ok := e.records[requestID]
	if !ok {
		rec = &requestTiming{insertedAt: time.Now()}
		e.records[requestID] = rec
	}
	return rec
}

func (e *MemoryEngine) evictLocked() {
	now := time.Now()
	for id, rec := range e.records {
		if e.shouldEvictRecord(rec, now) {
			delete(e.records, id)
		}
	}
	for len(e.records) > e.maxRecords {
		e.evictOneOldestLocked(now)
	}
}

func (e *MemoryEngine) shouldEvictRecord(rec *requestTiming, now time.Time) bool {
	if !rec.endTime.IsZero() {
		return now.Sub(rec.endTime) > e.maxAge
	}
	return now.Sub(rec.insertedAt) > e.maxAge
}

func (e *MemoryEngine) evictOneOldestLocked(now time.Time) {
	var oldestID string
	var oldest time.Time
	first := true
	for id, rec := range e.records {
		t := rec.endTime
		if t.IsZero() {
			t = rec.insertedAt
		}
		if first || t.Before(oldest) {
			first = false
			oldest = t
			oldestID = id
		}
	}
	if oldestID != "" {
		delete(e.records, oldestID)
	}
}
