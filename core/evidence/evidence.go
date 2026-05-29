package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
	"github.com/keithwegner/pq-fabric/core/anchors"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
)

const (
	SchemaVersion = "pq-fabric.evidence.v1"

	VerificationValid   = "valid"
	VerificationInvalid = "invalid"

	AnchorNotRequested = "not_requested"
	AnchorPending      = "pending_testnet_anchor"
	AnchorUnavailable  = "unavailable"
)

var ErrInvalid = errors.New("invalid evidence")

type EvidenceSubmission struct {
	SchemaVersion          string `json:"schema_version"`
	EvidenceCategory       string `json:"evidence_category"`
	ArtifactHash           string `json:"artifact_hash"`
	MetadataHash           string `json:"metadata_hash"`
	SubmittingOrganization string `json:"submitting_organization"`
	IdempotencyKey         string `json:"idempotency_key"`
	AnchorRequested        bool   `json:"anchor_requested,omitempty"`
}

type EvidenceReceipt struct {
	ReceiptID          string                 `json:"receipt_id"`
	EvidenceID         string                 `json:"evidence_id"`
	Submission         EvidenceSubmission     `json:"submission"`
	EventHash          string                 `json:"event_hash"`
	CommitHeight       uint64                 `json:"commit_height"`
	CommitRound        uint64                 `json:"commit_round"`
	BlockHash          string                 `json:"block_hash"`
	QCHash             string                 `json:"qc_hash"`
	MembershipVersion  uint64                 `json:"membership_version,omitempty"`
	ValidatorSetHash   string                 `json:"validator_set_hash,omitempty"`
	SignerCount        int                    `json:"signer_count"`
	ValidatorIDs       []string               `json:"validator_ids"`
	VerificationStatus string                 `json:"verification_status"`
	AnchorStatus       string                 `json:"anchor_status"`
	AnchorTransaction  string                 `json:"anchor_transaction,omitempty"`
	Commit             protocol.CommitRequest `json:"commit"`
	CreatedAtUnixMilli int64                  `json:"created_at_unix_milli"`
}

type VerificationRequest struct {
	ReceiptID  string           `json:"receipt_id,omitempty"`
	EvidenceID string           `json:"evidence_id,omitempty"`
	Receipt    *EvidenceReceipt `json:"receipt,omitempty"`
}

type VerificationResult struct {
	Valid         bool   `json:"valid"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	ReceiptID     string `json:"receipt_id,omitempty"`
	EvidenceID    string `json:"evidence_id,omitempty"`
	ExpectedHash  string `json:"expected_hash,omitempty"`
	ObservedHash  string `json:"observed_hash,omitempty"`
	QuorumStatus  string `json:"quorum_status"`
	AnchorStatus  string `json:"anchor_status"`
	SignerCount   int    `json:"signer_count"`
	Threshold     int    `json:"threshold"`
	ValidatorSize int    `json:"validator_size"`
}

type AnchorStatus struct {
	QCHash            string `json:"qc_hash"`
	Status            string `json:"status"`
	ReceiptID         string `json:"receipt_id,omitempty"`
	EvidenceID        string `json:"evidence_id,omitempty"`
	AnchorTransaction string `json:"anchor_transaction,omitempty"`
	Message           string `json:"message"`
}

func NormalizeSubmission(submission EvidenceSubmission) EvidenceSubmission {
	submission.SchemaVersion = strings.TrimSpace(submission.SchemaVersion)
	if submission.SchemaVersion == "" {
		submission.SchemaVersion = SchemaVersion
	}
	submission.EvidenceCategory = strings.TrimSpace(submission.EvidenceCategory)
	submission.ArtifactHash = strings.TrimSpace(submission.ArtifactHash)
	submission.MetadataHash = strings.TrimSpace(submission.MetadataHash)
	submission.SubmittingOrganization = strings.TrimSpace(submission.SubmittingOrganization)
	submission.IdempotencyKey = strings.TrimSpace(submission.IdempotencyKey)
	return submission
}

func ValidateSubmission(submission EvidenceSubmission) error {
	submission = NormalizeSubmission(submission)
	if submission.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: unsupported schema version %q", ErrInvalid, submission.SchemaVersion)
	}
	if submission.EvidenceCategory == "" {
		return fmt.Errorf("%w: evidence category required", ErrInvalid)
	}
	if submission.ArtifactHash == "" {
		return fmt.Errorf("%w: artifact hash required", ErrInvalid)
	}
	if submission.MetadataHash == "" {
		return fmt.Errorf("%w: metadata hash required", ErrInvalid)
	}
	if submission.SubmittingOrganization == "" {
		return fmt.Errorf("%w: submitting organization required", ErrInvalid)
	}
	if submission.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency key required", ErrInvalid)
	}
	return nil
}

func EventHash(submission EvidenceSubmission) (string, error) {
	submission = NormalizeSubmission(submission)
	if err := ValidateSubmission(submission); err != nil {
		return "", err
	}
	return messages.HashCanonical(struct {
		Kind       string             `json:"kind"`
		Submission EvidenceSubmission `json:"submission"`
	}{
		Kind:       "pq-fabric/evidence/event/v1",
		Submission: submission,
	})
}

func SubmissionPayload(submission EvidenceSubmission) (string, error) {
	submission = NormalizeSubmission(submission)
	eventHash, err := EventHash(submission)
	if err != nil {
		return "", err
	}
	payload := struct {
		Kind       string             `json:"kind"`
		EventHash  string             `json:"event_hash"`
		Submission EvidenceSubmission `json:"submission"`
	}{
		Kind:       "pq-fabric/evidence/submission/v1",
		EventHash:  eventHash,
		Submission: submission,
	}
	data, err := messages.CanonicalJSON(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func SubmissionFromPayload(payload string) (EvidenceSubmission, bool, error) {
	var decoded struct {
		Kind       string             `json:"kind"`
		EventHash  string             `json:"event_hash"`
		Submission EvidenceSubmission `json:"submission"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return EvidenceSubmission{}, false, nil
	}
	if decoded.Kind != "pq-fabric/evidence/submission/v1" {
		return EvidenceSubmission{}, false, nil
	}
	submission := NormalizeSubmission(decoded.Submission)
	eventHash, err := EventHash(submission)
	if err != nil {
		return EvidenceSubmission{}, true, err
	}
	if decoded.EventHash != eventHash {
		return EvidenceSubmission{}, true, fmt.Errorf("%w: payload event hash mismatch", ErrInvalid)
	}
	return submission, true, nil
}

func NewReceipt(submission EvidenceSubmission, commit protocol.CommitRequest) (EvidenceReceipt, error) {
	submission = NormalizeSubmission(submission)
	payload, err := SubmissionPayload(submission)
	if err != nil {
		return EvidenceReceipt{}, err
	}
	if commit.Block.Payload != payload {
		return EvidenceReceipt{}, fmt.Errorf("%w: commit payload does not match evidence submission", ErrInvalid)
	}
	eventHash, err := EventHash(submission)
	if err != nil {
		return EvidenceReceipt{}, err
	}
	blockHash, err := commit.Block.Hash()
	if err != nil {
		return EvidenceReceipt{}, err
	}
	qcHash, err := anchors.QuorumCertificateHash(commit.Certificate)
	if err != nil {
		return EvidenceReceipt{}, err
	}
	receiptID, err := ReceiptID(eventHash, blockHash, qcHash)
	if err != nil {
		return EvidenceReceipt{}, err
	}
	anchorStatus := AnchorNotRequested
	if submission.AnchorRequested {
		anchorStatus = AnchorPending
	}
	return EvidenceReceipt{
		ReceiptID:          receiptID,
		EvidenceID:         eventHash,
		Submission:         submission,
		EventHash:          eventHash,
		CommitHeight:       commit.Block.Height,
		CommitRound:        commit.Block.Round,
		BlockHash:          blockHash,
		QCHash:             qcHash,
		MembershipVersion:  commit.Block.MembershipVersion,
		ValidatorSetHash:   commit.Block.ValidatorSetHash,
		SignerCount:        len(uniqueValidatorIDs(commit.Certificate.Votes)),
		ValidatorIDs:       uniqueValidatorIDs(commit.Certificate.Votes),
		VerificationStatus: VerificationValid,
		AnchorStatus:       anchorStatus,
		Commit:             commit,
		CreatedAtUnixMilli: time.Now().UnixMilli(),
	}, nil
}

func ReceiptID(eventHash, blockHash, qcHash string) (string, error) {
	return messages.HashCanonical(struct {
		Kind      string `json:"kind"`
		EventHash string `json:"event_hash"`
		BlockHash string `json:"block_hash"`
		QCHash    string `json:"qc_hash"`
	}{
		Kind:      "pq-fabric/evidence/receipt/v1",
		EventHash: eventHash,
		BlockHash: blockHash,
		QCHash:    qcHash,
	})
}

func VerifyReceipt(receipt EvidenceReceipt, identities map[string]identity.ValidatorIdentity, verifier pqcrypto.SignatureVerifier, threshold int) VerificationResult {
	result := VerificationResult{
		Status:        VerificationInvalid,
		ReceiptID:     receipt.ReceiptID,
		EvidenceID:    receipt.EvidenceID,
		ExpectedHash:  receipt.EventHash,
		QuorumStatus:  "not_checked",
		AnchorStatus:  receipt.AnchorStatus,
		SignerCount:   receipt.SignerCount,
		Threshold:     threshold,
		ValidatorSize: len(identities),
	}
	observedEventHash, err := EventHash(receipt.Submission)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	result.ObservedHash = observedEventHash
	if receipt.EventHash != observedEventHash || receipt.EvidenceID != observedEventHash {
		result.Reason = "receipt event hash does not match submission"
		return result
	}
	payload, err := SubmissionPayload(receipt.Submission)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	if receipt.Commit.Block.Payload != payload {
		result.Reason = "receipt commit payload does not match submission"
		return result
	}
	blockHash, err := receipt.Commit.Block.Hash()
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	if receipt.BlockHash != blockHash || receipt.Commit.Certificate.BlockHash != blockHash {
		result.Reason = "receipt block hash does not match commit"
		return result
	}
	qcHash, err := anchors.QuorumCertificateHash(receipt.Commit.Certificate)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	if receipt.QCHash != qcHash {
		result.Reason = "receipt quorum certificate hash does not match commit certificate"
		return result
	}
	receiptID, err := ReceiptID(receipt.EventHash, receipt.BlockHash, receipt.QCHash)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	if receipt.ReceiptID != receiptID {
		result.Reason = "receipt id does not match event, block, and quorum certificate hashes"
		return result
	}
	if receipt.CommitHeight != receipt.Commit.Block.Height || receipt.CommitRound != receipt.Commit.Block.Round {
		result.Reason = "receipt commit height or round does not match commit"
		return result
	}
	if receipt.MembershipVersion != receipt.Commit.Block.MembershipVersion || receipt.ValidatorSetHash != receipt.Commit.Block.ValidatorSetHash {
		result.Reason = "receipt membership version or validator set hash does not match commit block"
		return result
	}
	if receipt.Commit.Block.MembershipVersion != receipt.Commit.Certificate.MembershipVersion || receipt.Commit.Block.ValidatorSetHash != receipt.Commit.Certificate.ValidatorSetHash {
		result.Reason = "commit block and quorum certificate membership metadata differ"
		return result
	}
	if threshold <= 0 {
		threshold = receipt.Commit.Certificate.Threshold
		result.Threshold = threshold
	}
	if len(identities) > 0 && verifier != nil {
		if err := protocol.VerifyQuorumCertificate(receipt.Commit.Certificate, identities, verifier); err != nil {
			result.QuorumStatus = "invalid"
			result.Reason = err.Error()
			return result
		}
		result.QuorumStatus = "valid"
	} else if receipt.Commit.Certificate.Threshold >= threshold && len(uniqueValidatorIDs(receipt.Commit.Certificate.Votes)) >= threshold {
		result.QuorumStatus = "structurally_present"
	} else {
		result.QuorumStatus = "insufficient"
		result.Reason = "quorum certificate has insufficient unique signers"
		return result
	}
	if receipt.SignerCount != len(uniqueValidatorIDs(receipt.Commit.Certificate.Votes)) {
		result.Reason = "receipt signer count does not match quorum certificate"
		return result
	}
	if !equalStringSlices(receipt.ValidatorIDs, uniqueValidatorIDs(receipt.Commit.Certificate.Votes)) {
		result.Reason = "receipt validator id list does not match quorum certificate"
		return result
	}
	result.Valid = true
	result.Status = VerificationValid
	result.Reason = "receipt is valid"
	return result
}

func MarshalReceipt(receipt EvidenceReceipt) ([]byte, error) {
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func UnmarshalReceipt(data []byte) (EvidenceReceipt, error) {
	var receipt EvidenceReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return EvidenceReceipt{}, err
	}
	return receipt, nil
}

func uniqueValidatorIDs(votes []protocol.Vote) []string {
	seen := make(map[string]struct{}, len(votes))
	for _, vote := range votes {
		if vote.VoterID != "" {
			seen[vote.VoterID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
