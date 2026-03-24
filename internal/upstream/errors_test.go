package upstream

import (
	"strings"
	"testing"
)

func TestError_ErrorString(t *testing.T) {
	e := New(KindTimeout, "deadline")
	if e.Error() == "" {
		t.Fatal("empty Error()")
	}
	if !strings.Contains(e.Error(), string(KindTimeout)) {
		t.Fatalf("unexpected: %q", e.Error())
	}
}

func TestNew(t *testing.T) {
	e := New(KindBackendError, "msg")
	if e.Kind != KindBackendError || e.Message != "msg" {
		t.Fatalf("%+v", e)
	}
}
