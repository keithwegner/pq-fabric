package state

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/keithwegner/pq-fabric/core/messages"
)

type Transaction struct {
	ID      string `json:"id"`
	Payload string `json:"payload,omitempty"`
}

type transitionRecord struct {
	PreviousDigest string        `json:"previous_digest"`
	Applied        []Transaction `json:"applied"`
}

type Machine struct {
	digest  string
	applied map[string]struct{}
}

func NewMachine() *Machine {
	return &Machine{digest: GenesisDigest(), applied: make(map[string]struct{})}
}

func GenesisDigest() string {
	return messages.HashBytes([]byte("pq-fabric/consensus/state/genesis/v1"))
}

func TransactionID(payload string) string {
	return messages.HashBytes([]byte("pq-fabric/consensus/state/tx/v1\x00" + payload))
}

func TransactionsFromPayload(payload string) []Transaction {
	return []Transaction{{ID: TransactionID(payload), Payload: payload}}
}

func NormalizeTransactions(payload string, txs []Transaction) []Transaction {
	if len(txs) == 0 {
		return TransactionsFromPayload(payload)
	}
	out := make([]Transaction, 0, len(txs))
	for _, tx := range txs {
		normalized := tx
		if strings.TrimSpace(normalized.ID) == "" && normalized.Payload != "" {
			normalized.ID = TransactionID(normalized.Payload)
		}
		out = append(out, normalized)
	}
	return out
}

func (m *Machine) Digest() string {
	return m.digest
}

func (m *Machine) Preview(txs []Transaction) (string, int, error) {
	digest, newlyApplied, err := transitionDigest(m.digest, m.applied, txs)
	return digest, len(newlyApplied), err
}

func (m *Machine) Apply(txs []Transaction) (string, int, error) {
	digest, newlyApplied, err := transitionDigest(m.digest, m.applied, txs)
	if err != nil {
		return "", 0, err
	}
	for _, tx := range newlyApplied {
		m.applied[tx.ID] = struct{}{}
	}
	m.digest = digest
	return m.digest, len(newlyApplied), nil
}

func (m *Machine) Clone() *Machine {
	applied := make(map[string]struct{}, len(m.applied))
	for id := range m.applied {
		applied[id] = struct{}{}
	}
	return &Machine{digest: m.digest, applied: applied}
}

func transitionDigest(previousDigest string, alreadyApplied map[string]struct{}, txs []Transaction) (string, []Transaction, error) {
	if previousDigest == "" {
		return "", nil, errors.New("previous state digest is required")
	}
	applied := make([]Transaction, 0, len(txs))
	seenInBlock := make(map[string]struct{})
	for _, raw := range txs {
		tx := raw
		if strings.TrimSpace(tx.ID) == "" && tx.Payload != "" {
			tx.ID = TransactionID(tx.Payload)
		}
		if strings.TrimSpace(tx.ID) == "" {
			return "", nil, errors.New("transaction id is required")
		}
		if _, ok := alreadyApplied[tx.ID]; ok {
			continue
		}
		if _, ok := seenInBlock[tx.ID]; ok {
			continue
		}
		seenInBlock[tx.ID] = struct{}{}
		applied = append(applied, tx)
	}
	if len(applied) == 0 {
		return previousDigest, nil, nil
	}
	record := transitionRecord{PreviousDigest: previousDigest, Applied: applied}
	digest, err := messages.HashCanonical(record)
	if err != nil {
		return "", nil, fmt.Errorf("hash state transition: %w", err)
	}
	return digest, applied, nil
}

func SortedAppliedIDs(txs []Transaction) []string {
	ids := make([]string, 0, len(txs))
	for _, tx := range txs {
		if tx.ID != "" {
			ids = append(ids, tx.ID)
		}
	}
	sort.Strings(ids)
	return ids
}
