package execution

import (
	"context"
	"errors"
	"testing"
)

func TestStepWriter_Run_NilStore_StillRunsFn(t *testing.T) {
	var w *StepWriter
	err := w.Run(context.Background(), StepNormalize, "", nil, func() (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestStepWriter_Run_NilStore_PropagatesError(t *testing.T) {
	w := &StepWriter{Store: nil}
	e := errors.New("fail")
	err := w.Run(context.Background(), StepPolicyCheck, "", nil, func() (map[string]any, error) {
		return nil, e
	})
	if err != e {
		t.Fatalf("got %v want %v", err, e)
	}
}

func TestStepWriter_Index(t *testing.T) {
	if (*StepWriter)(nil).Index() != 0 {
		t.Fatal("nil Index")
	}
}
