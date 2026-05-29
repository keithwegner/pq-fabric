package anchors

import (
	"context"
	"errors"
	"testing"

	consensusprotocol "github.com/keithwegner/pq-fabric/consensus/protocol"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
)

func testIdentityRecord(t *testing.T) IdentityRecord {
	t.Helper()
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	identities, err := identity.ValidatorIdentitiesForSuite(nil, selected)
	if err != nil {
		t.Fatal(err)
	}
	record, err := IdentityRecordFromValidator(identities["validator-1"], "ipfs://validator-1")
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestMockIdentityRegistrationLookupDuplicateUnauthorizedAndMismatch(t *testing.T) {
	ctx := context.Background()
	backend := NewMockBackend("admin")
	record := testIdentityRecord(t)
	if err := backend.RegisterIdentity(ctx, "admin", record); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := backend.GetIdentity(ctx, record.ValidatorID)
	if err != nil || !ok {
		t.Fatalf("identity lookup failed: ok=%v err=%v", ok, err)
	}
	if loaded.SignatureKeyFingerprint != record.SignatureKeyFingerprint {
		t.Fatal("signature fingerprint mismatch")
	}
	if err := backend.RegisterIdentity(ctx, "admin", record); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate identity error, got %v", err)
	}
	record2 := record
	record2.ValidatorID = "validator-2"
	if err := backend.RegisterIdentity(ctx, "intruder", record2); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized identity error, got %v", err)
	}
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	identities, _ := identity.ValidatorIdentitiesForSuite(nil, selected)
	tampered := loaded
	tampered.Region = "wrong"
	if err := CompareIdentityToValidator(tampered, identities["validator-1"]); !errors.Is(err, ErrMismatch) {
		t.Fatalf("expected mismatch detection, got %v", err)
	}
}

func TestMockCredentialGovernanceAndQCAchors(t *testing.T) {
	ctx := context.Background()
	backend := NewMockBackend("admin")
	identityRecord := testIdentityRecord(t)
	if err := backend.RegisterIdentity(ctx, "admin", identityRecord); err != nil {
		t.Fatal(err)
	}
	credentialHash := messages.HashBytes([]byte("credential"))
	credential := CredentialRecord{CredentialHash: credentialHash, SubjectValidatorID: identityRecord.ValidatorID, IssuerValidatorID: identityRecord.ValidatorID, ValidFromTick: 1, ValidUntilTick: 10}
	if err := backend.AnchorCredential(ctx, "admin", credential); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := backend.GetCredential(ctx, credentialHash); err != nil || !ok {
		t.Fatalf("credential lookup failed: ok=%v err=%v", ok, err)
	}
	if err := backend.AnchorCredential(ctx, "admin", credential); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate credential error, got %v", err)
	}
	if err := backend.AnchorCredential(ctx, "intruder", CredentialRecord{CredentialHash: messages.HashBytes([]byte("other")), SubjectValidatorID: identityRecord.ValidatorID, IssuerValidatorID: identityRecord.ValidatorID}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized credential error, got %v", err)
	}

	proposalHash := messages.HashBytes([]byte("proposal"))
	proposal := GovernanceProposalRecord{ProposalHash: proposalHash, CreatorID: identityRecord.ValidatorID, MetadataHash: messages.HashBytes([]byte("proposal metadata")), State: ProposalStateAnchored}
	if err := backend.AnchorGovernanceProposal(ctx, "admin", proposal); err != nil {
		t.Fatal(err)
	}
	if err := backend.UpdateGovernanceProposalState(ctx, "admin", proposalHash, ProposalStateAccepted); err != nil {
		t.Fatal(err)
	}
	loadedProposal, ok, err := backend.GetGovernanceProposal(ctx, proposalHash)
	if err != nil || !ok || loadedProposal.State != ProposalStateAccepted {
		t.Fatalf("proposal lookup/update failed: ok=%v proposal=%+v err=%v", ok, loadedProposal, err)
	}
	if err := backend.AnchorGovernanceProposal(ctx, "admin", proposal); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate proposal error, got %v", err)
	}
	if err := backend.UpdateGovernanceProposalState(ctx, "intruder", proposalHash, ProposalStateExecuted); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized proposal update error, got %v", err)
	}

	qcRecord := QuorumCertificateRecord{QCHash: messages.HashBytes([]byte("qc")), Height: 2, Round: 0, BlockHash: messages.HashBytes([]byte("block")), Threshold: 5, SignerCount: 5}
	if err := backend.AnchorQuorumCertificate(ctx, "admin", qcRecord); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := backend.GetQuorumCertificateAnchor(ctx, qcRecord.QCHash); err != nil || !ok {
		t.Fatalf("qc lookup failed: ok=%v err=%v", ok, err)
	}
	if err := backend.AnchorQuorumCertificate(ctx, "admin", qcRecord); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate qc error, got %v", err)
	}
	if err := backend.AnchorQuorumCertificate(ctx, "intruder", QuorumCertificateRecord{QCHash: messages.HashBytes([]byte("qc2")), Height: 2, BlockHash: messages.HashBytes([]byte("block")), Threshold: 5, SignerCount: 5}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized qc error, got %v", err)
	}
}

func TestQuorumCertificateRecordFromCertificate(t *testing.T) {
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	voterIDs := identity.DefaultValidatorIDs()[:5]
	var votes []consensusprotocol.Vote
	for _, voterID := range voterIDs {
		signer, err := selected.NewSigner(voterID)
		if err != nil {
			t.Fatal(err)
		}
		vote, err := consensusprotocol.SignStageVote(2, 0, consensusprotocol.StagePrecommit, "block-hash", voterID, signer)
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	cert, err := consensusprotocol.FormStageQuorumCertificate(2, 0, consensusprotocol.StagePrecommit, "block-hash", votes, 5)
	if err != nil {
		t.Fatal(err)
	}
	record, err := QuorumCertificateRecordFromCertificate(cert, "", "metadata")
	if err != nil {
		t.Fatal(err)
	}
	if record.QCHash == "" || record.Height != 2 || record.Threshold != 5 || record.SignerCount != 5 {
		t.Fatalf("unexpected qc anchor record: %+v", record)
	}
	changedTimestamp := cert
	changedTimestamp.FormedAtUnixMilli += 100000
	stableHash, err := QuorumCertificateHash(changedTimestamp)
	if err != nil {
		t.Fatal(err)
	}
	if stableHash != record.QCHash {
		t.Fatal("qc anchor hash should ignore non-deterministic formation timestamp")
	}
}
