package retransmit

import (
	"testing"

	bundleprotocol "github.com/keithwegner/pq-fabric/bundle/protocol"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func retransmitEnvelope(t *testing.T, seq uint64, txID string) bundleprotocol.Envelope {
	t.Helper()
	env, err := bundleprotocol.NewEnvelope(bundleprotocol.NewEnvelopeInput{
		SourceNodeID:      "node-a",
		DestinationNodeID: "node-b",
		ChannelID:         "execution",
		ChannelType:       "execution",
		SequenceNumber:    seq,
		TransactionID:     txID,
		CreationTick:      1,
		ExpirationTick:    100,
		Priority:          100,
		PayloadBytes:      []byte("payload-" + txID),
		CustodyRequested:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestQueueRetransmittedBundleCommitsOnce(t *testing.T) {
	q := NewQueue(NewLedger())
	env := retransmitEnvelope(t, 1, "tx-1")
	first := q.Submit(env, 1)
	second := q.Submit(env, 1)
	if !first.Applied {
		t.Fatalf("first submit should apply: %+v", first)
	}
	if !second.Duplicate || second.Applied {
		t.Fatalf("duplicate retransmission should be no-op: %+v", second)
	}
}

func TestQueueOutOfOrderSequenceIsPendingThenDrained(t *testing.T) {
	q := NewQueue(NewLedger())
	secondEnv := retransmitEnvelope(t, 2, "tx-2")
	firstEnv := retransmitEnvelope(t, 1, "tx-1")
	outOfOrder := q.Submit(secondEnv, 1)
	if !outOfOrder.PendingMissing || outOfOrder.ExpectedSequence != 1 {
		t.Fatalf("sequence 2 should wait for sequence 1: %+v", outOfOrder)
	}
	if got := len(q.PendingForRetransmit()); got != 1 {
		t.Fatalf("expected one pending retransmission, got %d", got)
	}
	first := q.Submit(firstEnv, 1)
	if !first.Applied {
		t.Fatalf("first sequence should apply and drain pending sequence 2: %+v", first)
	}
	if q.ledger.Count() != 2 {
		t.Fatalf("expected both transactions applied once after drain, got %d", q.ledger.Count())
	}
}

func TestQueueReconnectSendsPendingUnconfirmedBundles(t *testing.T) {
	q := NewQueue(NewLedger())
	firstEnv := retransmitEnvelope(t, 1, "tx-1")
	secondEnv := retransmitEnvelope(t, 2, "tx-2")
	q.Submit(firstEnv, 1)
	q.Submit(secondEnv, 1)
	q.Confirm(firstEnv.BundleID)
	pending := q.PendingForRetransmit()
	if len(pending) != 1 || pending[0].BundleID != secondEnv.BundleID {
		t.Fatalf("expected only unconfirmed second bundle pending, got %+v", pending)
	}
}

func TestPersistentQueueDoesNotDoubleApplyAfterRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	env := retransmitEnvelope(t, 1, "tx-1")
	q := NewQueue(NewPersistentLedger(store))
	first := q.Submit(env, 1)
	if !first.Applied {
		t.Fatalf("first submit should apply: %+v", first)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	afterRestart := NewQueue(NewPersistentLedger(reopened)).Submit(env, 1)
	if afterRestart.Applied || !afterRestart.Duplicate {
		t.Fatalf("accepted transaction should not double-apply after restart: %+v", afterRestart)
	}
}
