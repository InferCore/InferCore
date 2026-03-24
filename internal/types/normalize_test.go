package types

import "testing"

func TestNormalizeAIRequest_DefaultsInference(t *testing.T) {
	r := NormalizeAIRequest(AIRequest{
		TenantID: "t",
		TaskType: "x",
		Input:    map[string]any{"text": "hi"},
		Options:  RequestOptions{Stream: false, MaxTokens: 10},
	})
	if r.RequestType != RequestTypeInference {
		t.Fatalf("RequestType=%q", r.RequestType)
	}
	if r.PipelineRef != DefaultPipelineInference {
		t.Fatalf("PipelineRef=%q", r.PipelineRef)
	}
	if r.Context == nil {
		t.Fatal("Context should be non-nil")
	}
}

func TestNormalizeAIRequest_RAGPipeline(t *testing.T) {
	r := NormalizeAIRequest(AIRequest{
		RequestType: RequestTypeRAG,
		TenantID:    "t",
		TaskType:    "x",
		Input:       map[string]any{"text": "q"},
		Options:     RequestOptions{Stream: false, MaxTokens: 10},
	})
	if r.PipelineRef != DefaultPipelineRAG {
		t.Fatalf("PipelineRef=%q", r.PipelineRef)
	}
}

func TestNormalizeAIRequest_PreservesExplicitPipelineRef(t *testing.T) {
	r := NormalizeAIRequest(AIRequest{
		RequestType: RequestTypeInference,
		PipelineRef: "custom:v99",
		TenantID:    "t",
		TaskType:    "x",
		Input:       map[string]any{},
		Options:     RequestOptions{Stream: false, MaxTokens: 10},
	})
	if r.PipelineRef != "custom:v99" {
		t.Fatalf("PipelineRef=%q", r.PipelineRef)
	}
}
