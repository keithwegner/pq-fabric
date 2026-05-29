package retransmit

import (
	"fmt"
	"sort"

	bundleprotocol "github.com/keithwegner/pq-fabric/bundle/protocol"
)

type SubmitResult struct {
	BundleID         string `json:"bundle_id"`
	TransactionID    string `json:"transaction_id"`
	Sequence         uint64 `json:"sequence"`
	Applied          bool   `json:"applied"`
	Duplicate        bool   `json:"duplicate"`
	PendingMissing   bool   `json:"pending_missing"`
	ExpectedSequence uint64 `json:"expected_sequence"`
	ResultHash       string `json:"result_hash"`
	Message          string `json:"message"`
}

type Queue struct {
	ledger      *Ledger
	nextSeq     map[string]uint64
	gapPending  map[string]bundleprotocol.Envelope
	unconfirmed map[string]bundleprotocol.Envelope
}

func NewQueue(ledger *Ledger) *Queue {
	if ledger == nil {
		ledger = NewLedger()
	}
	return &Queue{
		ledger:      ledger,
		nextSeq:     make(map[string]uint64),
		gapPending:  make(map[string]bundleprotocol.Envelope),
		unconfirmed: make(map[string]bundleprotocol.Envelope),
	}
}

func (q *Queue) Submit(env bundleprotocol.Envelope, nowTick uint64) SubmitResult {
	if err := env.Validate(nowTick); err != nil {
		return SubmitResult{BundleID: env.BundleID, TransactionID: env.TransactionID, Sequence: env.SequenceNumber, Message: err.Error()}
	}
	key := streamKey(env)
	expected := q.expected(key)
	if env.SequenceNumber > expected {
		q.gapPending[env.BundleID] = env
		return SubmitResult{BundleID: env.BundleID, TransactionID: env.TransactionID, Sequence: env.SequenceNumber, PendingMissing: true, ExpectedSequence: expected, Message: "missing earlier sequence; bundle held for retry"}
	}
	tx := Transaction{ID: env.TransactionID, Sequence: env.SequenceNumber, Payload: string(env.PayloadBytes)}
	applied := q.ledger.Apply(tx)
	result := SubmitResult{
		BundleID:         env.BundleID,
		TransactionID:    env.TransactionID,
		Sequence:         env.SequenceNumber,
		Applied:          applied.Applied,
		Duplicate:        !applied.Applied,
		ExpectedSequence: expected,
		ResultHash:       applied.ResultHash,
		Message:          applied.Message,
	}
	if env.SequenceNumber == expected && applied.Applied {
		q.nextSeq[key] = expected + 1
		q.unconfirmed[env.BundleID] = env
		q.drainReady(key, nowTick)
	}
	return result
}

func (q *Queue) Confirm(bundleID string) bool {
	if _, ok := q.unconfirmed[bundleID]; !ok {
		return false
	}
	delete(q.unconfirmed, bundleID)
	return true
}

func (q *Queue) PendingForRetransmit() []bundleprotocol.Envelope {
	out := make([]bundleprotocol.Envelope, 0, len(q.unconfirmed)+len(q.gapPending))
	for _, env := range q.unconfirmed {
		out = append(out, env)
	}
	for _, env := range q.gapPending {
		out = append(out, env)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceNodeID != out[j].SourceNodeID {
			return out[i].SourceNodeID < out[j].SourceNodeID
		}
		if out[i].DestinationNodeID != out[j].DestinationNodeID {
			return out[i].DestinationNodeID < out[j].DestinationNodeID
		}
		if out[i].ChannelID != out[j].ChannelID {
			return out[i].ChannelID < out[j].ChannelID
		}
		return out[i].SequenceNumber < out[j].SequenceNumber
	})
	return out
}

func (q *Queue) drainReady(key string, nowTick uint64) {
	for {
		expected := q.expected(key)
		var readyID string
		var ready bundleprotocol.Envelope
		for bundleID, env := range q.gapPending {
			if streamKey(env) == key && env.SequenceNumber == expected {
				readyID = bundleID
				ready = env
				break
			}
		}
		if readyID == "" {
			return
		}
		delete(q.gapPending, readyID)
		if err := ready.Validate(nowTick); err != nil {
			continue
		}
		applied := q.ledger.Apply(Transaction{ID: ready.TransactionID, Sequence: ready.SequenceNumber, Payload: string(ready.PayloadBytes)})
		if applied.Applied {
			q.unconfirmed[ready.BundleID] = ready
			q.nextSeq[key] = expected + 1
		}
	}
}

func (q *Queue) expected(key string) uint64 {
	if next := q.nextSeq[key]; next > 0 {
		return next
	}
	q.nextSeq[key] = 1
	return 1
}

func streamKey(env bundleprotocol.Envelope) string {
	return fmt.Sprintf("%s\x00%s\x00%s", env.SourceNodeID, env.DestinationNodeID, env.ChannelID)
}
