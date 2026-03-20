package traceutil

import "testing"

func TestTraceAndSpanIDLengths(t *testing.T) {
	traceID := NewTraceID()
	spanID := NewSpanID()

	if len(traceID) != 32 {
		t.Fatalf("expected trace id length 32, got %d", len(traceID))
	}
	if len(spanID) != 16 {
		t.Fatalf("expected span id length 16, got %d", len(spanID))
	}
}

func TestTraceIDIsUsuallyUnique(t *testing.T) {
	a := NewTraceID()
	b := NewTraceID()
	if a == b {
		t.Fatalf("expected different trace ids, both were %q", a)
	}
}
