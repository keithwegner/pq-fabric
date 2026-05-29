package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	stateFile       = "validator_state.json"
	commitLogFile   = "commits.jsonl"
	idempotencyFile = "idempotency.jsonl"
	snapshotFile    = "snapshots.jsonl"
	evidenceFile    = "evidence.jsonl"
	auditFile       = "audit.jsonl"
)

type FileStore struct {
	mu          sync.Mutex
	dir         string
	state       ValidatorState
	hasState    bool
	commits     map[uint64]CommitRecord
	idempotency map[string]IdempotencyRecord
	snapshots   []SnapshotRecord
	evidence    map[string]EvidenceRecord
	receipts    map[string]string
	evidenceByQ map[string]string
	evidenceByI map[string]string
	audit       []AuditRecord
	closed      bool
}

func OpenFileStore(dataDir string) (*FileStore, error) {
	if dataDir == "" {
		return nil, errors.New("durable storage data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create durable storage data dir: %w", err)
	}
	store := &FileStore{
		dir:         dataDir,
		commits:     make(map[uint64]CommitRecord),
		idempotency: make(map[string]IdempotencyRecord),
		evidence:    make(map[string]EvidenceRecord),
		receipts:    make(map[string]string),
		evidenceByQ: make(map[string]string),
		evidenceByI: make(map[string]string),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) LoadValidatorState() (ValidatorState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return ValidatorState{}, false, err
	}
	return s.state, s.hasState, nil
}

func (s *FileStore) SaveValidatorState(state ValidatorState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return err
	}
	if err := s.writeStateLocked(state); err != nil {
		return err
	}
	s.state = state
	s.hasState = true
	return nil
}

func (s *FileStore) SaveCommit(record CommitRecord, state ValidatorState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return err
	}
	if existing, ok := s.commits[record.Height]; ok {
		if existing.BlockHash != record.BlockHash {
			return fmt.Errorf("conflicting durable commit at height %d", record.Height)
		}
		if err := s.writeStateLocked(state); err != nil {
			return err
		}
		s.state = state
		s.hasState = true
		return nil
	}
	if err := appendJSONLine(s.path(commitLogFile), record); err != nil {
		return err
	}
	s.commits[record.Height] = record
	if err := s.writeStateLocked(state); err != nil {
		return err
	}
	s.state = state
	s.hasState = true
	return nil
}

func (s *FileStore) ListCommits() ([]CommitRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return nil, err
	}
	return s.sortedCommitsLocked(), nil
}

func (s *FileStore) RecentCommits(limit int) ([]CommitRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return nil, err
	}
	return sortedCommitRecords(s.commits, false, limit), nil
}

func (s *FileStore) Commit(height uint64) (CommitRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return CommitRecord{}, false, err
	}
	record, ok := s.commits[height]
	return record, ok, nil
}

func (s *FileStore) RecordIdempotency(id, resultHash string) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return false, "", err
	}
	if existing, ok := s.idempotency[id]; ok {
		return false, existing.ResultHash, nil
	}
	record := IdempotencyRecord{ID: id, ResultHash: resultHash, AppliedAtUnixMilli: time.Now().UnixMilli()}
	if err := appendJSONLine(s.path(idempotencyFile), record); err != nil {
		return false, "", err
	}
	s.idempotency[id] = record
	return true, resultHash, nil
}

func (s *FileStore) IdempotencyResult(id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return "", false, err
	}
	record, ok := s.idempotency[id]
	return record.ResultHash, ok, nil
}

func (s *FileStore) IdempotencyCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return 0, err
	}
	return len(s.idempotency), nil
}

func (s *FileStore) SaveSnapshot(record SnapshotRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return err
	}
	if err := appendJSONLine(s.path(snapshotFile), record); err != nil {
		return err
	}
	s.snapshots = append(s.snapshots, record)
	return nil
}

func (s *FileStore) ListSnapshots() ([]SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return nil, err
	}
	return append([]SnapshotRecord(nil), s.snapshots...), nil
}

func (s *FileStore) SaveEvidence(record EvidenceRecord) (bool, EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return false, EvidenceRecord{}, err
	}
	if existing, ok := s.evidence[record.EvidenceID]; ok {
		if existing.ReceiptID != record.ReceiptID {
			return false, existing, fmt.Errorf("conflicting evidence record for %s", record.EvidenceID)
		}
		return false, existing, nil
	}
	if record.IdempotencyKey != "" {
		if evidenceID, ok := s.evidenceByI[record.IdempotencyKey]; ok {
			return false, s.evidence[evidenceID], nil
		}
	}
	if existingID, ok := s.receipts[record.ReceiptID]; ok && existingID != record.EvidenceID {
		return false, s.evidence[existingID], fmt.Errorf("conflicting receipt id %s", record.ReceiptID)
	}
	if err := appendJSONLine(s.path(evidenceFile), record); err != nil {
		return false, EvidenceRecord{}, err
	}
	s.indexEvidenceLocked(record)
	return true, record, nil
}

func (s *FileStore) EvidenceByID(evidenceID string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return EvidenceRecord{}, false, err
	}
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *FileStore) EvidenceByReceiptID(receiptID string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return EvidenceRecord{}, false, err
	}
	evidenceID, ok := s.receipts[receiptID]
	if !ok {
		return EvidenceRecord{}, false, nil
	}
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *FileStore) EvidenceByIdempotencyKey(idempotencyKey string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return EvidenceRecord{}, false, err
	}
	evidenceID, ok := s.evidenceByI[idempotencyKey]
	if !ok {
		return EvidenceRecord{}, false, nil
	}
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *FileStore) EvidenceByQCHash(qcHash string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return EvidenceRecord{}, false, err
	}
	evidenceID, ok := s.evidenceByQ[qcHash]
	if !ok {
		return EvidenceRecord{}, false, nil
	}
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *FileStore) ListEvidence() ([]EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(s.evidence))
	for id := range s.evidence {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]EvidenceRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.evidence[id])
	}
	return out, nil
}

func (s *FileStore) RecentEvidence(limit int) ([]EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return nil, err
	}
	records := make([]EvidenceRecord, 0, len(s.evidence))
	for _, record := range s.evidence {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAtUnixMilli == records[j].CreatedAtUnixMilli {
			return records[i].EvidenceID > records[j].EvidenceID
		}
		return records[i].CreatedAtUnixMilli > records[j].CreatedAtUnixMilli
	})
	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}
	return records, nil
}

func (s *FileStore) SaveAudit(record AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return err
	}
	if err := appendJSONLine(s.path(auditFile), record); err != nil {
		return err
	}
	s.audit = append(s.audit, record)
	return nil
}

func (s *FileStore) ListAudit(limit int) ([]AuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpen(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > len(s.audit) {
		limit = len(s.audit)
	}
	out := make([]AuditRecord, 0, limit)
	for i := len(s.audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.audit[i])
	}
	return out, nil
}

func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *FileStore) load() error {
	if err := s.loadState(); err != nil {
		return err
	}
	if err := loadJSONLines(s.path(commitLogFile), func(line int, record CommitRecord) error {
		if record.Height == 0 {
			return fmt.Errorf("commit line %d has zero height", line)
		}
		if record.BlockHash == "" {
			return fmt.Errorf("commit line %d missing block hash", line)
		}
		if len(record.CommitJSON) == 0 {
			return fmt.Errorf("commit line %d missing commit JSON", line)
		}
		if existing, ok := s.commits[record.Height]; ok && existing.BlockHash != record.BlockHash {
			return fmt.Errorf("conflicting commit records at height %d", record.Height)
		}
		s.commits[record.Height] = record
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONLines(s.path(idempotencyFile), func(line int, record IdempotencyRecord) error {
		if record.ID == "" {
			return fmt.Errorf("idempotency line %d missing id", line)
		}
		if record.ResultHash == "" {
			return fmt.Errorf("idempotency line %d missing result hash", line)
		}
		if existing, ok := s.idempotency[record.ID]; ok && existing.ResultHash != record.ResultHash {
			return fmt.Errorf("conflicting idempotency records for %s", record.ID)
		}
		s.idempotency[record.ID] = record
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONLines(s.path(snapshotFile), func(line int, record SnapshotRecord) error {
		if record.ID == "" {
			return fmt.Errorf("snapshot line %d missing id", line)
		}
		s.snapshots = append(s.snapshots, record)
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONLines(s.path(evidenceFile), func(line int, record EvidenceRecord) error {
		if record.EvidenceID == "" {
			return fmt.Errorf("evidence line %d missing evidence id", line)
		}
		if record.ReceiptID == "" {
			return fmt.Errorf("evidence line %d missing receipt id", line)
		}
		if len(record.ReceiptJSON) == 0 {
			return fmt.Errorf("evidence line %d missing receipt JSON", line)
		}
		if existing, ok := s.evidence[record.EvidenceID]; ok && existing.ReceiptID != record.ReceiptID {
			return fmt.Errorf("conflicting evidence records for %s", record.EvidenceID)
		}
		if existingID, ok := s.receipts[record.ReceiptID]; ok && existingID != record.EvidenceID {
			return fmt.Errorf("conflicting receipt id %s", record.ReceiptID)
		}
		if record.IdempotencyKey != "" {
			if existingID, ok := s.evidenceByI[record.IdempotencyKey]; ok && existingID != record.EvidenceID {
				return fmt.Errorf("conflicting idempotency key %s", record.IdempotencyKey)
			}
		}
		s.indexEvidenceLocked(record)
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONLines(s.path(auditFile), func(line int, record AuditRecord) error {
		if record.RequestID == "" {
			return fmt.Errorf("audit line %d missing request id", line)
		}
		if record.Method == "" || record.Path == "" {
			return fmt.Errorf("audit line %d missing method or path", line)
		}
		if record.StatusCode == 0 {
			return fmt.Errorf("audit line %d missing status code", line)
		}
		s.audit = append(s.audit, record)
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *FileStore) indexEvidenceLocked(record EvidenceRecord) {
	s.evidence[record.EvidenceID] = record
	s.receipts[record.ReceiptID] = record.EvidenceID
	if record.QCHash != "" {
		s.evidenceByQ[record.QCHash] = record.EvidenceID
	}
	if record.IdempotencyKey != "" {
		s.evidenceByI[record.IdempotencyKey] = record.EvidenceID
	}
}

func (s *FileStore) loadState() error {
	data, err := os.ReadFile(s.path(stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read durable validator state: %w", err)
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return fmt.Errorf("parse durable validator state: %w", err)
	}
	s.hasState = true
	return nil
}

func (s *FileStore) writeStateLocked(state ValidatorState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := atomicWriteFile(s.path(stateFile), data); err != nil {
		return err
	}
	return syncDir(s.dir)
}

func (s *FileStore) sortedCommitsLocked() []CommitRecord {
	heights := make([]int, 0, len(s.commits))
	for height := range s.commits {
		heights = append(heights, int(height))
	}
	sort.Ints(heights)
	out := make([]CommitRecord, 0, len(heights))
	for _, height := range heights {
		out = append(out, s.commits[uint64(height)])
	}
	return out
}

func (s *FileStore) requireOpen() error {
	if s.closed {
		return errors.New("durable store is closed")
	}
	return nil
}

func (s *FileStore) path(name string) string {
	return filepath.Join(s.dir, name)
}

func appendJSONLine(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func loadJSONLines[T any](path string, handle func(line int, record T) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var record T
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("parse %s line %d: %w", path, line, err)
		}
		if err := handle(line, record); err != nil {
			return fmt.Errorf("validate %s line %d: %w", path, line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
