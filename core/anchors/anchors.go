package anchors

import (
	"context"
	"errors"
	"fmt"
	"sort"

	consensusprotocol "github.com/keithwegner/pq-fabric/consensus/protocol"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
)

const (
	RoleIdentityAdmin    = "identity_admin"
	RoleCredentialIssuer = "credential_issuer"
	RoleGovernanceAdmin  = "governance_admin"
	RoleQCAnchorer       = "qc_anchorer"

	ProposalStateAnchored = "anchored"
	ProposalStateAccepted = "accepted"
	ProposalStateRejected = "rejected"
	ProposalStateExecuted = "executed"
)

var (
	ErrUnauthorized = errors.New("anchor operation unauthorized")
	ErrInvalid      = errors.New("invalid anchor record")
	ErrDuplicate    = errors.New("anchor record already exists")
	ErrNotFound     = errors.New("anchor record not found")
	ErrMismatch     = errors.New("anchor record mismatch")
)

type Client interface {
	RegisterIdentity(ctx context.Context, actor string, record IdentityRecord) error
	UpdateIdentity(ctx context.Context, actor string, record IdentityRecord) error
	GetIdentity(ctx context.Context, validatorID string) (IdentityRecord, bool, error)
	AnchorCredential(ctx context.Context, actor string, record CredentialRecord) error
	GetCredential(ctx context.Context, credentialHash string) (CredentialRecord, bool, error)
	AnchorGovernanceProposal(ctx context.Context, actor string, record GovernanceProposalRecord) error
	UpdateGovernanceProposalState(ctx context.Context, actor, proposalHash, state string) error
	GetGovernanceProposal(ctx context.Context, proposalHash string) (GovernanceProposalRecord, bool, error)
	AnchorQuorumCertificate(ctx context.Context, actor string, record QuorumCertificateRecord) error
	GetQuorumCertificateAnchor(ctx context.Context, qcHash string) (QuorumCertificateRecord, bool, error)
	Status(ctx context.Context) (Status, error)
}

type IdentityRecord struct {
	ValidatorID             string `json:"validator_id"`
	Region                  string `json:"region"`
	SignatureAlgorithm      string `json:"signature_algorithm"`
	SignatureKeyFingerprint string `json:"signature_key_fingerprint"`
	KEMAlgorithm            string `json:"kem_algorithm"`
	KEMKeyFingerprint       string `json:"kem_key_fingerprint"`
	MetadataURI             string `json:"metadata_uri,omitempty"`
	MetadataHash            string `json:"metadata_hash"`
	UpdatedTick             uint64 `json:"updated_tick,omitempty"`
}

type CredentialRecord struct {
	CredentialHash     string `json:"credential_hash"`
	SubjectValidatorID string `json:"subject_validator_id"`
	IssuerValidatorID  string `json:"issuer_validator_id"`
	ValidFromTick      uint64 `json:"valid_from_tick,omitempty"`
	ValidUntilTick     uint64 `json:"valid_until_tick,omitempty"`
	MetadataHash       string `json:"metadata_hash,omitempty"`
	AnchoredTick       uint64 `json:"anchored_tick,omitempty"`
}

type GovernanceProposalRecord struct {
	ProposalHash string `json:"proposal_hash"`
	CreatorID    string `json:"creator_id"`
	MetadataURI  string `json:"metadata_uri,omitempty"`
	MetadataHash string `json:"metadata_hash,omitempty"`
	State        string `json:"state"`
	CreatedTick  uint64 `json:"created_tick,omitempty"`
	UpdatedTick  uint64 `json:"updated_tick,omitempty"`
}

type QuorumCertificateRecord struct {
	QCHash       string `json:"qc_hash"`
	Height       uint64 `json:"height"`
	Round        uint64 `json:"round"`
	BlockHash    string `json:"block_hash,omitempty"`
	EventHash    string `json:"event_hash,omitempty"`
	Threshold    int    `json:"threshold"`
	SignerCount  int    `json:"signer_count"`
	MetadataHash string `json:"metadata_hash,omitempty"`
	AnchoredTick uint64 `json:"anchored_tick,omitempty"`
}

type Status struct {
	Backend                 string `json:"backend"`
	IdentityCount           int    `json:"identity_count"`
	CredentialCount         int    `json:"credential_count"`
	GovernanceProposalCount int    `json:"governance_proposal_count"`
	QuorumCertificateCount  int    `json:"quorum_certificate_count"`
	Configured              bool   `json:"configured"`
}

func IdentityRecordFromValidator(v identity.ValidatorIdentity, metadataURI string) (IdentityRecord, error) {
	signatureAlgorithm := v.SignatureAlgorithmName()
	signatureFingerprint := identity.PublicKeyFingerprint(signatureAlgorithm, v.SignaturePublicKeyBytes())
	kemFingerprint := identity.PublicKeyFingerprint(v.KEMAlgorithm, v.KEMPublicKey)
	record := IdentityRecord{
		ValidatorID:             v.ID,
		Region:                  v.Region,
		SignatureAlgorithm:      signatureAlgorithm,
		SignatureKeyFingerprint: signatureFingerprint,
		KEMAlgorithm:            v.KEMAlgorithm,
		KEMKeyFingerprint:       kemFingerprint,
		MetadataURI:             metadataURI,
	}
	metadataHash, err := messages.HashCanonical(struct {
		ValidatorID             string `json:"validator_id"`
		Region                  string `json:"region"`
		SignatureAlgorithm      string `json:"signature_algorithm"`
		SignatureKeyFingerprint string `json:"signature_key_fingerprint"`
		KEMAlgorithm            string `json:"kem_algorithm"`
		KEMKeyFingerprint       string `json:"kem_key_fingerprint"`
		MetadataURI             string `json:"metadata_uri,omitempty"`
	}{
		ValidatorID:             record.ValidatorID,
		Region:                  record.Region,
		SignatureAlgorithm:      record.SignatureAlgorithm,
		SignatureKeyFingerprint: record.SignatureKeyFingerprint,
		KEMAlgorithm:            record.KEMAlgorithm,
		KEMKeyFingerprint:       record.KEMKeyFingerprint,
		MetadataURI:             record.MetadataURI,
	})
	if err != nil {
		return IdentityRecord{}, err
	}
	record.MetadataHash = metadataHash
	return record, ValidateIdentityRecord(record)
}

func CompareIdentityToValidator(record IdentityRecord, v identity.ValidatorIdentity) error {
	expected, err := IdentityRecordFromValidator(v, record.MetadataURI)
	if err != nil {
		return err
	}
	if record.ValidatorID != expected.ValidatorID ||
		record.Region != expected.Region ||
		record.SignatureAlgorithm != expected.SignatureAlgorithm ||
		record.SignatureKeyFingerprint != expected.SignatureKeyFingerprint ||
		record.KEMAlgorithm != expected.KEMAlgorithm ||
		record.KEMKeyFingerprint != expected.KEMKeyFingerprint ||
		record.MetadataHash != expected.MetadataHash {
		return fmt.Errorf("%w: validator identity anchor does not match local metadata for %s", ErrMismatch, v.ID)
	}
	return nil
}

func QuorumCertificateHash(cert consensusprotocol.QuorumCertificate) (string, error) {
	votes := append([]consensusprotocol.Vote(nil), cert.Votes...)
	sort.Slice(votes, func(i, j int) bool {
		return votes[i].VoterID < votes[j].VoterID
	})
	return messages.HashCanonical(struct {
		Height             uint64                   `json:"height"`
		Round              uint64                   `json:"round"`
		Stage              string                   `json:"stage"`
		BlockHash          string                   `json:"block_hash"`
		Decision           string                   `json:"decision"`
		Threshold          int                      `json:"threshold"`
		ValidatorVoteCount int                      `json:"validator_vote_count"`
		Votes              []consensusprotocol.Vote `json:"votes"`
	}{
		Height:             cert.Height,
		Round:              cert.Round,
		Stage:              consensusprotocol.NormalizeStage(cert.Stage),
		BlockHash:          cert.BlockHash,
		Decision:           cert.Decision,
		Threshold:          cert.Threshold,
		ValidatorVoteCount: cert.ValidatorVoteCount,
		Votes:              votes,
	})
}

func QuorumCertificateRecordFromCertificate(cert consensusprotocol.QuorumCertificate, eventHash, metadataHash string) (QuorumCertificateRecord, error) {
	qcHash, err := QuorumCertificateHash(cert)
	if err != nil {
		return QuorumCertificateRecord{}, err
	}
	record := QuorumCertificateRecord{
		QCHash:       qcHash,
		Height:       cert.Height,
		Round:        cert.Round,
		BlockHash:    cert.BlockHash,
		EventHash:    eventHash,
		Threshold:    cert.Threshold,
		SignerCount:  len(cert.Votes),
		MetadataHash: metadataHash,
	}
	return record, ValidateQuorumCertificateRecord(record)
}

func ValidateIdentityRecord(record IdentityRecord) error {
	if record.ValidatorID == "" {
		return fmt.Errorf("%w: validator id required", ErrInvalid)
	}
	if record.SignatureAlgorithm == "" || record.KEMAlgorithm == "" {
		return fmt.Errorf("%w: signature and KEM algorithms required", ErrInvalid)
	}
	if record.SignatureKeyFingerprint == "" || record.KEMKeyFingerprint == "" {
		return fmt.Errorf("%w: key fingerprints required", ErrInvalid)
	}
	if record.MetadataHash == "" {
		return fmt.Errorf("%w: metadata hash required", ErrInvalid)
	}
	return nil
}

func ValidateCredentialRecord(record CredentialRecord) error {
	if record.CredentialHash == "" {
		return fmt.Errorf("%w: credential hash required", ErrInvalid)
	}
	if record.SubjectValidatorID == "" || record.IssuerValidatorID == "" {
		return fmt.Errorf("%w: subject and issuer identities required", ErrInvalid)
	}
	if record.ValidUntilTick > 0 && record.ValidUntilTick < record.ValidFromTick {
		return fmt.Errorf("%w: invalid credential validity window", ErrInvalid)
	}
	return nil
}

func ValidateGovernanceProposalRecord(record GovernanceProposalRecord) error {
	if record.ProposalHash == "" {
		return fmt.Errorf("%w: proposal hash required", ErrInvalid)
	}
	if record.CreatorID == "" {
		return fmt.Errorf("%w: proposal creator required", ErrInvalid)
	}
	if record.State == "" {
		return fmt.Errorf("%w: proposal state required", ErrInvalid)
	}
	return nil
}

func ValidateQuorumCertificateRecord(record QuorumCertificateRecord) error {
	if record.QCHash == "" {
		return fmt.Errorf("%w: quorum certificate hash required", ErrInvalid)
	}
	if record.BlockHash == "" && record.EventHash == "" {
		return fmt.Errorf("%w: block hash or event hash required", ErrInvalid)
	}
	if record.Threshold <= 0 || record.SignerCount < record.Threshold {
		return fmt.Errorf("%w: invalid quorum metadata", ErrInvalid)
	}
	return nil
}

func SortedIdentityRecords(records map[string]IdentityRecord) []IdentityRecord {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]IdentityRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, records[key])
	}
	return out
}
