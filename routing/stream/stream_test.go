package stream

import "testing"

func TestManagerRejectsDuplicateAndUnknownStreams(t *testing.T) {
	manager := NewManager()
	first, err := manager.Open()
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 {
		t.Fatalf("expected first stream id 1, got %d", first)
	}
	if err := manager.OpenWithID(first); err == nil {
		t.Fatal("expected duplicate stream id to be rejected")
	}
	if err := manager.EnsureOpen(99); err == nil {
		t.Fatal("expected unknown stream id to be rejected")
	}
	if err := manager.Close(first); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(first); err == nil {
		t.Fatal("expected closing stream twice to fail safely")
	}
}
