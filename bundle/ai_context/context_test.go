package ai_context

import (
	"testing"

	"github.com/keithwegner/pq-fabric/bundle/ccsds"
)

func TestDefaultContextWindow(t *testing.T) {
	window := DefaultContextWindow()
	if window.MaxTokens != 128000 {
		t.Fatalf("expected default max tokens, got %d", window.MaxTokens)
	}
	if window.Policies[ccsds.Execution].PriorityWeight == 0 {
		t.Fatal("expected execution channel policy")
	}
}
