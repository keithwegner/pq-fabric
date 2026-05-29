package evidence

import (
	"testing"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
	consensusstate "github.com/keithwegner/pq-fabric/consensus/state"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
)

func TestSubmissionPayloadRoundTripAndReceiptVerification(t *testing.T) {
	submission := testSubmission()
	payload, err := SubmissionPayload(submission)
	if err != nil {
		t.Fatal(err)
	}
	decoded, ok, err := SubmissionFromPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected evidence payload")
	}
	if decoded.ArtifactHash != submission.ArtifactHash {
		t.Fatalf("unexpected decoded submission: %+v", decoded)
	}
	commit := makeEvidenceCommit(t, payload)
	receipt, err := NewReceipt(submission, commit)
	if err != nil {
		t.Fatal(err)
	}
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	identities, err := identity.ValidatorIdentitiesForSuite(map[string]string{}, selected)
	if err != nil {
		t.Fatal(err)
	}
	result := VerifyReceipt(receipt, identities, selected.NewVerifier(), 5)
	if !result.Valid || result.Status != VerificationValid {
		t.Fatalf("expected valid receipt, got %+v", result)
	}
}

func TestValidateSubmissionRejectsPayloadlessEvidence(t *testing.T) {
	submission := testSubmission()
	submission.MetadataHash = ""
	if err := ValidateSubmission(submission); err == nil {
		t.Fatal("expected missing metadata hash to fail")
	}
}

func TestVerifyReceiptRejectsTamperedSubmission(t *testing.T) {
	submission := testSubmission()
	payload, err := SubmissionPayload(submission)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := NewReceipt(submission, makeEvidenceCommit(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	receipt.Submission.ArtifactHash = "tampered"
	result := VerifyReceipt(receipt, nil, nil, 5)
	if result.Valid {
		t.Fatalf("expected tampered receipt to fail: %+v", result)
	}
}

func TestVerifyReceiptRejectsTamperedReceiptID(t *testing.T) {
	submission := testSubmission()
	payload, err := SubmissionPayload(submission)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := NewReceipt(submission, makeEvidenceCommit(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	receipt.ReceiptID = "tampered"
	result := VerifyReceipt(receipt, nil, nil, 5)
	if result.Valid {
		t.Fatalf("expected tampered receipt id to fail: %+v", result)
	}
}

func testSubmission() EvidenceSubmission {
	return EvidenceSubmission{
		SchemaVersion:          SchemaVersion,
		EvidenceCategory:       "incident-report",
		ArtifactHash:           "sha256:0123456789abcdef",
		MetadataHash:           "sha256:abcdef0123456789",
		SubmittingOrganization: "bestgate",
		IdempotencyKey:         "incident-123",
		AnchorRequested:        true,
	}
}

func makeEvidenceCommit(t *testing.T, payload string) protocol.CommitRequest {
	t.Helper()
	machine := consensusstate.NewMachine()
	transactions := consensusstate.TransactionsFromPayload(payload)
	stateDigest, _, err := machine.Apply(transactions)
	if err != nil {
		t.Fatal(err)
	}
	block := protocol.NewRoundBlock(1, 0, protocol.GenesisHash, payload, "validator-1", stateDigest)
	block.Transactions = transactions
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	votes := make([]protocol.Vote, 0, 5)
	for _, id := range []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5"} {
		vote, err := protocol.SignStageVote(1, 0, protocol.StagePrecommit, blockHash, id, pqcrypto.NewDeterministicDevSigner(id))
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	cert, err := protocol.FormStageQuorumCertificate(1, 0, protocol.StagePrecommit, blockHash, votes, 5)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.CommitRequest{Block: block, Certificate: cert}
}
