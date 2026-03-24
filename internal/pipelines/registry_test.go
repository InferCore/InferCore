package pipelines

import (
	"testing"

	"github.com/infercore/infercore/internal/types"
)

func TestClassify(t *testing.T) {
	if got := Classify(types.RequestTypeRAG, ""); got != KindRAG {
		t.Fatalf("rag: %s", got)
	}
	if got := Classify(types.RequestTypeAgent, ""); got != KindAgent {
		t.Fatalf("agent: %s", got)
	}
	if got := Classify("", "rag/basic:v1"); got != KindRAG {
		t.Fatalf("ref rag: %s", got)
	}
	if got := Classify("", types.DefaultPipelineInference); got != KindInference {
		t.Fatalf("inference: %s", got)
	}
}
