package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
	consensusstate "github.com/keithwegner/pq-fabric/consensus/state"
	"github.com/keithwegner/pq-fabric/core/consortium"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	evidencepkg "github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func TestBackupSQLiteCreatesRestorableVerifiedEvidence(t *testing.T) {
	tmp := t.TempDir()
	sourceDB := filepath.Join(tmp, "source", "validator.db")
	backupDB := filepath.Join(tmp, "backup", "validator.db")
	manifest, receipt := testReceipt(t)
	manifestPath := filepath.Join(tmp, "manifest.json")
	writeJSON(t, manifestPath, manifest)
	store, err := storage.OpenSQLiteStore(sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	commitJSON, err := json.Marshal(receipt.Commit)
	if err != nil {
		t.Fatal(err)
	}
	certificateJSON, err := json.Marshal(receipt.Commit.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCommit(storage.CommitRecord{
		Height:          receipt.CommitHeight,
		Round:           receipt.CommitRound,
		BlockHash:       receipt.BlockHash,
		StateDigest:     receipt.Commit.Block.StateDigest,
		CommitJSON:      commitJSON,
		CertificateJSON: certificateJSON,
		IdentityKeyID:   "test-key",
	}, storage.ValidatorState{NodeID: "validator-1", Region: "nyc", Height: receipt.CommitHeight, LastHash: receipt.BlockHash, CommitCount: 1}); err != nil {
		t.Fatal(err)
	}
	receiptJSON, err := evidencepkg.MarshalReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveEvidence(storage.EvidenceRecord{
		EvidenceID:     receipt.EvidenceID,
		ReceiptID:      receipt.ReceiptID,
		EventHash:      receipt.EventHash,
		QCHash:         receipt.QCHash,
		CommitHeight:   receipt.CommitHeight,
		SubmittingOrg:  receipt.Submission.SubmittingOrganization,
		IdempotencyKey: receipt.Submission.IdempotencyKey,
		ReceiptJSON:    receiptJSON,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RecordIdempotency(receipt.Submission.IdempotencyKey, receipt.ReceiptID); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAudit(storage.AuditRecord{RequestID: "request-1", TimestampUnixMilli: 1, Method: "POST", Path: "/v1/evidence", StatusCode: 200}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	report, err := BackupSQLite(context.Background(), SQLiteOptions{
		SourceDatabase:  sourceDB,
		BackupDatabase:  backupDB,
		ManifestPath:    manifestPath,
		ManifestHistory: manifestPath,
		ReceiptLimit:    5,
	})
	if err != nil {
		t.Fatalf("backup failed: %v\n%+v", err, report)
	}
	if report.Status != "pass" || report.VerificationStatus != "valid" || report.VerifiedReceipts != 1 || report.CommitCount != 1 || report.EvidenceCount != 1 {
		t.Fatalf("unexpected backup report: %+v", report)
	}
	restore, err := CheckSQLiteRestore(context.Background(), SQLiteOptions{BackupDatabase: backupDB, ManifestPath: manifestPath, ManifestHistory: manifestPath, ReceiptLimit: 5})
	if err != nil {
		t.Fatalf("restore check failed: %v\n%+v", err, restore)
	}
	if restore.Status != "pass" || restore.VerifiedReceipts != 1 || restore.VerificationMode != "manifest-history" {
		t.Fatalf("unexpected restore report: %+v", restore)
	}
}

func testReceipt(t *testing.T) (consortium.Manifest, evidencepkg.EvidenceReceipt) {
	t.Helper()
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	identities, err := identity.ValidatorIdentitiesForSuite(map[string]string{}, selected)
	if err != nil {
		t.Fatal(err)
	}
	manifest := consortium.ManifestFromIdentities("backup-test-consortium", 1, 5, identities, identity.DefaultValidatorIDs())
	manifestHash, err := manifest.Hash()
	if err != nil {
		t.Fatal(err)
	}
	submission := evidencepkg.EvidenceSubmission{
		SchemaVersion:          evidencepkg.SchemaVersion,
		EvidenceCategory:       "backup-test",
		ArtifactHash:           "sha256:backup-artifact",
		MetadataHash:           "sha256:backup-metadata",
		SubmittingOrganization: "backup-test",
		IdempotencyKey:         "backup-test-idempotency",
	}
	payload, err := evidencepkg.SubmissionPayload(submission)
	if err != nil {
		t.Fatal(err)
	}
	transactions := consensusstate.TransactionsFromPayload(payload)
	machine := consensusstate.NewMachine()
	stateDigest, _, err := machine.Apply(transactions)
	if err != nil {
		t.Fatal(err)
	}
	block := protocol.NewRoundBlock(1, 0, protocol.GenesisHash, payload, "validator-1", stateDigest)
	block.Transactions = transactions
	block.MembershipVersion = manifest.MembershipVersion
	block.ValidatorSetHash = manifestHash
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	votes := make([]protocol.Vote, 0, 5)
	for _, id := range identity.DefaultValidatorIDs()[:5] {
		vote, err := protocol.SignStageVote(1, 0, protocol.StagePrecommit, blockHash, id, pqcrypto.NewDeterministicDevSigner(id))
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	qc, err := protocol.FormStageQuorumCertificate(1, 0, protocol.StagePrecommit, blockHash, votes, manifest.QuorumThreshold)
	if err != nil {
		t.Fatal(err)
	}
	qc.MembershipVersion = manifest.MembershipVersion
	qc.ValidatorSetHash = manifestHash
	receipt, err := evidencepkg.NewReceipt(submission, protocol.CommitRequest{Block: block, Certificate: qc})
	if err != nil {
		t.Fatal(err)
	}
	return manifest, receipt
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
