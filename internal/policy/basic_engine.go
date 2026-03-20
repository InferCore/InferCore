package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

type BasicEngine struct {
	cfg *config.Config

	mu           sync.Mutex
	windowSecond int64
	tenantCounts map[string]int
}

func NewBasicEngine(cfg *config.Config) *BasicEngine {
	return &BasicEngine{
		cfg:          cfg,
		tenantCounts: map[string]int{},
	}
}

func (e *BasicEngine) Evaluate(_ context.Context, req types.InferenceRequest) (types.PolicyDecision, error) {
	tenant, ok := e.cfg.TenantByID(req.TenantID)
	if !ok {
		return types.PolicyDecision{
			Allowed: false,
			Reason:  "unknown tenant",
		}, nil
	}

	normalized := req
	if normalized.Priority == "" {
		normalized.Priority = tenant.Priority
	}

	estimated := estimateBudgetConsumption(normalized)
	if tenant.BudgetPerRequest > 0 && estimated > tenant.BudgetPerRequest {
		return types.PolicyDecision{
			Allowed:    false,
			Reason:     fmt.Sprintf("budget exceeded: estimated=%.2f budget=%.2f", estimated, tenant.BudgetPerRequest),
			Normalized: normalized,
		}, nil
	}

	if !e.allowByRateLimit(tenant.ID, tenant.RateLimitRPS) {
		return types.PolicyDecision{
			Allowed:    false,
			Reason:     "rate limit exceeded",
			Normalized: normalized,
		}, nil
	}

	return types.PolicyDecision{
		Allowed:    true,
		Reason:     "allowed",
		Normalized: normalized,
	}, nil
}

func (e *BasicEngine) allowByRateLimit(tenantID string, limit int) bool {
	if limit <= 0 {
		return true
	}

	nowSec := time.Now().Unix()
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.windowSecond != nowSec {
		e.windowSecond = nowSec
		e.tenantCounts = map[string]int{}
	}

	current := e.tenantCounts[tenantID]
	if current >= limit {
		return false
	}

	e.tenantCounts[tenantID] = current + 1
	return true
}

func estimateBudgetConsumption(req types.InferenceRequest) float64 {
	maxTokens := req.Options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}
	return 1.0 + float64(maxTokens)/256.0
}
