package retransmit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

type Transaction struct {
	ID       string `json:"id"`
	Sequence uint64 `json:"sequence"`
	Payload  string `json:"payload"`
}

type Result struct {
	TransactionID string `json:"transaction_id"`
	Sequence      uint64 `json:"sequence"`
	Applied       bool   `json:"applied"`
	ResultHash    string `json:"result_hash"`
	Message       string `json:"message"`
}

type IdempotencyStore interface {
	RecordIdempotency(id, resultHash string) (applied bool, existingResultHash string, err error)
	IdempotencyCount() (int, error)
}

type Ledger struct {
	mu      sync.Mutex
	applied map[string]Result
	store   IdempotencyStore
}

func NewLedger() *Ledger {
	return &Ledger{applied: make(map[string]Result)}
}

func NewPersistentLedger(store IdempotencyStore) *Ledger {
	return &Ledger{applied: make(map[string]Result), store: store}
}

func (l *Ledger) Apply(tx Transaction) Result {
	resultHash := tx.Hash()
	if l.store != nil {
		applied, existingHash, err := l.store.RecordIdempotency(tx.ID, resultHash)
		if err != nil {
			return Result{TransactionID: tx.ID, Sequence: tx.Sequence, Applied: false, ResultHash: resultHash, Message: "idempotency store error: " + err.Error()}
		}
		if !applied {
			return Result{TransactionID: tx.ID, Sequence: tx.Sequence, Applied: false, ResultHash: existingHash, Message: "duplicate retransmission deduplicated by transaction id"}
		}
		return Result{TransactionID: tx.ID, Sequence: tx.Sequence, Applied: true, ResultHash: resultHash, Message: "transaction applied exactly once"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.applied[tx.ID]; ok {
		existing.Applied = false
		existing.Message = "duplicate retransmission deduplicated by transaction id"
		return existing
	}
	result := Result{
		TransactionID: tx.ID,
		Sequence:      tx.Sequence,
		Applied:       true,
		ResultHash:    resultHash,
		Message:       "transaction applied exactly once",
	}
	l.applied[tx.ID] = result
	return result
}

func (l *Ledger) Count() int {
	if l.store != nil {
		count, err := l.store.IdempotencyCount()
		if err != nil {
			return 0
		}
		return count
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.applied)
}

func (tx Transaction) Hash() string {
	b, err := json.Marshal(tx)
	if err != nil {
		panic(fmt.Sprintf("transaction hash marshal failed: %v", err))
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
