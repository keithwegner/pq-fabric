package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryStoreCommitStateAndIdempotency(t *testing.T) {
	store := NewMemoryStore()
	state := testState(1, "hash-1")
	record := testCommitRecord(1, "hash-1")
	if err := store.SaveCommit(record, state); err != nil {
		t.Fatal(err)
	}
	loadedState, ok, err := store.LoadValidatorState()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loadedState.Height != 1 || loadedState.LastHash != "hash-1" {
		t.Fatalf("unexpected memory state: %+v ok=%t", loadedState, ok)
	}
	commits, err := store.ListCommits()
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].BlockHash != "hash-1" || len(commits[0].CertificateJSON) == 0 {
		t.Fatalf("unexpected memory commits: %+v", commits)
	}
	applied, existing, err := store.RecordIdempotency("tx-1", "result-1")
	if err != nil {
		t.Fatal(err)
	}
	if !applied || existing != "result-1" {
		t.Fatalf("expected first idempotency apply, got applied=%t existing=%s", applied, existing)
	}
	applied, existing, err = store.RecordIdempotency("tx-1", "result-2")
	if err != nil {
		t.Fatal(err)
	}
	if applied || existing != "result-1" {
		t.Fatalf("expected duplicate idempotency rejection, got applied=%t existing=%s", applied, existing)
	}
}

func TestFileStoreCreatesMissingDirectoryAndRecoversState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "validator-1")
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected data dir to be created: %v", err)
	}
	record := testCommitRecord(1, "hash-1")
	if err := store.SaveCommit(record, testState(1, "hash-1")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(SnapshotRecord{ID: "checkpoint-1", Height: 1, LastHash: "hash-1", SnapshotJSON: []byte(`{"height":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, ok, err := reopened.LoadValidatorState()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || state.Height != 1 || state.LastHash != "hash-1" {
		t.Fatalf("unexpected recovered state: %+v ok=%t", state, ok)
	}
	commit, ok, err := reopened.Commit(1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || commit.BlockHash != "hash-1" || string(commit.CertificateJSON) != `{"height":1}` {
		t.Fatalf("unexpected recovered commit: %+v ok=%t", commit, ok)
	}
	snapshots, err := reopened.ListSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].ID != "checkpoint-1" {
		t.Fatalf("unexpected recovered snapshots: %+v", snapshots)
	}
}

func TestFileStoreIdempotencySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	applied, existing, err := store.RecordIdempotency("tx-1", "result-1")
	if err != nil {
		t.Fatal(err)
	}
	if !applied || existing != "result-1" {
		t.Fatalf("expected first apply, got applied=%t existing=%s", applied, existing)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	applied, existing, err = reopened.RecordIdempotency("tx-1", "result-2")
	if err != nil {
		t.Fatal(err)
	}
	if applied || existing != "result-1" {
		t.Fatalf("expected duplicate after restart, got applied=%t existing=%s", applied, existing)
	}
	count, err := reopened.IdempotencyCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected idempotency count 1, got %d", count)
	}
}

func TestEvidenceRecordsIndexedByReceiptIdempotencyAndQCHash(t *testing.T) {
	store := NewMemoryStore()
	record := EvidenceRecord{
		EvidenceID:     "evidence-1",
		ReceiptID:      "receipt-1",
		EventHash:      "event-1",
		QCHash:         "qc-1",
		CommitHeight:   7,
		SubmittingOrg:  "bestgate",
		IdempotencyKey: "idem-1",
		ReceiptJSON:    []byte(`{"receipt_id":"receipt-1"}`),
	}
	created, existing, err := store.SaveEvidence(record)
	if err != nil {
		t.Fatal(err)
	}
	if !created || existing.ReceiptID != record.ReceiptID {
		t.Fatalf("expected created record, got created=%t existing=%+v", created, existing)
	}
	created, existing, err = store.SaveEvidence(record)
	if err != nil {
		t.Fatal(err)
	}
	if created || existing.EvidenceID != record.EvidenceID {
		t.Fatalf("expected duplicate save to return existing, got created=%t existing=%+v", created, existing)
	}
	for name, lookup := range map[string]func() (EvidenceRecord, bool, error){
		"evidence":    func() (EvidenceRecord, bool, error) { return store.EvidenceByID("evidence-1") },
		"receipt":     func() (EvidenceRecord, bool, error) { return store.EvidenceByReceiptID("receipt-1") },
		"idempotency": func() (EvidenceRecord, bool, error) { return store.EvidenceByIdempotencyKey("idem-1") },
		"qc":          func() (EvidenceRecord, bool, error) { return store.EvidenceByQCHash("qc-1") },
	} {
		got, ok, err := lookup()
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got.EvidenceID != record.EvidenceID {
			t.Fatalf("%s lookup failed: ok=%t record=%+v", name, ok, got)
		}
	}
}

func TestFileStoreEvidenceSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	record := EvidenceRecord{
		EvidenceID:     "evidence-1",
		ReceiptID:      "receipt-1",
		EventHash:      "event-1",
		QCHash:         "qc-1",
		CommitHeight:   7,
		SubmittingOrg:  "bestgate",
		IdempotencyKey: "idem-1",
		ReceiptJSON:    []byte(`{"receipt_id":"receipt-1"}`),
	}
	if _, _, err := store.SaveEvidence(record); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, ok, err := reopened.EvidenceByReceiptID("receipt-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.EvidenceID != record.EvidenceID || got.IdempotencyKey != record.IdempotencyKey {
		t.Fatalf("unexpected recovered evidence record: ok=%t record=%+v", ok, got)
	}
}

func TestSQLiteStoreCommitEvidenceIdempotencyAndAuditSurviveRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "validator.db")
	store, err := OpenSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	state := testState(1, "hash-1")
	record := testCommitRecord(1, "hash-1")
	if err := store.SaveCommit(record, state); err != nil {
		t.Fatal(err)
	}
	applied, existing, err := store.RecordIdempotency("tx-1", "result-1")
	if err != nil {
		t.Fatal(err)
	}
	if !applied || existing != "result-1" {
		t.Fatalf("expected first idempotency insert, got applied=%t existing=%s", applied, existing)
	}
	evidence := EvidenceRecord{
		EvidenceID:     "evidence-1",
		ReceiptID:      "receipt-1",
		EventHash:      "event-1",
		QCHash:         "qc-1",
		CommitHeight:   1,
		SubmittingOrg:  "bestgate",
		IdempotencyKey: "idem-1",
		ReceiptJSON:    []byte(`{"receipt_id":"receipt-1"}`),
	}
	if created, _, err := store.SaveEvidence(evidence); err != nil || !created {
		t.Fatalf("expected sqlite evidence create, created=%t err=%v", created, err)
	}
	if err := store.SaveAudit(AuditRecord{RequestID: "request-1", TimestampUnixMilli: 1, Method: "GET", Path: "/health", StatusCode: 200}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadValidatorState()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded.Height != 1 || loaded.LastHash != "hash-1" {
		t.Fatalf("unexpected sqlite state after restart: ok=%t state=%+v", ok, loaded)
	}
	commit, ok, err := reopened.Commit(1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || commit.BlockHash != "hash-1" {
		t.Fatalf("unexpected sqlite commit after restart: ok=%t commit=%+v", ok, commit)
	}
	applied, existing, err = reopened.RecordIdempotency("tx-1", "result-2")
	if err != nil {
		t.Fatal(err)
	}
	if applied || existing != "result-1" {
		t.Fatalf("expected sqlite duplicate idempotency to return original result, applied=%t existing=%s", applied, existing)
	}
	for name, lookup := range map[string]func() (EvidenceRecord, bool, error){
		"evidence":    func() (EvidenceRecord, bool, error) { return reopened.EvidenceByID("evidence-1") },
		"receipt":     func() (EvidenceRecord, bool, error) { return reopened.EvidenceByReceiptID("receipt-1") },
		"idempotency": func() (EvidenceRecord, bool, error) { return reopened.EvidenceByIdempotencyKey("idem-1") },
		"qc":          func() (EvidenceRecord, bool, error) { return reopened.EvidenceByQCHash("qc-1") },
	} {
		got, ok, err := lookup()
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got.EvidenceID != "evidence-1" {
			t.Fatalf("%s sqlite lookup failed: ok=%t record=%+v", name, ok, got)
		}
	}
	audit, err := reopened.ListAudit(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].RequestID != "request-1" {
		t.Fatalf("unexpected sqlite audit records: %+v", audit)
	}
}

func TestSQLiteMigrationStatusAndSchemaVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "validator.db")
	dryRun, err := CheckSQLiteMigrations(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.CurrentVersion != 0 || len(dryRun.PendingVersions) != 1 || dryRun.PendingVersions[0] != SQLiteSchemaVersion {
		t.Fatalf("unexpected dry-run migration report: %+v", dryRun)
	}
	applied, err := CheckSQLiteMigrations(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if applied.CurrentVersion != SQLiteSchemaVersion || len(applied.AppliedVersions) != 1 {
		t.Fatalf("expected schema migration to apply: %+v", applied)
	}
	current, err := CheckSQLiteMigrations(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentVersion != SQLiteSchemaVersion || len(current.PendingVersions) != 0 {
		t.Fatalf("expected schema to be current: %+v", current)
	}
}

func TestSQLiteMigrationRejectsFutureVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "validator.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at_unix_milli INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name, applied_at_unix_milli) VALUES (?, ?, ?)`, SQLiteSchemaVersion+1, "future", int64(1)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := CheckSQLiteMigrations(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "fail" {
		t.Fatalf("expected future schema version to fail: %+v", report)
	}
}

func TestSQLiteStoreSaveCommitIsAtomicForConflicts(t *testing.T) {
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "validator.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveCommit(testCommitRecord(1, "hash-1"), testState(1, "hash-1")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCommit(testCommitRecord(1, "hash-2"), testState(2, "hash-2")); err == nil {
		t.Fatal("expected conflicting sqlite commit to fail")
	}
	state, ok, err := store.LoadValidatorState()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || state.Height != 1 || state.LastHash != "hash-1" {
		t.Fatalf("conflicting commit should not update sqlite state, got ok=%t state=%+v", ok, state)
	}
}

func TestAuditRecordsAreRecentFirstAndSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := AuditRecord{RequestID: "request-1", TimestampUnixMilli: 1, PrincipalID: "reader", Method: "GET", Path: "/v1/evidence/example", StatusCode: 404}
	second := AuditRecord{RequestID: "request-2", TimestampUnixMilli: 2, PrincipalID: "admin", Method: "GET", Path: "/v1/audit/recent", StatusCode: 200}
	if err := store.SaveAudit(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAudit(second); err != nil {
		t.Fatal(err)
	}
	recent, err := store.ListAudit(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].RequestID != "request-2" {
		t.Fatalf("expected newest audit record first, got %+v", recent)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	all, err := reopened.ListAudit(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].RequestID != "request-2" || all[1].RequestID != "request-1" {
		t.Fatalf("unexpected reopened audit records: %+v", all)
	}
}

func TestRecentCommitsAndEvidenceAcrossBackends(t *testing.T) {
	for name, open := range map[string]func(t *testing.T) ValidatorStore{
		"memory": func(t *testing.T) ValidatorStore { return NewMemoryStore() },
		"file": func(t *testing.T) ValidatorStore {
			store, err := OpenFileStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
		"sqlite": func(t *testing.T) ValidatorStore {
			store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "validator.db"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := open(t)
			defer store.Close()
			for i := uint64(1); i <= 3; i++ {
				suffix := fmt.Sprintf("%d", i)
				record := testCommitRecord(i, "hash-"+suffix)
				record.CreatedAtUnixMilli = int64(i)
				if err := store.SaveCommit(record, testState(i, record.BlockHash)); err != nil {
					t.Fatal(err)
				}
				evidence := EvidenceRecord{
					EvidenceID:         "evidence-" + suffix,
					ReceiptID:          "receipt-" + suffix,
					EventHash:          "event-" + suffix,
					CommitHeight:       i,
					ReceiptJSON:        []byte(`{"receipt_id":"receipt"}`),
					CreatedAtUnixMilli: int64(i),
				}
				if _, _, err := store.SaveEvidence(evidence); err != nil {
					t.Fatal(err)
				}
			}
			commits, err := store.RecentCommits(2)
			if err != nil {
				t.Fatal(err)
			}
			if len(commits) != 2 || commits[0].Height != 3 || commits[1].Height != 2 {
				t.Fatalf("unexpected recent commits: %+v", commits)
			}
			evidence, err := store.RecentEvidence(2)
			if err != nil {
				t.Fatal(err)
			}
			if len(evidence) != 2 || evidence[0].EvidenceID != "evidence-3" || evidence[1].EvidenceID != "evidence-2" {
				t.Fatalf("unexpected recent evidence: %+v", evidence)
			}
		})
	}
}

func TestFileStoreRejectsCorruptedState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFileStore(dir); err == nil {
		t.Fatal("expected corrupted state to fail")
	}
}

func TestFileStoreRejectsCorruptedCommitLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, commitLogFile), []byte("{not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFileStore(dir); err == nil {
		t.Fatal("expected corrupted commit log to fail")
	}
}

func testState(height uint64, hash string) ValidatorState {
	return ValidatorState{
		NodeID:             "validator-1",
		Region:             "nyc",
		Height:             height,
		LastHash:           hash,
		CommitCount:        int(height),
		IdentityKeyID:      "key-1",
		SignatureAlgorithm: "sig",
		KEMAlgorithm:       "kem",
	}
}

func testCommitRecord(height uint64, hash string) CommitRecord {
	certificate, _ := json.Marshal(map[string]uint64{"height": height})
	return CommitRecord{
		Height:          height,
		BlockHash:       hash,
		CommitJSON:      []byte(`{"block":{"height":1},"certificate":{"height":1}}`),
		CertificateJSON: certificate,
		IdentityKeyID:   "key-1",
	}
}
