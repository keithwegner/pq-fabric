package custody

import (
	"encoding/json"
	"fmt"
	"strings"

	consensusprotocol "github.com/keithwegner/pq-fabric/consensus/protocol"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
	"github.com/keithwegner/pq-fabric/core/storage"
)

const SnapshotPrefix = "bundle-custody/"

type Event struct {
	BundleID          string                              `json:"bundle_id"`
	TransactionID     string                              `json:"transaction_id"`
	SourceNode        string                              `json:"source_node"`
	CustodyHolder     string                              `json:"custody_holder"`
	DestinationNode   string                              `json:"destination_node"`
	BundleDigest      string                              `json:"bundle_digest"`
	SequenceNumber    uint64                              `json:"sequence_number"`
	LogicalTick       uint64                              `json:"logical_tick"`
	QuorumCertificate consensusprotocol.QuorumCertificate `json:"quorum_certificate,omitempty"`
}

type eventToDigest struct {
	BundleID        string `json:"bundle_id"`
	TransactionID   string `json:"transaction_id"`
	SourceNode      string `json:"source_node"`
	CustodyHolder   string `json:"custody_holder"`
	DestinationNode string `json:"destination_node"`
	BundleDigest    string `json:"bundle_digest"`
	SequenceNumber  uint64 `json:"sequence_number"`
	LogicalTick     uint64 `json:"logical_tick"`
}

type Confirmation struct {
	Event       Event                               `json:"event"`
	EventDigest string                              `json:"event_digest"`
	Confirmed   bool                                `json:"confirmed"`
	QuorumSize  int                                 `json:"quorum_size"`
	Certificate consensusprotocol.QuorumCertificate `json:"certificate"`
}

type PersistenceRecord struct {
	BundleID      string `json:"bundle_id"`
	TransactionID string `json:"transaction_id"`
	EventDigest   string `json:"event_digest"`
	Confirmed     bool   `json:"confirmed"`
	QuorumSize    int    `json:"quorum_size"`
	AppliedCount  int    `json:"applied_count"`
}

func (e Event) Digest() (string, error) {
	return messages.HashCanonical(eventToDigest{
		BundleID:        e.BundleID,
		TransactionID:   e.TransactionID,
		SourceNode:      e.SourceNode,
		CustodyHolder:   e.CustodyHolder,
		DestinationNode: e.DestinationNode,
		BundleDigest:    e.BundleDigest,
		SequenceNumber:  e.SequenceNumber,
		LogicalTick:     e.LogicalTick,
	})
}

func SignEventVotes(event Event, voterIDs []string, selected cryptosuite.CryptoSuite) ([]consensusprotocol.Vote, error) {
	eventDigest, err := event.Digest()
	if err != nil {
		return nil, err
	}
	votes := make([]consensusprotocol.Vote, 0, len(voterIDs))
	for _, voterID := range voterIDs {
		signer, err := selected.NewSigner(voterID)
		if err != nil {
			return nil, fmt.Errorf("create signer for %s: %w", voterID, err)
		}
		vote, err := consensusprotocol.SignStageVote(1, 0, consensusprotocol.StagePrecommit, eventDigest, voterID, signer)
		if err != nil {
			return nil, err
		}
		votes = append(votes, vote)
	}
	return votes, nil
}

func Confirm(event Event, votes []consensusprotocol.Vote, identities map[string]identity.ValidatorIdentity, verifier pqcrypto.SignatureVerifier, threshold int) (Confirmation, error) {
	eventDigest, err := event.Digest()
	if err != nil {
		return Confirmation{}, err
	}
	cert, err := consensusprotocol.FormStageQuorumCertificate(1, 0, consensusprotocol.StagePrecommit, eventDigest, votes, threshold)
	if err != nil {
		return Confirmation{}, err
	}
	if err := Verify(event, cert, identities, verifier); err != nil {
		return Confirmation{}, err
	}
	event.QuorumCertificate = cert
	return Confirmation{Event: event, EventDigest: eventDigest, Confirmed: true, QuorumSize: len(cert.Votes), Certificate: cert}, nil
}

func Verify(event Event, cert consensusprotocol.QuorumCertificate, identities map[string]identity.ValidatorIdentity, verifier pqcrypto.SignatureVerifier) error {
	eventDigest, err := event.Digest()
	if err != nil {
		return err
	}
	if cert.BlockHash != eventDigest {
		return fmt.Errorf("custody quorum certificate digest mismatch")
	}
	return consensusprotocol.VerifyQuorumCertificate(cert, identities, verifier)
}

func ApplyConfirmation(store storage.ValidatorStore, confirmation Confirmation) (bool, error) {
	if !confirmation.Confirmed {
		return false, fmt.Errorf("custody confirmation is not confirmed")
	}
	idempotencyID := SnapshotPrefix + confirmation.Event.BundleID
	applied, existingDigest, err := store.RecordIdempotency(idempotencyID, confirmation.EventDigest)
	if err != nil {
		return false, err
	}
	if !applied {
		if existingDigest != confirmation.EventDigest {
			return false, fmt.Errorf("conflicting custody confirmation for %s", confirmation.Event.BundleID)
		}
		return false, nil
	}
	record := PersistenceRecord{
		BundleID:      confirmation.Event.BundleID,
		TransactionID: confirmation.Event.TransactionID,
		EventDigest:   confirmation.EventDigest,
		Confirmed:     confirmation.Confirmed,
		QuorumSize:    confirmation.QuorumSize,
		AppliedCount:  1,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	return true, store.SaveSnapshot(storage.SnapshotRecord{
		ID:           idempotencyID,
		Height:       confirmation.Event.SequenceNumber,
		LastHash:     confirmation.Event.BundleDigest,
		SnapshotJSON: data,
	})
}

func LoadConfirmed(store storage.ValidatorStore) ([]PersistenceRecord, error) {
	snapshots, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}
	var records []PersistenceRecord
	for _, snapshot := range snapshots {
		if !strings.HasPrefix(snapshot.ID, SnapshotPrefix) {
			continue
		}
		var record PersistenceRecord
		if err := json.Unmarshal(snapshot.SnapshotJSON, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}
