package requests

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_CreateGet(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	row := RequestRow{
		RequestID: "r1",
		TenantID:  "t",
		TaskType:  "chat",
		Priority:  "p",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := st.CreateRequest(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRequest(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestID != "r1" || got.TenantID != "t" {
		t.Fatalf("%+v", got)
	}
	if err := st.CreateRequest(ctx, row); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestMemoryStore_GetRequest_NotFound(t *testing.T) {
	st := NewMemoryStore()
	_, err := st.GetRequest(context.Background(), "missing")
	if err != ErrNotFound {
		t.Fatalf("err=%v", err)
	}
}
