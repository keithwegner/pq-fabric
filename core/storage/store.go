package storage

import "errors"

const (
	ModeMemory  = "memory"
	ModeDurable = "durable"
	ModeSQLite  = "sqlite"
)

var ErrNotFound = errors.New("storage record not found")

type ValidatorState struct {
	NodeID             string `json:"node_id"`
	Region             string `json:"region"`
	Height             uint64 `json:"height"`
	Round              uint64 `json:"round"`
	LastHash           string `json:"last_hash"`
	StateDigest        string `json:"state_digest"`
	LockedHeight       uint64 `json:"locked_height,omitempty"`
	LockedRound        uint64 `json:"locked_round,omitempty"`
	LockedBlockHash    string `json:"locked_block_hash,omitempty"`
	CommitCount        int    `json:"commit_count"`
	IdentityKeyID      string `json:"identity_key_id"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	KEMAlgorithm       string `json:"kem_algorithm"`
	UpdatedAtUnixMilli int64  `json:"updated_at_unix_milli"`
}

type CommitRecord struct {
	Height             uint64 `json:"height"`
	Round              uint64 `json:"round"`
	BlockHash          string `json:"block_hash"`
	StateDigest        string `json:"state_digest"`
	CommitJSON         []byte `json:"commit_json"`
	CertificateJSON    []byte `json:"certificate_json"`
	IdentityKeyID      string `json:"identity_key_id"`
	CreatedAtUnixMilli int64  `json:"created_at_unix_milli"`
}

type IdempotencyRecord struct {
	ID                 string `json:"id"`
	ResultHash         string `json:"result_hash"`
	AppliedAtUnixMilli int64  `json:"applied_at_unix_milli"`
}

type SnapshotRecord struct {
	ID                 string `json:"id"`
	Height             uint64 `json:"height"`
	LastHash           string `json:"last_hash"`
	SnapshotJSON       []byte `json:"snapshot_json"`
	CreatedAtUnixMilli int64  `json:"created_at_unix_milli"`
}

type EvidenceRecord struct {
	EvidenceID         string `json:"evidence_id"`
	ReceiptID          string `json:"receipt_id"`
	EventHash          string `json:"event_hash"`
	QCHash             string `json:"qc_hash"`
	CommitHeight       uint64 `json:"commit_height"`
	SubmittingOrg      string `json:"submitting_organization"`
	IdempotencyKey     string `json:"idempotency_key"`
	ReceiptJSON        []byte `json:"receipt_json"`
	CreatedAtUnixMilli int64  `json:"created_at_unix_milli"`
}

type AuditRecord struct {
	RequestID          string `json:"request_id"`
	TimestampUnixMilli int64  `json:"timestamp_unix_milli"`
	PrincipalID        string `json:"principal_id,omitempty"`
	Organization       string `json:"organization,omitempty"`
	Method             string `json:"method"`
	Path               string `json:"path"`
	StatusCode         int    `json:"status_code"`
	DurationMillis     int64  `json:"duration_millis"`
	ClientAddr         string `json:"client_addr,omitempty"`
	DeniedReason       string `json:"denied_reason,omitempty"`
}

type ValidatorStore interface {
	LoadValidatorState() (ValidatorState, bool, error)
	SaveValidatorState(state ValidatorState) error
	SaveCommit(record CommitRecord, state ValidatorState) error
	ListCommits() ([]CommitRecord, error)
	RecentCommits(limit int) ([]CommitRecord, error)
	Commit(height uint64) (CommitRecord, bool, error)
	RecordIdempotency(id, resultHash string) (applied bool, existingResultHash string, err error)
	IdempotencyResult(id string) (string, bool, error)
	IdempotencyCount() (int, error)
	SaveSnapshot(record SnapshotRecord) error
	ListSnapshots() ([]SnapshotRecord, error)
	SaveEvidence(record EvidenceRecord) (created bool, existing EvidenceRecord, err error)
	EvidenceByID(evidenceID string) (EvidenceRecord, bool, error)
	EvidenceByReceiptID(receiptID string) (EvidenceRecord, bool, error)
	EvidenceByIdempotencyKey(idempotencyKey string) (EvidenceRecord, bool, error)
	EvidenceByQCHash(qcHash string) (EvidenceRecord, bool, error)
	ListEvidence() ([]EvidenceRecord, error)
	RecentEvidence(limit int) ([]EvidenceRecord, error)
	SaveAudit(record AuditRecord) error
	ListAudit(limit int) ([]AuditRecord, error)
	Close() error
}

func OpenValidatorStore(mode, dataDir string, databaseURL ...string) (ValidatorStore, error) {
	switch mode {
	case "", ModeMemory:
		return NewMemoryStore(), nil
	case ModeDurable:
		return OpenFileStore(dataDir)
	case ModeSQLite:
		dsn := dataDir
		if len(databaseURL) > 0 && databaseURL[0] != "" {
			dsn = databaseURL[0]
		}
		return OpenSQLiteStore(dsn)
	default:
		return nil, errors.New("unsupported storage mode: " + mode)
	}
}
