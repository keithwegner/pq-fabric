package custody

import (
	"testing"

	consensusprotocol "github.com/keithwegner/pq-fabric/consensus/protocol"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func testEvent() Event {
	return Event{
		BundleID:        "bundle-1",
		TransactionID:   "tx-1",
		SourceNode:      "node-a",
		CustodyHolder:   "validator-1",
		DestinationNode: "node-b",
		BundleDigest:    "digest-1",
		SequenceNumber:  1,
		LogicalTick:     10,
	}
}

func testCustodySetup(t *testing.T) (cryptosuite.CryptoSuite, map[string]identity.ValidatorIdentity) {
	t.Helper()
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	identities, err := identity.ValidatorIdentitiesForSuite(nil, selected)
	if err != nil {
		t.Fatal(err)
	}
	return selected, identities
}

func TestCustodyConfirmationSevenOfSevenAndFiveOfSeven(t *testing.T) {
	selected, identities := testCustodySetup(t)
	event := testEvent()
	votes, err := SignEventVotes(event, identity.DefaultValidatorIDs(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if confirmation, err := Confirm(event, votes, identities, selected.NewVerifier(), 5); err != nil || !confirmation.Confirmed || confirmation.QuorumSize != 7 {
		t.Fatalf("7-of-7 custody confirmation failed: confirmation=%+v err=%v", confirmation, err)
	}
	if confirmation, err := Confirm(event, votes[:5], identities, selected.NewVerifier(), 5); err != nil || !confirmation.Confirmed || confirmation.QuorumSize != 5 {
		t.Fatalf("5-of-7 custody confirmation failed: confirmation=%+v err=%v", confirmation, err)
	}
}

func TestCustodyConfirmationRejectsFourVotesDuplicatesMismatchesAndUnknowns(t *testing.T) {
	selected, identities := testCustodySetup(t)
	event := testEvent()
	votes, err := SignEventVotes(event, identity.DefaultValidatorIDs(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Confirm(event, votes[:4], identities, selected.NewVerifier(), 5); err == nil {
		t.Fatal("4-of-7 custody confirmation should fail")
	}
	duplicateVotes := append([]consensusprotocol.Vote(nil), votes[:4]...)
	duplicateVotes = append(duplicateVotes, votes[0])
	if _, err := Confirm(event, duplicateVotes, identities, selected.NewVerifier(), 5); err == nil {
		t.Fatal("duplicate validator vote should not count toward quorum")
	}
	mismatched := testEvent()
	mismatched.BundleDigest = "wrong-digest"
	wrongVotes, err := SignEventVotes(mismatched, identity.DefaultValidatorIDs()[:5], selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Confirm(event, wrongVotes, identities, selected.NewVerifier(), 5); err == nil {
		t.Fatal("votes for different custody digest should not combine")
	}
	unknownVotes, err := SignEventVotes(event, []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-unknown"}, selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Confirm(event, unknownVotes, identities, selected.NewVerifier(), 5); err == nil {
		t.Fatal("unknown validator should fail custody confirmation")
	}
}

func TestCustodyConfirmationRejectsInvalidSignature(t *testing.T) {
	selected, identities := testCustodySetup(t)
	event := testEvent()
	votes, err := SignEventVotes(event, identity.DefaultValidatorIDs()[:5], selected)
	if err != nil {
		t.Fatal(err)
	}
	votes[0].Signature = "invalid"
	if _, err := Confirm(event, votes, identities, selected.NewVerifier(), 5); err == nil {
		t.Fatal("invalid signature should fail custody confirmation")
	}
}

func TestCustodyPersistenceSurvivesRestartAndReplayIsSafe(t *testing.T) {
	selected, identities := testCustodySetup(t)
	event := testEvent()
	votes, err := SignEventVotes(event, identity.DefaultValidatorIDs()[:5], selected)
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := Confirm(event, votes, identities, selected.NewVerifier(), 5)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyConfirmation(store, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("first custody confirmation should apply")
	}
	replayed, err := ApplyConfirmation(store, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if replayed {
		t.Fatal("replayed custody confirmation should be idempotent")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := LoadConfirmed(reopened)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !records[0].Confirmed || records[0].BundleID != event.BundleID {
		t.Fatalf("custody confirmation did not survive restart: %+v", records)
	}
	restartReplay, err := ApplyConfirmation(reopened, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if restartReplay {
		t.Fatal("restart replay should not double-apply custody confirmation")
	}
}
