package bedrock

import (
	"testing"

	"github.com/infercore/infercore/internal/config"
)

func TestAdapter_NameAndMetadata(t *testing.T) {
	a := New(config.BackendConfig{
		Name:           "bedrock-b",
		Type:           "bedrock",
		AWSRegion:      "us-east-1",
		DefaultModel:   "anthropic.claude-v1",
		Capabilities:   []string{"chat"},
		Cost:           config.CostConfig{Unit: 2},
		MaxConcurrency: 5,
	})
	if a.Name() != "bedrock-b" {
		t.Fatalf("Name=%q", a.Name())
	}
	md := a.Metadata()
	if md.Name != "bedrock-b" || md.Type != "bedrock" || md.CostUnit != 2 {
		t.Fatalf("%+v", md)
	}
}
