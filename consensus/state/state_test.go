package state

import "testing"

func TestDeterministicStateDigest(t *testing.T) {
	txs := []Transaction{{ID: "tx-1", Payload: "alpha"}, {ID: "tx-2", Payload: "beta"}}
	left := NewMachine()
	right := NewMachine()
	leftDigest, leftApplied, err := left.Apply(txs)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, rightApplied, err := right.Apply(txs)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("expected deterministic digest, got %s and %s", leftDigest, rightDigest)
	}
	if leftApplied != 2 || rightApplied != 2 {
		t.Fatalf("expected both machines to apply 2 transactions, got %d and %d", leftApplied, rightApplied)
	}
}

func TestDuplicateTransactionIDsAreNotDoubleApplied(t *testing.T) {
	machine := NewMachine()
	first, applied, err := machine.Apply([]Transaction{{ID: "tx-1", Payload: "alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("expected first transaction to apply, got %d", applied)
	}
	second, applied, err := machine.Apply([]Transaction{{ID: "tx-1", Payload: "alpha again"}})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatalf("expected duplicate transaction to be skipped, got %d", applied)
	}
	if first != second {
		t.Fatalf("duplicate transaction changed digest: %s -> %s", first, second)
	}
}

func TestTransactionsFromPayloadHasStableID(t *testing.T) {
	left := TransactionsFromPayload("same payload")
	right := TransactionsFromPayload("same payload")
	if len(left) != 1 || len(right) != 1 {
		t.Fatalf("expected one transaction per payload, got %d and %d", len(left), len(right))
	}
	if left[0].ID != right[0].ID {
		t.Fatalf("expected stable transaction ID, got %s and %s", left[0].ID, right[0].ID)
	}
}

func TestMalformedTransactionFailsSafely(t *testing.T) {
	machine := NewMachine()
	if _, _, err := machine.Apply([]Transaction{{}}); err == nil {
		t.Fatal("expected empty transaction to fail")
	}
}
