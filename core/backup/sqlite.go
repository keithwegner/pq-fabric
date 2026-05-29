package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keithwegner/pq-fabric/core/consortium"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	"github.com/keithwegner/pq-fabric/core/crypto/dev"
	"github.com/keithwegner/pq-fabric/core/crypto/mldsa"
	evidencepkg "github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/storage"
	_ "modernc.org/sqlite"
)

const reportLimitations = "Local SQLite backup and restore evidence only; no managed backup service, cloud snapshot, secret-manager fetch, Kubernetes apply, image signing, or certification claim."

type SQLiteOptions struct {
	SourceDatabase  string
	BackupDatabase  string
	ManifestPath    string
	ManifestHistory string
	ReceiptLimit    int
	Force           bool
}

type SQLiteReport struct {
	Status               string                        `json:"status"`
	GeneratedAtUnixMilli int64                         `json:"generated_at_unix_milli"`
	SourceDatabase       string                        `json:"source_database,omitempty"`
	BackupDatabase       string                        `json:"backup_database,omitempty"`
	SourceSHA256         string                        `json:"source_sha256,omitempty"`
	BackupSHA256         string                        `json:"backup_sha256,omitempty"`
	IntegrityCheck       string                        `json:"integrity_check,omitempty"`
	Migration            storage.SQLiteMigrationReport `json:"migration"`
	SchemaVersion        int                           `json:"schema_version"`
	CommitCount          int                           `json:"commit_count"`
	EvidenceCount        int                           `json:"evidence_count"`
	AuditCount           int                           `json:"audit_count"`
	IdempotencyCount     int                           `json:"idempotency_count"`
	VerificationMode     string                        `json:"verification_mode"`
	VerificationStatus   string                        `json:"verification_status"`
	VerifiedReceipts     int                           `json:"verified_receipts"`
	InvalidReceipts      int                           `json:"invalid_receipts"`
	ReceiptChecks        []ReceiptCheck                `json:"receipt_checks,omitempty"`
	Message              string                        `json:"message"`
	Limitations          string                        `json:"limitations"`
}

type ReceiptCheck struct {
	ReceiptID         string `json:"receipt_id"`
	EvidenceID        string `json:"evidence_id"`
	CommitHeight      uint64 `json:"commit_height"`
	SignerCount       int    `json:"signer_count"`
	MembershipVersion uint64 `json:"membership_version,omitempty"`
	ValidatorSetHash  string `json:"validator_set_hash,omitempty"`
	Valid             bool   `json:"valid"`
	Status            string `json:"status"`
	QuorumStatus      string `json:"quorum_status"`
	Reason            string `json:"reason,omitempty"`
}

func BackupSQLite(ctx context.Context, opts SQLiteOptions) (SQLiteReport, error) {
	opts.ReceiptLimit = normalizeLimit(opts.ReceiptLimit)
	source := strings.TrimSpace(opts.SourceDatabase)
	backup := strings.TrimSpace(opts.BackupDatabase)
	report := SQLiteReport{
		Status:               "fail",
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		SourceDatabase:       source,
		BackupDatabase:       backup,
		SchemaVersion:        storage.SQLiteSchemaVersion,
		Limitations:          reportLimitations,
	}
	if source == "" {
		report.Message = "source database is required"
		return report, errors.New(report.Message)
	}
	if backup == "" {
		report.Message = "backup database is required"
		return report, errors.New(report.Message)
	}
	if !opts.Force && fileExists(backup) {
		report.Message = "backup database already exists; pass --force to replace it"
		return report, errors.New(report.Message)
	}
	migration, err := storage.CheckSQLiteMigrations(source, true)
	report.Migration = migration
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	if migration.Status == "fail" {
		report.Message = migration.Message
		return report, errors.New(migration.Message)
	}
	if integrity, err := storage.SQLiteIntegrityCheck(source); err != nil {
		report.IntegrityCheck = integrity
		report.Message = err.Error()
		return report, err
	} else {
		report.IntegrityCheck = integrity
	}
	if opts.Force {
		_ = os.Remove(backup)
		_ = os.Remove(backup + "-wal")
		_ = os.Remove(backup + "-shm")
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		report.Message = err.Error()
		return report, err
	}
	if err := vacuumSQLiteInto(ctx, source, backup); err != nil {
		report.Message = err.Error()
		return report, err
	}
	sourceHash, _ := fileSHA256(source)
	backupHash, _ := fileSHA256(backup)
	report.SourceSHA256 = sourceHash
	report.BackupSHA256 = backupHash

	checked, err := CheckSQLiteRestore(ctx, SQLiteOptions{
		BackupDatabase:  backup,
		ManifestPath:    opts.ManifestPath,
		ManifestHistory: opts.ManifestHistory,
		ReceiptLimit:    opts.ReceiptLimit,
	})
	checked.SourceDatabase = source
	checked.SourceSHA256 = sourceHash
	if checked.BackupSHA256 == "" {
		checked.BackupSHA256 = backupHash
	}
	if err != nil {
		return checked, err
	}
	checked.Message = "SQLite backup created and restored receipt verification completed"
	return checked, nil
}

func CheckSQLiteRestore(ctx context.Context, opts SQLiteOptions) (SQLiteReport, error) {
	_ = ctx
	opts.ReceiptLimit = normalizeLimit(opts.ReceiptLimit)
	dbPath := firstNonEmpty(opts.BackupDatabase, opts.SourceDatabase)
	report := SQLiteReport{
		Status:               "fail",
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		BackupDatabase:       dbPath,
		SchemaVersion:        storage.SQLiteSchemaVersion,
		Limitations:          reportLimitations,
	}
	if strings.TrimSpace(dbPath) == "" {
		report.Message = "database is required"
		return report, errors.New(report.Message)
	}
	migration, err := storage.CheckSQLiteMigrations(dbPath, true)
	report.Migration = migration
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	if migration.Status == "fail" {
		report.Message = migration.Message
		return report, errors.New(migration.Message)
	}
	integrity, err := storage.SQLiteIntegrityCheck(dbPath)
	report.IntegrityCheck = integrity
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	report.BackupSHA256, _ = fileSHA256(dbPath)
	openPath, cleanup, err := restoreWorkingCopy(dbPath)
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	defer cleanup()
	store, err := storage.OpenSQLiteStore(openPath)
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	defer store.Close()
	commits, err := store.ListCommits()
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	evidenceRecords, err := store.ListEvidence()
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	audit, err := store.ListAudit(0)
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	idempotencyCount, err := store.IdempotencyCount()
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	report.CommitCount = len(commits)
	report.EvidenceCount = len(evidenceRecords)
	report.AuditCount = len(audit)
	report.IdempotencyCount = idempotencyCount

	history, manifestBacked, err := loadManifestHistory(opts.ManifestPath, opts.ManifestHistory)
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	if manifestBacked {
		report.VerificationMode = "manifest-history"
	} else {
		report.VerificationMode = "structural"
	}
	recent, err := store.RecentEvidence(opts.ReceiptLimit)
	if err != nil {
		report.Message = err.Error()
		return report, err
	}
	for _, record := range recent {
		receipt, err := evidencepkg.UnmarshalReceipt(record.ReceiptJSON)
		if err != nil {
			report.InvalidReceipts++
			report.ReceiptChecks = append(report.ReceiptChecks, ReceiptCheck{
				ReceiptID:  record.ReceiptID,
				EvidenceID: record.EvidenceID,
				Valid:      false,
				Status:     evidencepkg.VerificationInvalid,
				Reason:     err.Error(),
			})
			continue
		}
		result := verifyReceipt(receipt, history, manifestBacked)
		check := ReceiptCheck{
			ReceiptID:         receipt.ReceiptID,
			EvidenceID:        receipt.EvidenceID,
			CommitHeight:      receipt.CommitHeight,
			SignerCount:       receipt.SignerCount,
			MembershipVersion: receipt.MembershipVersion,
			ValidatorSetHash:  receipt.ValidatorSetHash,
			Valid:             result.Valid,
			Status:            result.Status,
			QuorumStatus:      result.QuorumStatus,
			Reason:            result.Reason,
		}
		report.ReceiptChecks = append(report.ReceiptChecks, check)
		if result.Valid {
			report.VerifiedReceipts++
		} else {
			report.InvalidReceipts++
		}
	}
	switch {
	case report.InvalidReceipts > 0:
		report.Status = "fail"
		report.VerificationStatus = "invalid"
		report.Message = "one or more restored receipts failed verification"
		return report, errors.New(report.Message)
	case len(recent) == 0:
		report.Status = "pass"
		report.VerificationStatus = "no_receipts"
		report.Message = "SQLite database opened and integrity check passed; no receipts were available for spot-check verification"
	default:
		report.Status = "pass"
		report.VerificationStatus = "valid"
		report.Message = "SQLite restore verification completed"
	}
	return report, nil
}

func Text(report SQLiteReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric sqlite backup report\n")
	fmt.Fprintf(&b, "status=%s schema=%d integrity=%s verification=%s mode=%s\n", report.Status, report.Migration.CurrentVersion, report.IntegrityCheck, report.VerificationStatus, report.VerificationMode)
	if report.SourceDatabase != "" {
		fmt.Fprintf(&b, "source=%s sha256=%s\n", report.SourceDatabase, short(report.SourceSHA256))
	}
	if report.BackupDatabase != "" {
		fmt.Fprintf(&b, "backup=%s sha256=%s\n", report.BackupDatabase, short(report.BackupSHA256))
	}
	fmt.Fprintf(&b, "counts commits=%d evidence=%d audit=%d idempotency=%d\n", report.CommitCount, report.EvidenceCount, report.AuditCount, report.IdempotencyCount)
	fmt.Fprintf(&b, "receipts verified=%d invalid=%d\n", report.VerifiedReceipts, report.InvalidReceipts)
	for _, check := range report.ReceiptChecks {
		fmt.Fprintf(&b, "receipt=%s valid=%t quorum=%s signers=%d height=%d\n", short(check.ReceiptID), check.Valid, check.QuorumStatus, check.SignerCount, check.CommitHeight)
	}
	fmt.Fprintf(&b, "message=%s\n", report.Message)
	fmt.Fprintf(&b, "limitations=%s\n", report.Limitations)
	return b.String()
}

func vacuumSQLiteInto(ctx context.Context, source, destination string) error {
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(FULL)`); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO '`+escapeSQLiteString(destination)+`'`)
	return err
}

func verifyReceipt(receipt evidencepkg.EvidenceReceipt, history consortium.History, manifestBacked bool) evidencepkg.VerificationResult {
	if !manifestBacked {
		return evidencepkg.VerifyReceipt(receipt, nil, nil, 0)
	}
	hash := firstNonEmpty(receipt.ValidatorSetHash, receipt.Commit.Block.ValidatorSetHash)
	version := receipt.MembershipVersion
	if version == 0 {
		version = receipt.Commit.Block.MembershipVersion
	}
	manifest, ok := history.ManifestByHash(hash)
	if !ok {
		return invalidReceipt(receipt, "unknown validator set hash "+hash)
	}
	if manifest.MembershipVersion != version {
		return invalidReceipt(receipt, fmt.Sprintf("validator set hash %s belongs to version %d, not %d", hash, manifest.MembershipVersion, version))
	}
	identities, err := manifest.ActiveIdentities()
	if err != nil {
		return invalidReceipt(receipt, err.Error())
	}
	verifier, err := verifierForManifest(manifest)
	if err != nil {
		return invalidReceipt(receipt, err.Error())
	}
	return evidencepkg.VerifyReceipt(receipt, identities, verifier, manifest.QuorumThreshold)
}

func invalidReceipt(receipt evidencepkg.EvidenceReceipt, reason string) evidencepkg.VerificationResult {
	return evidencepkg.VerificationResult{
		Valid:        false,
		Status:       evidencepkg.VerificationInvalid,
		Reason:       reason,
		ReceiptID:    receipt.ReceiptID,
		EvidenceID:   receipt.EvidenceID,
		QuorumStatus: "invalid",
		AnchorStatus: receipt.AnchorStatus,
		SignerCount:  receipt.SignerCount,
	}
}

func verifierForManifest(manifest consortium.Manifest) (pqcrypto.SignatureVerifier, error) {
	records := manifest.ActiveValidators()
	if len(records) == 0 {
		return nil, errors.New("manifest has no active validators")
	}
	switch records[0].SignatureAlgorithm {
	case dev.SignatureAlgorithm:
		return dev.SignatureVerifier{}, nil
	case mldsa.Algorithm:
		return mldsa.NewVerifier(), nil
	default:
		return nil, fmt.Errorf("unsupported receipt signature algorithm %q", records[0].SignatureAlgorithm)
	}
}

func loadManifestHistory(manifestPath, historyRaw string) (consortium.History, bool, error) {
	manifestPath = strings.TrimSpace(manifestPath)
	historyRaw = strings.TrimSpace(historyRaw)
	if manifestPath == "" && historyRaw == "" {
		return consortium.History{}, false, nil
	}
	history := consortium.History{}
	var err error
	if historyRaw != "" {
		history, err = consortium.LoadManifestHistory(historyRaw)
		if err != nil {
			return consortium.History{}, true, err
		}
	}
	if manifestPath != "" {
		manifest, err := consortium.LoadManifest(manifestPath)
		if err != nil {
			return consortium.History{}, true, err
		}
		history, err = history.WithManifest(manifest)
		if err != nil {
			return consortium.History{}, true, err
		}
	}
	return history, true, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func restoreWorkingCopy(path string) (string, func(), error) {
	if strings.HasPrefix(path, "file:") || strings.Contains(path, "?") {
		return path, func() {}, nil
	}
	src, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer src.Close()
	tempDir, err := os.MkdirTemp("", "pqfabric-restore-check-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	dstPath := filepath.Join(tempDir, filepath.Base(path))
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		cleanup()
		return "", nil, err
	}
	if err := dst.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return dstPath, cleanup, nil
}

func escapeSQLiteString(value string) string {
	return strings.ReplaceAll(value, `'`, `''`)
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func short(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
