package storage

import "testing"

func TestIdempotencyLedger(t *testing.T) {
	ledger := NewIdempotencyLedger()
	if !ledger.Apply("tx-1", "hash-1") {
		t.Fatal("first apply should be new")
	}
	if ledger.Apply("tx-1", "hash-2") {
		t.Fatal("duplicate apply should be rejected")
	}
	result, ok := ledger.Result("tx-1")
	if !ok || result != "hash-1" {
		t.Fatalf("expected original result, got %q ok=%t", result, ok)
	}
	if _, ok := ledger.Result("missing"); ok {
		t.Fatal("missing result should not exist")
	}
	if ledger.Count() != 1 {
		t.Fatalf("expected count 1, got %d", ledger.Count())
	}
}
