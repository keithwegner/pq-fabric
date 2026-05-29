package reconcile

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/keithwegner/pq-fabric/core/messages"
	"github.com/keithwegner/pq-fabric/core/storage"
)

type StateDigest struct {
	Height   uint64 `json:"height"`
	LastHash string `json:"last_hash"`
}

func DeterministicMatch(a, b StateDigest) error {
	if a.Height != b.Height || a.LastHash != b.LastHash {
		return fmt.Errorf("state divergence: left=(%d,%s) right=(%d,%s)", a.Height, a.LastHash, b.Height, b.LastHash)
	}
	return nil
}

const SnapshotID = "bundle-reconcile/state"

type BundleRecord struct {
	BundleID      string `json:"bundle_id"`
	Digest        string `json:"digest"`
	TransactionID string `json:"transaction_id"`
	ChannelID     string `json:"channel_id"`
	Sequence      uint64 `json:"sequence"`
}

type BundleState struct {
	LatestSequenceByChannel map[string]uint64       `json:"latest_sequence_by_channel"`
	CommittedTransactionIDs map[string]string       `json:"committed_transaction_ids"`
	CustodyStatusByBundle   map[string]string       `json:"custody_status_by_bundle"`
	ContextStateDigest      string                  `json:"context_state_digest"`
	PendingBundleIDs        []string                `json:"pending_bundle_ids"`
	Bundles                 map[string]BundleRecord `json:"bundles"`
}

type Plan struct {
	Noop                 bool     `json:"noop"`
	MissingBundleIDs     []string `json:"missing_bundle_ids"`
	DuplicateBundleIDs   []string `json:"duplicate_bundle_ids"`
	ConflictingBundleIDs []string `json:"conflicting_bundle_ids"`
	AppliedBundleIDs     []string `json:"applied_bundle_ids"`
}

func NewBundleState() BundleState {
	return BundleState{
		LatestSequenceByChannel: make(map[string]uint64),
		CommittedTransactionIDs: make(map[string]string),
		CustodyStatusByBundle:   make(map[string]string),
		Bundles:                 make(map[string]BundleRecord),
	}
}

func (s BundleState) Digest() (string, error) {
	return messages.HashCanonical(struct {
		LatestSequenceByChannel map[string]uint64       `json:"latest_sequence_by_channel"`
		CommittedTransactionIDs map[string]string       `json:"committed_transaction_ids"`
		CustodyStatusByBundle   map[string]string       `json:"custody_status_by_bundle"`
		PendingBundleIDs        []string                `json:"pending_bundle_ids"`
		Bundles                 map[string]BundleRecord `json:"bundles"`
	}{
		LatestSequenceByChannel: s.LatestSequenceByChannel,
		CommittedTransactionIDs: s.CommittedTransactionIDs,
		CustodyStatusByBundle:   s.CustodyStatusByBundle,
		PendingBundleIDs:        sortedStrings(s.PendingBundleIDs),
		Bundles:                 s.Bundles,
	})
}

func Compare(local, remote BundleState) Plan {
	var plan Plan
	for id, remoteRecord := range remote.Bundles {
		localRecord, ok := local.Bundles[id]
		if !ok {
			plan.MissingBundleIDs = append(plan.MissingBundleIDs, id)
			continue
		}
		if localRecord.Digest != remoteRecord.Digest {
			plan.ConflictingBundleIDs = append(plan.ConflictingBundleIDs, id)
			continue
		}
		plan.DuplicateBundleIDs = append(plan.DuplicateBundleIDs, id)
	}
	sort.Strings(plan.MissingBundleIDs)
	sort.Strings(plan.DuplicateBundleIDs)
	sort.Strings(plan.ConflictingBundleIDs)
	plan.Noop = len(plan.MissingBundleIDs) == 0 && len(plan.ConflictingBundleIDs) == 0
	return plan
}

func Apply(local, remote BundleState, plan Plan) (BundleState, error) {
	if len(plan.ConflictingBundleIDs) > 0 {
		return local, fmt.Errorf("bundle digest conflicts: %v", plan.ConflictingBundleIDs)
	}
	next := cloneState(local)
	for _, id := range plan.MissingBundleIDs {
		record, ok := remote.Bundles[id]
		if !ok {
			continue
		}
		next.Bundles[id] = record
		next.CommittedTransactionIDs[record.TransactionID] = record.Digest
		if record.Sequence > next.LatestSequenceByChannel[record.ChannelID] {
			next.LatestSequenceByChannel[record.ChannelID] = record.Sequence
		}
		if status, ok := remote.CustodyStatusByBundle[id]; ok {
			next.CustodyStatusByBundle[id] = status
		}
		plan.AppliedBundleIDs = append(plan.AppliedBundleIDs, id)
	}
	next.PendingBundleIDs = subtractPending(next.PendingBundleIDs, plan.MissingBundleIDs)
	digest, err := next.Digest()
	if err != nil {
		return next, err
	}
	if len(plan.MissingBundleIDs) > 0 {
		next.ContextStateDigest = digest
	} else if remote.ContextStateDigest != "" && len(plan.ConflictingBundleIDs) == 0 {
		next.ContextStateDigest = local.ContextStateDigest
	}
	return next, nil
}

func SaveSnapshot(store storage.ValidatorStore, state BundleState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	digest, err := state.Digest()
	if err != nil {
		return err
	}
	return store.SaveSnapshot(storage.SnapshotRecord{ID: SnapshotID, LastHash: digest, SnapshotJSON: data})
}

func LoadLatestSnapshot(store storage.ValidatorStore) (BundleState, bool, error) {
	snapshots, err := store.ListSnapshots()
	if err != nil {
		return BundleState{}, false, err
	}
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i].ID != SnapshotID {
			continue
		}
		var state BundleState
		if err := json.Unmarshal(snapshots[i].SnapshotJSON, &state); err != nil {
			return BundleState{}, false, err
		}
		ensureMaps(&state)
		return state, true, nil
	}
	return BundleState{}, false, nil
}

func cloneState(in BundleState) BundleState {
	ensureMaps(&in)
	out := NewBundleState()
	out.ContextStateDigest = in.ContextStateDigest
	out.PendingBundleIDs = append([]string(nil), in.PendingBundleIDs...)
	for k, v := range in.LatestSequenceByChannel {
		out.LatestSequenceByChannel[k] = v
	}
	for k, v := range in.CommittedTransactionIDs {
		out.CommittedTransactionIDs[k] = v
	}
	for k, v := range in.CustodyStatusByBundle {
		out.CustodyStatusByBundle[k] = v
	}
	for k, v := range in.Bundles {
		out.Bundles[k] = v
	}
	return out
}

func ensureMaps(s *BundleState) {
	if s.LatestSequenceByChannel == nil {
		s.LatestSequenceByChannel = make(map[string]uint64)
	}
	if s.CommittedTransactionIDs == nil {
		s.CommittedTransactionIDs = make(map[string]string)
	}
	if s.CustodyStatusByBundle == nil {
		s.CustodyStatusByBundle = make(map[string]string)
	}
	if s.Bundles == nil {
		s.Bundles = make(map[string]BundleRecord)
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func subtractPending(pending []string, applied []string) []string {
	appliedSet := make(map[string]struct{}, len(applied))
	for _, id := range applied {
		appliedSet[id] = struct{}{}
	}
	var out []string
	for _, id := range pending {
		if _, ok := appliedSet[id]; ok {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
