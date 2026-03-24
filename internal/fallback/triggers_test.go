package fallback

import "testing"

func TestIsValidTrigger(t *testing.T) {
	if !IsValidTrigger(TriggerTimeout) {
		t.Fatal("timeout should be valid")
	}
	if IsValidTrigger("not_a_trigger") {
		t.Fatal("invalid trigger accepted")
	}
}
