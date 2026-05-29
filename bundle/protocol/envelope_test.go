package protocol

import (
	"errors"
	"testing"
)

func sampleEnvelope(t *testing.T) Envelope {
	t.Helper()
	env, err := NewEnvelope(NewEnvelopeInput{
		SourceNodeID:      "node-a",
		DestinationNodeID: "node-b",
		ChannelID:         "execution",
		ChannelType:       "execution",
		SequenceNumber:    1,
		TransactionID:     "tx-1",
		CreationTick:      10,
		ExpirationTick:    20,
		Priority:          100,
		PayloadBytes:      []byte("run tool"),
		CustodyRequested:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestEnvelopeDigestStable(t *testing.T) {
	env := sampleEnvelope(t)
	first, err := env.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := env.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("digest changed across calls: %s != %s", first, second)
	}
	if env.BundleID != "bundle-"+first[:24] {
		t.Fatalf("bundle id should be derived from digest, got %s", env.BundleID)
	}
}

func TestEnvelopeDigestChangesForPayloadAndTransaction(t *testing.T) {
	env := sampleEnvelope(t)
	original, err := env.Digest()
	if err != nil {
		t.Fatal(err)
	}
	changedPayload := env
	changedPayload.PayloadBytes = []byte("different")
	changedPayload.PayloadDigest = ""
	payloadDigest, err := changedPayload.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if payloadDigest == original {
		t.Fatal("changed payload should change digest")
	}
	changedTx := env
	changedTx.TransactionID = "tx-2"
	txDigest, err := changedTx.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if txDigest == original {
		t.Fatal("changed transaction id should change digest")
	}
}

func TestEnvelopeValidationRejectsMalformedAndExpired(t *testing.T) {
	env := sampleEnvelope(t)
	if err := env.Validate(15); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
	env.TransactionID = ""
	if err := env.Validate(15); !errors.Is(err, ErrMalformed) {
		t.Fatalf("expected malformed transaction id error, got %v", err)
	}
	expired := sampleEnvelope(t)
	if err := expired.Validate(21); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expired error, got %v", err)
	}
}

func TestEnvelopeValidationRejectsDigestMismatch(t *testing.T) {
	env := sampleEnvelope(t)
	env.PayloadBytes = []byte("tampered")
	if err := env.Validate(11); !errors.Is(err, ErrMalformed) {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestEnvelopeIndexHandlesDuplicatesSafely(t *testing.T) {
	idx := NewIndex()
	env := sampleEnvelope(t)
	if err := idx.Add(env); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(env); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected same digest duplicate, got %v", err)
	}
	conflict := env
	conflict.TransactionID = "tx-conflict"
	if err := idx.Add(conflict); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected conflicting duplicate error, got %v", err)
	}
}
