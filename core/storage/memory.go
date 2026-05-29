package storage

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu          sync.Mutex
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
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		commits:     make(map[uint64]CommitRecord),
		idempotency: make(map[string]IdempotencyRecord),
		evidence:    make(map[string]EvidenceRecord),
		receipts:    make(map[string]string),
		evidenceByQ: make(map[string]string),
		evidenceByI: make(map[string]string),
	}
}

func (s *MemoryStore) LoadValidatorState() (ValidatorState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.hasState, nil
}

func (s *MemoryStore) SaveValidatorState(state ValidatorState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	s.hasState = true
	return nil
}

func (s *MemoryStore) SaveCommit(record CommitRecord, state ValidatorState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.commits[record.Height]; ok {
		if existing.BlockHash != record.BlockHash {
			return fmt.Errorf("conflicting commit at height %d", record.Height)
		}
		s.state = state
		s.hasState = true
		return nil
	}
	s.commits[record.Height] = record
	s.state = state
	s.hasState = true
	return nil
}

func (s *MemoryStore) ListCommits() ([]CommitRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedCommitRecords(s.commits, true, 0), nil
}

func (s *MemoryStore) RecentCommits(limit int) ([]CommitRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedCommitRecords(s.commits, false, limit), nil
}

func (s *MemoryStore) Commit(height uint64) (CommitRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.commits[height]
	return record, ok, nil
}

func (s *MemoryStore) RecordIdempotency(id, resultHash string) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.idempotency[id]; ok {
		return false, existing.ResultHash, nil
	}
	s.idempotency[id] = IdempotencyRecord{ID: id, ResultHash: resultHash, AppliedAtUnixMilli: time.Now().UnixMilli()}
	return true, resultHash, nil
}

func (s *MemoryStore) IdempotencyResult(id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.idempotency[id]
	return record.ResultHash, ok, nil
}

func (s *MemoryStore) IdempotencyCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.idempotency), nil
}

func (s *MemoryStore) SaveSnapshot(record SnapshotRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, record)
	return nil
}

func (s *MemoryStore) ListSnapshots() ([]SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SnapshotRecord(nil), s.snapshots...), nil
}

func (s *MemoryStore) SaveEvidence(record EvidenceRecord) (bool, EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.evidence[record.EvidenceID] = record
	s.receipts[record.ReceiptID] = record.EvidenceID
	if record.QCHash != "" {
		s.evidenceByQ[record.QCHash] = record.EvidenceID
	}
	if record.IdempotencyKey != "" {
		s.evidenceByI[record.IdempotencyKey] = record.EvidenceID
	}
	return true, record, nil
}

func (s *MemoryStore) EvidenceByID(evidenceID string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *MemoryStore) EvidenceByReceiptID(receiptID string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	evidenceID, ok := s.receipts[receiptID]
	if !ok {
		return EvidenceRecord{}, false, nil
	}
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *MemoryStore) EvidenceByIdempotencyKey(idempotencyKey string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	evidenceID, ok := s.evidenceByI[idempotencyKey]
	if !ok {
		return EvidenceRecord{}, false, nil
	}
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *MemoryStore) EvidenceByQCHash(qcHash string) (EvidenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	evidenceID, ok := s.evidenceByQ[qcHash]
	if !ok {
		return EvidenceRecord{}, false, nil
	}
	record, ok := s.evidence[evidenceID]
	return record, ok, nil
}

func (s *MemoryStore) ListEvidence() ([]EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *MemoryStore) RecentEvidence(limit int) ([]EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *MemoryStore) SaveAudit(record AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, record)
	return nil
}

func (s *MemoryStore) ListAudit(limit int) ([]AuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.audit) {
		limit = len(s.audit)
	}
	out := make([]AuditRecord, 0, limit)
	for i := len(s.audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.audit[i])
	}
	return out, nil
}

func (s *MemoryStore) Close() error {
	return nil
}

func sortedCommitRecords(commits map[uint64]CommitRecord, ascending bool, limit int) []CommitRecord {
	heights := make([]int, 0, len(commits))
	for height := range commits {
		heights = append(heights, int(height))
	}
	sort.Ints(heights)
	if !ascending {
		for i, j := 0, len(heights)-1; i < j; i, j = i+1, j-1 {
			heights[i], heights[j] = heights[j], heights[i]
		}
	}
	if limit > 0 && limit < len(heights) {
		heights = heights[:limit]
	}
	out := make([]CommitRecord, 0, len(heights))
	for _, height := range heights {
		out = append(out, commits[uint64(height)])
	}
	return out
}

// IdempotencyLedger tracks applied transaction/message identifiers so a retry
// after reconnect cannot apply the same logical operation twice.
type IdempotencyLedger struct {
	mu      sync.Mutex
	applied map[string]string
}

func NewIdempotencyLedger() *IdempotencyLedger {
	return &IdempotencyLedger{applied: make(map[string]string)}
}

// Apply returns true if the transaction id was new, false if it had already
// been applied. The stored value is a hash or other deterministic result marker.
func (l *IdempotencyLedger) Apply(id string, resultHash string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.applied[id]; ok {
		return false
	}
	l.applied[id] = resultHash
	return true
}

func (l *IdempotencyLedger) Result(id string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	result, ok := l.applied[id]
	return result, ok
}

func (l *IdempotencyLedger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.applied)
}
