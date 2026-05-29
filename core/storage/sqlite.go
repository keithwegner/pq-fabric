package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const SQLiteSchemaVersion = 1

type SQLiteStore struct {
	db *sql.DB
}

type SQLiteMigrationReport struct {
	Status               string `json:"status"`
	DatabaseURL          string `json:"database_url,omitempty"`
	CurrentVersion       int    `json:"current_version"`
	TargetVersion        int    `json:"target_version"`
	DryRun               bool   `json:"dry_run"`
	PendingVersions      []int  `json:"pending_versions,omitempty"`
	AppliedVersions      []int  `json:"applied_versions,omitempty"`
	GeneratedAtUnixMilli int64  `json:"generated_at_unix_milli"`
	Message              string `json:"message"`
}

func OpenSQLiteStore(dsn string) (*SQLiteStore, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("sqlite database URL is required")
	}
	if err := ensureSQLiteDir(dsn); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	store := &SQLiteStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) init() error {
	_, err := migrateSQLite(s.db, false)
	return err
}

func CheckSQLiteMigrations(dsn string, dryRun bool) (SQLiteMigrationReport, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return SQLiteMigrationReport{}, errors.New("sqlite database URL is required")
	}
	report := SQLiteMigrationReport{
		Status:               "pass",
		DatabaseURL:          dsn,
		TargetVersion:        SQLiteSchemaVersion,
		DryRun:               dryRun,
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
	}
	if dryRun && simpleSQLitePath(dsn) && !fileExists(dsn) {
		report.CurrentVersion = 0
		report.PendingVersions = []int{SQLiteSchemaVersion}
		report.Message = "database does not exist; schema version 1 would be created"
		return report, nil
	}
	if !dryRun {
		if err := ensureSQLiteDir(dsn); err != nil {
			return SQLiteMigrationReport{}, err
		}
	}
	openDSN := dsn
	if dryRun && simpleSQLitePath(dsn) {
		openDSN = "file:" + filepath.ToSlash(dsn) + "?mode=ro"
	}
	db, err := sql.Open("sqlite", openDSN)
	if err != nil {
		return SQLiteMigrationReport{}, err
	}
	defer db.Close()
	if dryRun {
		current, err := sqliteCurrentVersion(db)
		if err != nil {
			return SQLiteMigrationReport{}, err
		}
		report.CurrentVersion = current
		if current > SQLiteSchemaVersion {
			report.Status = "fail"
			report.Message = fmt.Sprintf("database schema version %d is newer than supported version %d", current, SQLiteSchemaVersion)
			return report, nil
		}
		if current < SQLiteSchemaVersion {
			report.PendingVersions = []int{SQLiteSchemaVersion}
			report.Message = "pending sqlite schema migration"
		} else {
			report.Message = "sqlite schema is current"
		}
		return report, nil
	}
	applied, err := migrateSQLite(db, false)
	if err != nil {
		return SQLiteMigrationReport{}, err
	}
	current, err := sqliteCurrentVersion(db)
	if err != nil {
		return SQLiteMigrationReport{}, err
	}
	report.CurrentVersion = current
	report.AppliedVersions = applied
	if len(applied) == 0 {
		report.Message = "sqlite schema is current"
	} else {
		report.Message = "sqlite schema migrations applied"
	}
	return report, nil
}

func SQLiteIntegrityCheck(dsn string) (string, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&result); err != nil {
		return "", err
	}
	if result != "ok" {
		return result, fmt.Errorf("sqlite integrity check failed: %s", result)
	}
	return result, nil
}

func migrateSQLite(db *sql.DB, dryRun bool) ([]int, error) {
	if dryRun {
		current, err := sqliteCurrentVersion(db)
		if err != nil {
			return nil, err
		}
		if current > SQLiteSchemaVersion {
			return nil, fmt.Errorf("sqlite schema version %d is newer than supported version %d", current, SQLiteSchemaVersion)
		}
		if current < SQLiteSchemaVersion {
			return []int{SQLiteSchemaVersion}, nil
		}
		return nil, nil
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return nil, err
	}
	current, err := sqliteCurrentVersion(db)
	if err != nil {
		return nil, err
	}
	if current > SQLiteSchemaVersion {
		return nil, fmt.Errorf("sqlite schema version %d is newer than supported version %d", current, SQLiteSchemaVersion)
	}
	if current >= SQLiteSchemaVersion {
		return nil, nil
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessCommitted(tx)
	for _, statement := range sqliteV1Statements {
		if _, err := tx.Exec(statement); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name, applied_at_unix_milli) VALUES (?, ?, ?) ON CONFLICT(version) DO NOTHING`, SQLiteSchemaVersion, "initial evidence fabric sqlite schema", time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	return []int{SQLiteSchemaVersion}, tx.Commit()
}

func sqliteCurrentVersion(db *sql.DB) (int, error) {
	var exists int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, nil
	}
	var version sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, err
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

func ensureSQLiteDir(dsn string) error {
	if !simpleSQLitePath(dsn) {
		return nil
	}
	if dir := filepath.Dir(dsn); dir != "." && dir != "" {
		return os.MkdirAll(dir, 0o755)
	}
	return nil
}

func simpleSQLitePath(dsn string) bool {
	return !strings.HasPrefix(dsn, "file:") && !strings.Contains(dsn, "?")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var sqliteV1Statements = []string{
	`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at_unix_milli INTEGER NOT NULL
		)`,
	`CREATE TABLE IF NOT EXISTS validator_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			node_id TEXT,
			region TEXT,
			height INTEGER NOT NULL,
			round INTEGER NOT NULL,
			last_hash TEXT,
			state_digest TEXT,
			locked_height INTEGER,
			locked_round INTEGER,
			locked_block_hash TEXT,
			commit_count INTEGER NOT NULL,
			identity_key_id TEXT,
			signature_algorithm TEXT,
			kem_algorithm TEXT,
			updated_at_unix_milli INTEGER NOT NULL
		)`,
	`CREATE TABLE IF NOT EXISTS commits (
			height INTEGER PRIMARY KEY,
			round INTEGER NOT NULL,
			block_hash TEXT NOT NULL,
			state_digest TEXT,
			commit_json BLOB NOT NULL,
			certificate_json BLOB,
			identity_key_id TEXT,
			created_at_unix_milli INTEGER NOT NULL
		)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS commits_block_hash_idx ON commits(block_hash)`,
	`CREATE TABLE IF NOT EXISTS idempotency (
			id TEXT PRIMARY KEY,
			result_hash TEXT NOT NULL,
			applied_at_unix_milli INTEGER NOT NULL
		)`,
	`CREATE TABLE IF NOT EXISTS snapshots (
			id TEXT PRIMARY KEY,
			height INTEGER NOT NULL,
			last_hash TEXT,
			snapshot_json BLOB NOT NULL,
			created_at_unix_milli INTEGER NOT NULL
		)`,
	`CREATE TABLE IF NOT EXISTS evidence (
			evidence_id TEXT PRIMARY KEY,
			receipt_id TEXT NOT NULL UNIQUE,
			event_hash TEXT NOT NULL,
			qc_hash TEXT,
			commit_height INTEGER NOT NULL,
			submitting_org TEXT,
			idempotency_key TEXT,
			receipt_json BLOB NOT NULL,
			created_at_unix_milli INTEGER NOT NULL
		)`,
	`CREATE INDEX IF NOT EXISTS evidence_event_hash_idx ON evidence(event_hash)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS evidence_qc_hash_idx ON evidence(qc_hash) WHERE qc_hash <> ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS evidence_idempotency_key_idx ON evidence(idempotency_key) WHERE idempotency_key <> ''`,
	`CREATE INDEX IF NOT EXISTS evidence_submitter_idx ON evidence(submitting_org)`,
	`CREATE INDEX IF NOT EXISTS evidence_commit_height_idx ON evidence(commit_height)`,
	`CREATE TABLE IF NOT EXISTS audit (
			row_id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			timestamp_unix_milli INTEGER NOT NULL,
			principal_id TEXT,
			organization TEXT,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			duration_millis INTEGER,
			client_addr TEXT,
			denied_reason TEXT
		)`,
	`CREATE INDEX IF NOT EXISTS audit_recent_idx ON audit(timestamp_unix_milli DESC, row_id DESC)`,
}

func (s *SQLiteStore) LoadValidatorState() (ValidatorState, bool, error) {
	row := s.db.QueryRow(`SELECT node_id, region, height, round, last_hash, state_digest, locked_height, locked_round, locked_block_hash, commit_count, identity_key_id, signature_algorithm, kem_algorithm, updated_at_unix_milli FROM validator_state WHERE id = 1`)
	state, err := scanValidatorState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ValidatorState{}, false, nil
	}
	if err != nil {
		return ValidatorState{}, false, err
	}
	return state, true, nil
}

func (s *SQLiteStore) SaveValidatorState(state ValidatorState) error {
	_, err := s.db.Exec(`INSERT INTO validator_state (id, node_id, region, height, round, last_hash, state_digest, locked_height, locked_round, locked_block_hash, commit_count, identity_key_id, signature_algorithm, kem_algorithm, updated_at_unix_milli)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET node_id=excluded.node_id, region=excluded.region, height=excluded.height, round=excluded.round, last_hash=excluded.last_hash, state_digest=excluded.state_digest, locked_height=excluded.locked_height, locked_round=excluded.locked_round, locked_block_hash=excluded.locked_block_hash, commit_count=excluded.commit_count, identity_key_id=excluded.identity_key_id, signature_algorithm=excluded.signature_algorithm, kem_algorithm=excluded.kem_algorithm, updated_at_unix_milli=excluded.updated_at_unix_milli`,
		state.NodeID, state.Region, state.Height, state.Round, state.LastHash, state.StateDigest, state.LockedHeight, state.LockedRound, state.LockedBlockHash, state.CommitCount, state.IdentityKeyID, state.SignatureAlgorithm, state.KEMAlgorithm, state.UpdatedAtUnixMilli)
	return err
}

func (s *SQLiteStore) SaveCommit(record CommitRecord, state ValidatorState) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)
	existing, ok, err := sqliteCommitTx(tx, record.Height)
	if err != nil {
		return err
	}
	if ok {
		if existing.BlockHash != record.BlockHash {
			return fmt.Errorf("conflicting sqlite commit at height %d", record.Height)
		}
		if err := sqliteSaveStateTx(tx, state); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO commits (height, round, block_hash, state_digest, commit_json, certificate_json, identity_key_id, created_at_unix_milli) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Height, record.Round, record.BlockHash, record.StateDigest, record.CommitJSON, record.CertificateJSON, record.IdentityKeyID, record.CreatedAtUnixMilli); err != nil {
		return err
	}
	if err := sqliteSaveStateTx(tx, state); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListCommits() ([]CommitRecord, error) {
	rows, err := s.db.Query(`SELECT height, round, block_hash, state_digest, commit_json, certificate_json, identity_key_id, created_at_unix_milli FROM commits ORDER BY height ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommitRecord
	for rows.Next() {
		record, err := scanCommit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) RecentCommits(limit int) ([]CommitRecord, error) {
	query := `SELECT height, round, block_hash, state_digest, commit_json, certificate_json, identity_key_id, created_at_unix_milli FROM commits ORDER BY height DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.Query(query+` LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommitRecord
	for rows.Next() {
		record, err := scanCommit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Commit(height uint64) (CommitRecord, bool, error) {
	return sqliteCommitQuery(s.db, height)
}

func (s *SQLiteStore) RecordIdempotency(id, resultHash string) (bool, string, error) {
	record := IdempotencyRecord{ID: id, ResultHash: resultHash, AppliedAtUnixMilli: time.Now().UnixMilli()}
	result, err := s.db.Exec(`INSERT OR IGNORE INTO idempotency (id, result_hash, applied_at_unix_milli) VALUES (?, ?, ?)`, record.ID, record.ResultHash, record.AppliedAtUnixMilli)
	if err != nil {
		return false, "", err
	}
	rows, _ := result.RowsAffected()
	if rows == 1 {
		return true, resultHash, nil
	}
	existing, ok, err := s.IdempotencyResult(id)
	return false, existing, errOrNotFound(ok, err)
}

func (s *SQLiteStore) IdempotencyResult(id string) (string, bool, error) {
	var resultHash string
	err := s.db.QueryRow(`SELECT result_hash FROM idempotency WHERE id = ?`, id).Scan(&resultHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return resultHash, err == nil, err
}

func (s *SQLiteStore) IdempotencyCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM idempotency`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) SaveSnapshot(record SnapshotRecord) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO snapshots (id, height, last_hash, snapshot_json, created_at_unix_milli) VALUES (?, ?, ?, ?, ?)`,
		record.ID, record.Height, record.LastHash, record.SnapshotJSON, record.CreatedAtUnixMilli)
	return err
}

func (s *SQLiteStore) ListSnapshots() ([]SnapshotRecord, error) {
	rows, err := s.db.Query(`SELECT id, height, last_hash, snapshot_json, created_at_unix_milli FROM snapshots ORDER BY created_at_unix_milli ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SnapshotRecord
	for rows.Next() {
		var record SnapshotRecord
		if err := rows.Scan(&record.ID, &record.Height, &record.LastHash, &record.SnapshotJSON, &record.CreatedAtUnixMilli); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SaveEvidence(record EvidenceRecord) (bool, EvidenceRecord, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, EvidenceRecord{}, err
	}
	defer rollbackUnlessCommitted(tx)
	if existing, ok, err := sqliteEvidenceByColumnTx(tx, "evidence_id", record.EvidenceID); err != nil {
		return false, EvidenceRecord{}, err
	} else if ok {
		if existing.ReceiptID != record.ReceiptID {
			return false, existing, fmt.Errorf("conflicting evidence record for %s", record.EvidenceID)
		}
		return false, existing, tx.Commit()
	}
	if record.IdempotencyKey != "" {
		if existing, ok, err := sqliteEvidenceByColumnTx(tx, "idempotency_key", record.IdempotencyKey); err != nil {
			return false, EvidenceRecord{}, err
		} else if ok {
			return false, existing, tx.Commit()
		}
	}
	if existing, ok, err := sqliteEvidenceByColumnTx(tx, "receipt_id", record.ReceiptID); err != nil {
		return false, EvidenceRecord{}, err
	} else if ok && existing.EvidenceID != record.EvidenceID {
		return false, existing, fmt.Errorf("conflicting receipt id %s", record.ReceiptID)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO evidence (evidence_id, receipt_id, event_hash, qc_hash, commit_height, submitting_org, idempotency_key, receipt_json, created_at_unix_milli) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.EvidenceID, record.ReceiptID, record.EventHash, record.QCHash, record.CommitHeight, record.SubmittingOrg, record.IdempotencyKey, record.ReceiptJSON, record.CreatedAtUnixMilli); err != nil {
		return false, EvidenceRecord{}, err
	}
	return true, record, tx.Commit()
}

func (s *SQLiteStore) EvidenceByID(evidenceID string) (EvidenceRecord, bool, error) {
	return sqliteEvidenceByColumn(s.db, "evidence_id", evidenceID)
}

func (s *SQLiteStore) EvidenceByReceiptID(receiptID string) (EvidenceRecord, bool, error) {
	return sqliteEvidenceByColumn(s.db, "receipt_id", receiptID)
}

func (s *SQLiteStore) EvidenceByIdempotencyKey(idempotencyKey string) (EvidenceRecord, bool, error) {
	return sqliteEvidenceByColumn(s.db, "idempotency_key", idempotencyKey)
}

func (s *SQLiteStore) EvidenceByQCHash(qcHash string) (EvidenceRecord, bool, error) {
	return sqliteEvidenceByColumn(s.db, "qc_hash", qcHash)
}

func (s *SQLiteStore) ListEvidence() ([]EvidenceRecord, error) {
	rows, err := s.db.Query(`SELECT evidence_id, receipt_id, event_hash, qc_hash, commit_height, submitting_org, idempotency_key, receipt_json, created_at_unix_milli FROM evidence ORDER BY evidence_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvidenceRows(rows)
}

func (s *SQLiteStore) RecentEvidence(limit int) ([]EvidenceRecord, error) {
	query := `SELECT evidence_id, receipt_id, event_hash, qc_hash, commit_height, submitting_org, idempotency_key, receipt_json, created_at_unix_milli FROM evidence ORDER BY created_at_unix_milli DESC, evidence_id DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.Query(query+` LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvidenceRows(rows)
}

func (s *SQLiteStore) SaveAudit(record AuditRecord) error {
	_, err := s.db.Exec(`INSERT INTO audit (request_id, timestamp_unix_milli, principal_id, organization, method, path, status_code, duration_millis, client_addr, denied_reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RequestID, record.TimestampUnixMilli, record.PrincipalID, record.Organization, record.Method, record.Path, record.StatusCode, record.DurationMillis, record.ClientAddr, record.DeniedReason)
	return err
}

func (s *SQLiteStore) ListAudit(limit int) ([]AuditRecord, error) {
	query := `SELECT request_id, timestamp_unix_milli, principal_id, organization, method, path, status_code, duration_millis, client_addr, denied_reason FROM audit ORDER BY timestamp_unix_milli DESC, row_id DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.Query(query+` LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRecord
	for rows.Next() {
		var record AuditRecord
		if err := rows.Scan(&record.RequestID, &record.TimestampUnixMilli, &record.PrincipalID, &record.Organization, &record.Method, &record.Path, &record.StatusCode, &record.DurationMillis, &record.ClientAddr, &record.DeniedReason); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func sqliteSaveStateTx(tx *sql.Tx, state ValidatorState) error {
	_, err := tx.Exec(`INSERT INTO validator_state (id, node_id, region, height, round, last_hash, state_digest, locked_height, locked_round, locked_block_hash, commit_count, identity_key_id, signature_algorithm, kem_algorithm, updated_at_unix_milli)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET node_id=excluded.node_id, region=excluded.region, height=excluded.height, round=excluded.round, last_hash=excluded.last_hash, state_digest=excluded.state_digest, locked_height=excluded.locked_height, locked_round=excluded.locked_round, locked_block_hash=excluded.locked_block_hash, commit_count=excluded.commit_count, identity_key_id=excluded.identity_key_id, signature_algorithm=excluded.signature_algorithm, kem_algorithm=excluded.kem_algorithm, updated_at_unix_milli=excluded.updated_at_unix_milli`,
		state.NodeID, state.Region, state.Height, state.Round, state.LastHash, state.StateDigest, state.LockedHeight, state.LockedRound, state.LockedBlockHash, state.CommitCount, state.IdentityKeyID, state.SignatureAlgorithm, state.KEMAlgorithm, state.UpdatedAtUnixMilli)
	return err
}

func sqliteCommitTx(tx *sql.Tx, height uint64) (CommitRecord, bool, error) {
	row := tx.QueryRow(`SELECT height, round, block_hash, state_digest, commit_json, certificate_json, identity_key_id, created_at_unix_milli FROM commits WHERE height = ?`, height)
	record, err := scanCommit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CommitRecord{}, false, nil
	}
	return record, err == nil, err
}

func sqliteCommitQuery(db *sql.DB, height uint64) (CommitRecord, bool, error) {
	row := db.QueryRow(`SELECT height, round, block_hash, state_digest, commit_json, certificate_json, identity_key_id, created_at_unix_milli FROM commits WHERE height = ?`, height)
	record, err := scanCommit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CommitRecord{}, false, nil
	}
	return record, err == nil, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanValidatorState(row scanner) (ValidatorState, error) {
	var state ValidatorState
	err := row.Scan(&state.NodeID, &state.Region, &state.Height, &state.Round, &state.LastHash, &state.StateDigest, &state.LockedHeight, &state.LockedRound, &state.LockedBlockHash, &state.CommitCount, &state.IdentityKeyID, &state.SignatureAlgorithm, &state.KEMAlgorithm, &state.UpdatedAtUnixMilli)
	return state, err
}

func scanCommit(row scanner) (CommitRecord, error) {
	var record CommitRecord
	err := row.Scan(&record.Height, &record.Round, &record.BlockHash, &record.StateDigest, &record.CommitJSON, &record.CertificateJSON, &record.IdentityKeyID, &record.CreatedAtUnixMilli)
	return record, err
}

func sqliteEvidenceByColumn(db *sql.DB, column, value string) (EvidenceRecord, bool, error) {
	row := db.QueryRow(`SELECT evidence_id, receipt_id, event_hash, qc_hash, commit_height, submitting_org, idempotency_key, receipt_json, created_at_unix_milli FROM evidence WHERE `+column+` = ?`, value)
	record, err := scanEvidence(row)
	if errors.Is(err, sql.ErrNoRows) {
		return EvidenceRecord{}, false, nil
	}
	return record, err == nil, err
}

func sqliteEvidenceByColumnTx(tx *sql.Tx, column, value string) (EvidenceRecord, bool, error) {
	row := tx.QueryRow(`SELECT evidence_id, receipt_id, event_hash, qc_hash, commit_height, submitting_org, idempotency_key, receipt_json, created_at_unix_milli FROM evidence WHERE `+column+` = ?`, value)
	record, err := scanEvidence(row)
	if errors.Is(err, sql.ErrNoRows) {
		return EvidenceRecord{}, false, nil
	}
	return record, err == nil, err
}

func scanEvidence(row scanner) (EvidenceRecord, error) {
	var record EvidenceRecord
	err := row.Scan(&record.EvidenceID, &record.ReceiptID, &record.EventHash, &record.QCHash, &record.CommitHeight, &record.SubmittingOrg, &record.IdempotencyKey, &record.ReceiptJSON, &record.CreatedAtUnixMilli)
	return record, err
}

func scanEvidenceRows(rows *sql.Rows) ([]EvidenceRecord, error) {
	var out []EvidenceRecord
	for rows.Next() {
		record, err := scanEvidence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func rollbackUnlessCommitted(tx *sql.Tx) {
	_ = tx.Rollback()
}

func errOrNotFound(ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
