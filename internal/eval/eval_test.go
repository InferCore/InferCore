package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_SingleItemOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/infer" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"selected_backend": "small-model",
			"fallback":         map[string]any{"triggered": false},
		})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "dataset.json")
	raw := `[{"tenant_id":"team-a","task_type":"chat","input":{"text":"hi"},"options":{"stream":false,"max_tokens":64}}]`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Run(context.Background(), srv.URL, path, &buf, "inference/basic:v1", "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "summary:") || !strings.Contains(out, "items=1") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
