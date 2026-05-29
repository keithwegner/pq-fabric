package retransmit

import (
	"testing"

	"github.com/keithwegner/pq-fabric/core/storage"
)

func TestLedgerDeduplicatesRetransmission(t *testing.T) {
	ledger := NewLedger()
	tx := Transaction{ID: "tx-1", Sequence: 42, Payload: "pay once"}
	first := ledger.Apply(tx)
	second := ledger.Apply(tx)
	if !first.Applied {
		t.Fatal("first apply should be applied")
	}
	if second.Applied {
		t.Fatal("second apply should be deduplicated")
	}
	if ledger.Count() != 1 {
		t.Fatalf("expected ledger count 1, got %d", ledger.Count())
	}
	if first.ResultHash != second.ResultHash {
		t.Fatal("deduplicated retry should return deterministic original result")
	}
}

func TestPersistentLedgerDeduplicatesAfterRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	tx := Transaction{ID: "tx-1", Sequence: 42, Payload: "pay once"}
	first := NewPersistentLedger(store).Apply(tx)
	if !first.Applied {
		t.Fatalf("first apply should be applied: %+v", first)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	second := NewPersistentLedger(reopened).Apply(tx)
	if second.Applied {
		t.Fatalf("second apply after restart should be deduplicated: %+v", second)
	}
	if second.ResultHash != first.ResultHash {
		t.Fatalf("expected original result hash after restart, got %s want %s", second.ResultHash, first.ResultHash)
	}
	if count := NewPersistentLedger(reopened).Count(); count != 1 {
		t.Fatalf("expected persistent ledger count 1, got %d", count)
	}
}
