package cost

import (
	"testing"

	"github.com/infercore/infercore/internal/types"
)

func TestSimpleEngine_Estimate(t *testing.T) {
	e := NewSimpleEngine()
	out := e.Estimate(
		types.AIRequest{Options: types.RequestOptions{MaxTokens: 1000}},
		types.BackendMetadata{CostUnit: 2.0},
	)
	if out.UnitCost != 2.0 {
		t.Fatalf("UnitCost=%v", out.UnitCost)
	}
	if out.EstimatedTotal <= out.UnitCost {
		t.Fatalf("EstimatedTotal=%v", out.EstimatedTotal)
	}
}

func TestSimpleEngine_Estimate_DefaultMaxTokens(t *testing.T) {
	e := NewSimpleEngine()
	out := e.Estimate(
		types.AIRequest{Options: types.RequestOptions{MaxTokens: 0}},
		types.BackendMetadata{CostUnit: 1.0},
	)
	if out.EstimatedTotal <= 0 {
		t.Fatalf("EstimatedTotal=%v", out.EstimatedTotal)
	}
}
