package mock

import (
	"context"
	"testing"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

func TestAdapter_InvokeAndMetadata(t *testing.T) {
	a := New(config.BackendConfig{
		Name:         "m",
		Type:         "mock",
		Capabilities: []string{"chat"},
		Cost:         config.CostConfig{Unit: 1},
	})
	resp, err := a.Invoke(context.Background(), types.BackendRequest{
		AIRequest: types.AIRequest{Input: map[string]any{"text": "ping"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	txt, _ := resp.Output["text"].(string)
	if txt == "" {
		t.Fatal("empty text")
	}
	if err := a.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	md := a.Metadata()
	if md.Name != "m" || md.Type != "mock" {
		t.Fatalf("%+v", md)
	}
}
