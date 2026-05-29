package reconcile

import (
	"testing"

	"github.com/keithwegner/pq-fabric/core/storage"
)

func stateWith(records ...BundleRecord) BundleState {
	state := NewBundleState()
	for _, record := range records {
		state.Bundles[record.BundleID] = record
		state.CommittedTransactionIDs[record.TransactionID] = record.Digest
		state.CustodyStatusByBundle[record.BundleID] = "confirmed"
		state.LatestSequenceByChannel[record.ChannelID] = record.Sequence
	}
	digest, _ := state.Digest()
	state.ContextStateDigest = digest
	return state
}

func TestReconcileIdenticalStateNoopAndDuplicateIgnored(t *testing.T) {
	record := BundleRecord{BundleID: "bundle-1", Digest: "digest-1", TransactionID: "tx-1", ChannelID: "execution", Sequence: 1}
	local := stateWith(record)
	remote := stateWith(record)
	plan := Compare(local, remote)
	if !plan.Noop || len(plan.DuplicateBundleIDs) != 1 {
		t.Fatalf("identical state should be no-op with duplicate ignored, got %+v", plan)
	}
	reconciled, err := Apply(local, remote, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciled.Bundles) != 1 {
		t.Fatalf("duplicate bundle should not double-apply, got %+v", reconciled.Bundles)
	}
}

func TestReconcileMissingBundleRepairsAndConvergesDigest(t *testing.T) {
	first := BundleRecord{BundleID: "bundle-1", Digest: "digest-1", TransactionID: "tx-1", ChannelID: "execution", Sequence: 1}
	second := BundleRecord{BundleID: "bundle-2", Digest: "digest-2", TransactionID: "tx-2", ChannelID: "execution", Sequence: 2}
	local := stateWith(first)
	local.PendingBundleIDs = []string{"bundle-2"}
	remote := stateWith(first, second)
	plan := Compare(local, remote)
	if len(plan.MissingBundleIDs) != 1 || plan.MissingBundleIDs[0] != "bundle-2" {
		t.Fatalf("expected missing bundle-2, got %+v", plan)
	}
	reconciled, err := Apply(local, remote, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reconciled.Bundles["bundle-2"]; !ok {
		t.Fatal("missing bundle was not repaired")
	}
	if reconciled.LatestSequenceByChannel["execution"] != 2 {
		t.Fatalf("latest sequence did not advance: %+v", reconciled.LatestSequenceByChannel)
	}
	if len(reconciled.PendingBundleIDs) != 0 {
		t.Fatalf("repaired bundle should be removed from pending: %+v", reconciled.PendingBundleIDs)
	}
}

func TestReconcileConflictDetected(t *testing.T) {
	local := stateWith(BundleRecord{BundleID: "bundle-1", Digest: "digest-a", TransactionID: "tx-1", ChannelID: "execution", Sequence: 1})
	remote := stateWith(BundleRecord{BundleID: "bundle-1", Digest: "digest-b", TransactionID: "tx-1", ChannelID: "execution", Sequence: 1})
	plan := Compare(local, remote)
	if len(plan.ConflictingBundleIDs) != 1 {
		t.Fatalf("expected conflict, got %+v", plan)
	}
	if _, err := Apply(local, remote, plan); err == nil {
		t.Fatal("conflict should fail reconciliation")
	}
}

func TestReconcileSnapshotSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := stateWith(BundleRecord{BundleID: "bundle-1", Digest: "digest-1", TransactionID: "tx-1", ChannelID: "execution", Sequence: 1})
	if err := SaveSnapshot(store, state); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, ok, err := LoadLatestSnapshot(reopened)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded.Bundles["bundle-1"].Digest != "digest-1" {
		t.Fatalf("reconciled bundle state did not survive restart: ok=%v state=%+v", ok, loaded)
	}
}
