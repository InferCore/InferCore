package replay

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/infercore/infercore/internal/adapters/mock"
	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/requests"
	"github.com/infercore/infercore/internal/types"
)

func TestReplay_NilStore(t *testing.T) {
	_, err := Replay(context.Background(), &config.Config{}, nil, "x", ModeExact, Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("err=%v", err)
	}
}

func TestReplay_UnknownMode(t *testing.T) {
	st := requests.NewMemoryStore()
	req := types.AIRequest{
		TenantID: "t",
		TaskType: "chat",
		Priority: "p",
		Input:    map[string]any{"text": "hi"},
		Options:  types.RequestOptions{Stream: false, MaxTokens: 64},
	}
	full, _ := json.Marshal(req)
	_ = st.CreateRequest(context.Background(), requests.RequestRow{
		RequestID:     "rid1",
		TenantID:      "t",
		TaskType:      "chat",
		Priority:      "p",
		AIRequestJSON: full,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	_, err := Replay(context.Background(), &config.Config{}, st, "rid1", Mode("bogus"), Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "unknown replay mode") {
		t.Fatalf("err=%v", err)
	}
}

func TestReplayExact_MissingPrimaryBackend(t *testing.T) {
	st := requests.NewMemoryStore()
	req := types.AIRequest{
		TenantID: "t",
		TaskType: "chat",
		Priority: "p",
		Input:    map[string]any{"text": "hi"},
		Options:  types.RequestOptions{Stream: false, MaxTokens: 64},
	}
	full, _ := json.Marshal(req)
	_ = st.CreateRequest(context.Background(), requests.RequestRow{
		RequestID:     "rid2",
		TenantID:      "t",
		TaskType:      "chat",
		Priority:      "p",
		AIRequestJSON: full,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	_, err := Replay(context.Background(), &config.Config{}, st, "rid2", ModeExact, Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "missing primary backend") {
		t.Fatalf("err=%v", err)
	}
}

func replayTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{RequestTimeoutMS: 3000},
		Backends: []config.BackendConfig{
			{Name: "small-model", Type: "mock", TimeoutMS: 100, Cost: config.CostConfig{Unit: 1}, Capabilities: []string{"chat"}},
		},
		Tenants: []config.TenantConfig{{ID: "team-a", Class: "premium", Priority: "high", BudgetPerRequest: 100, RateLimitRPS: 100}},
		Routing: config.RoutingConfig{DefaultBackend: "small-model"},
		Reliability: config.ReliabilityConfig{
			FallbackEnabled: true,
			FallbackRules: []config.FallbackRule{
				{FromBackend: "small-model", On: []string{"timeout"}, FallbackTo: "small-model"},
			},
		},
	}
}

func TestReplayCurrent_InferenceSuccess(t *testing.T) {
	cfg := replayTestConfig()
	st := requests.NewMemoryStore()
	req := types.AIRequest{
		TenantID: "team-a",
		TaskType: "chat",
		Priority: "high",
		Input:    map[string]any{"text": "replay hi"},
		Options:  types.RequestOptions{Stream: false, MaxTokens: 64},
	}
	full, _ := json.Marshal(req)
	_ = st.CreateRequest(context.Background(), requests.RequestRow{
		RequestID:     "rid3",
		TenantID:      req.TenantID,
		TaskType:      req.TaskType,
		Priority:      req.Priority,
		AIRequestJSON: full,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	ad := mock.New(cfg.Backends[0])
	deps := NewDependenciesFromConfig(cfg, map[string]interfaces.BackendAdapter{
		"small-model": ad,
	}, nil)
	resp, err := Replay(context.Background(), cfg, st, "rid3", ModeCurrent, deps)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("status=%q", resp.Status)
	}
	if resp.SelectedBackend != "small-model" {
		t.Fatalf("backend=%q", resp.SelectedBackend)
	}
}
