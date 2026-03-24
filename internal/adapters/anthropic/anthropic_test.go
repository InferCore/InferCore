package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

func TestAdapter_InvokeNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/messages" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "hello anthropic"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	a := New(config.BackendConfig{
		Name:         "a",
		Type:         "anthropic",
		Endpoint:     srv.URL,
		TimeoutMS:    5000,
		APIKey:       "k",
		DefaultModel: "claude-3-5-sonnet-20241022",
	})
	resp, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{
			Input: map[string]any{"text": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	txt, _ := resp.Output["text"].(string)
	if txt != "hello anthropic" {
		t.Fatalf("text=%q", txt)
	}
}

func TestAdapter_Health(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	a := New(config.BackendConfig{
		Name:         "a",
		Type:         "anthropic",
		Endpoint:     srv.URL,
		TimeoutMS:    5000,
		APIKey:       "secret",
		DefaultModel: "claude-3-5-sonnet-20241022",
	})
	if err := a.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestAdapter_Metadata(t *testing.T) {
	a := New(config.BackendConfig{
		Name:           "n",
		Type:           "anthropic",
		Capabilities:   []string{"chat"},
		Cost:           config.CostConfig{Unit: 3},
		MaxConcurrency: 10,
	})
	md := a.Metadata()
	if md.Name != "n" || md.Type != "anthropic" || md.CostUnit != 3 || md.MaxConcurrency != 10 {
		t.Fatalf("%+v", md)
	}
}
