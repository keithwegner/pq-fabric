package health

import (
	"sort"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
)

type Status string

const (
	StatusHealthy         Status = "healthy"
	StatusDegraded        Status = "degraded"
	StatusSuspectedFailed Status = "suspected_failed"
	StatusFailed          Status = "failed"
	StatusRecovering      Status = "recovering"
	StatusRecovered       Status = "recovered"
)

const (
	EventStatusChanged        = "status_changed"
	EventFailureDetected      = "failure_detected"
	EventLaggingDetected      = "lagging_detected"
	EventInconsistentDetected = "inconsistent_state_detected"
	EventRemediationStarted   = "remediation_started"
	EventRemediationCompleted = "remediation_completed"
	EventConvergenceVerified  = "convergence_verified"
	EventMessagePreservation  = "message_preservation"
)

type HeartbeatRecord struct {
	ValidatorID        string `json:"validator_id"`
	Region             string `json:"region"`
	Height             uint64 `json:"height"`
	Round              uint64 `json:"round"`
	BlockHash          string `json:"block_hash"`
	StateDigest        string `json:"state_digest"`
	LogicalTick        uint64 `json:"logical_tick"`
	TimestampUnixMilli int64  `json:"timestamp_unix_milli,omitempty"`
	Status             Status `json:"status,omitempty"`
}

type ValidatorHealth struct {
	ValidatorID              string `json:"validator_id"`
	Region                   string `json:"region"`
	CurrentHeight            uint64 `json:"current_height"`
	CurrentRound             uint64 `json:"current_round"`
	LatestCommittedBlockHash string `json:"latest_committed_block_hash"`
	LatestStateDigest        string `json:"latest_state_digest"`
	LastHeartbeatTick        uint64 `json:"last_heartbeat_tick"`
	LastHeartbeatUnixMilli   int64  `json:"last_heartbeat_unix_milli,omitempty"`
	Status                   Status `json:"status"`
	FailureDetectedTick      uint64 `json:"failure_detected_tick,omitempty"`
	RemediationStartedTick   uint64 `json:"remediation_started_tick,omitempty"`
	RemediationCompletedTick uint64 `json:"remediation_completed_tick,omitempty"`
	RecoveryCompletedTick    uint64 `json:"recovery_completed_tick,omitempty"`
	LastStatusChangeTick     uint64 `json:"last_status_change_tick,omitempty"`
	Reason                   string `json:"reason,omitempty"`
}

type EvidenceRecord struct {
	EventType          string `json:"event_type"`
	ValidatorID        string `json:"validator_id,omitempty"`
	ObservedStatus     Status `json:"observed_status,omitempty"`
	PreviousStatus     Status `json:"previous_status,omitempty"`
	Height             uint64 `json:"height,omitempty"`
	Round              uint64 `json:"round,omitempty"`
	BlockHash          string `json:"block_hash,omitempty"`
	StateDigest        string `json:"state_digest,omitempty"`
	LogicalTick        uint64 `json:"logical_tick"`
	TimestampUnixMilli int64  `json:"timestamp_unix_milli,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type DetectorConfig struct {
	SuspectAfterTicks  uint64
	FailAfterTicks     uint64
	LagHeightThreshold uint64
}

type Detector struct {
	cfg    DetectorConfig
	states map[string]ValidatorHealth
}

func NewDetector(cfg DetectorConfig) *Detector {
	if cfg.SuspectAfterTicks == 0 {
		cfg.SuspectAfterTicks = 2
	}
	if cfg.FailAfterTicks == 0 {
		cfg.FailAfterTicks = cfg.SuspectAfterTicks + 1
	}
	if cfg.FailAfterTicks < cfg.SuspectAfterTicks {
		cfg.FailAfterTicks = cfg.SuspectAfterTicks
	}
	return &Detector{cfg: cfg, states: make(map[string]ValidatorHealth)}
}

func HeartbeatFromSnapshot(snapshot protocol.StateSnapshot, logicalTick uint64, status Status) HeartbeatRecord {
	if status == "" {
		status = StatusHealthy
	}
	return HeartbeatRecord{
		ValidatorID:        snapshot.NodeID,
		Region:             snapshot.Region,
		Height:             snapshot.Height,
		Round:              snapshot.Round,
		BlockHash:          snapshot.LastHash,
		StateDigest:        snapshot.StateDigest,
		LogicalTick:        logicalTick,
		TimestampUnixMilli: time.Now().UnixMilli(),
		Status:             status,
	}
}

func (d *Detector) Observe(hb HeartbeatRecord, reference *HeartbeatRecord) []EvidenceRecord {
	if hb.Status == "" {
		hb.Status = StatusHealthy
	}
	state := d.states[hb.ValidatorID]
	previous := state.Status
	if previous == "" {
		previous = StatusHealthy
	}
	next := hb.Status
	reason := "heartbeat received"
	if reference != nil {
		switch {
		case hb.Height == reference.Height && hb.BlockHash != "" && reference.BlockHash != "" && hb.BlockHash != reference.BlockHash:
			next = StatusFailed
			reason = "reported block hash differs from reference committed hash"
		case hb.Height == reference.Height && hb.StateDigest != "" && reference.StateDigest != "" && hb.StateDigest != reference.StateDigest:
			next = StatusFailed
			reason = "reported state digest differs from reference committed state digest"
		case reference.Height > hb.Height+d.cfg.LagHeightThreshold:
			next = StatusDegraded
			reason = "validator is lagging behind committed height"
		case isUnhealthy(previous) && hb.Height == reference.Height && hb.BlockHash == reference.BlockHash && hb.StateDigest == reference.StateDigest:
			next = StatusRecovered
			reason = "validator heartbeat converged to reference state"
		default:
			next = StatusHealthy
		}
	}
	state.ValidatorID = hb.ValidatorID
	state.Region = hb.Region
	state.CurrentHeight = hb.Height
	state.CurrentRound = hb.Round
	state.LatestCommittedBlockHash = hb.BlockHash
	state.LatestStateDigest = hb.StateDigest
	state.LastHeartbeatTick = hb.LogicalTick
	state.LastHeartbeatUnixMilli = hb.TimestampUnixMilli
	state.Reason = reason
	events := d.setStatus(&state, previous, next, hb.LogicalTick, hb.TimestampUnixMilli, reason)
	d.states[hb.ValidatorID] = state
	return events
}

func (d *Detector) Evaluate(logicalTick uint64, timestampUnixMilli int64, _ HeartbeatRecord) []EvidenceRecord {
	var events []EvidenceRecord
	ids := make([]string, 0, len(d.states))
	for id := range d.states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		state := d.states[id]
		if state.LastHeartbeatTick == 0 || logicalTick <= state.LastHeartbeatTick {
			continue
		}
		missed := logicalTick - state.LastHeartbeatTick
		previous := state.Status
		next := previous
		reason := ""
		if missed >= d.cfg.FailAfterTicks {
			next = StatusFailed
			reason = "heartbeat failure threshold exceeded"
		} else if missed >= d.cfg.SuspectAfterTicks {
			next = StatusSuspectedFailed
			reason = "heartbeat suspect threshold exceeded"
		}
		if next == previous || next == "" {
			continue
		}
		state.Reason = reason
		events = append(events, d.setStatus(&state, previous, next, logicalTick, timestampUnixMilli, reason)...)
		d.states[id] = state
	}
	return events
}

func (d *Detector) MarkRecovering(validatorID string, logicalTick uint64, timestampUnixMilli int64, reason string) EvidenceRecord {
	state := d.states[validatorID]
	previous := state.Status
	if previous == "" {
		previous = StatusHealthy
	}
	state.Status = StatusRecovering
	state.RemediationStartedTick = logicalTick
	state.LastStatusChangeTick = logicalTick
	state.Reason = reason
	d.states[validatorID] = state
	return EvidenceRecord{
		EventType:          EventRemediationStarted,
		ValidatorID:        validatorID,
		ObservedStatus:     StatusRecovering,
		PreviousStatus:     previous,
		Height:             state.CurrentHeight,
		Round:              state.CurrentRound,
		BlockHash:          state.LatestCommittedBlockHash,
		StateDigest:        state.LatestStateDigest,
		LogicalTick:        logicalTick,
		TimestampUnixMilli: timestampUnixMilli,
		Reason:             reason,
	}
}

func (d *Detector) MarkRemediationCompleted(validatorID string, logicalTick uint64, timestampUnixMilli int64, reason string) EvidenceRecord {
	state := d.states[validatorID]
	previous := state.Status
	if previous == "" {
		previous = StatusRecovering
	}
	state.Status = StatusRecovered
	state.RemediationCompletedTick = logicalTick
	state.RecoveryCompletedTick = logicalTick
	state.LastStatusChangeTick = logicalTick
	state.Reason = reason
	d.states[validatorID] = state
	return EvidenceRecord{
		EventType:          EventRemediationCompleted,
		ValidatorID:        validatorID,
		ObservedStatus:     StatusRecovered,
		PreviousStatus:     previous,
		Height:             state.CurrentHeight,
		Round:              state.CurrentRound,
		BlockHash:          state.LatestCommittedBlockHash,
		StateDigest:        state.LatestStateDigest,
		LogicalTick:        logicalTick,
		TimestampUnixMilli: timestampUnixMilli,
		Reason:             reason,
	}
}

func (d *Detector) State(validatorID string) (ValidatorHealth, bool) {
	state, ok := d.states[validatorID]
	return state, ok
}

func (d *Detector) States() []ValidatorHealth {
	ids := make([]string, 0, len(d.states))
	for id := range d.states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ValidatorHealth, 0, len(ids))
	for _, id := range ids {
		out = append(out, d.states[id])
	}
	return out
}

func (d *Detector) setStatus(state *ValidatorHealth, previous, next Status, logicalTick uint64, timestampUnixMilli int64, reason string) []EvidenceRecord {
	if next == "" {
		next = StatusHealthy
	}
	state.Status = next
	if previous == next && state.LastStatusChangeTick != 0 {
		return nil
	}
	state.LastStatusChangeTick = logicalTick
	if next == StatusFailed && state.FailureDetectedTick == 0 {
		state.FailureDetectedTick = logicalTick
	}
	eventType := EventStatusChanged
	switch next {
	case StatusFailed:
		if reason == "reported block hash differs from reference committed hash" || reason == "reported state digest differs from reference committed state digest" {
			eventType = EventInconsistentDetected
		} else {
			eventType = EventFailureDetected
		}
	case StatusDegraded:
		eventType = EventLaggingDetected
	}
	return []EvidenceRecord{{
		EventType:          eventType,
		ValidatorID:        state.ValidatorID,
		ObservedStatus:     next,
		PreviousStatus:     previous,
		Height:             state.CurrentHeight,
		Round:              state.CurrentRound,
		BlockHash:          state.LatestCommittedBlockHash,
		StateDigest:        state.LatestStateDigest,
		LogicalTick:        logicalTick,
		TimestampUnixMilli: timestampUnixMilli,
		Reason:             reason,
	}}
}

func isUnhealthy(status Status) bool {
	switch status {
	case StatusDegraded, StatusSuspectedFailed, StatusFailed, StatusRecovering:
		return true
	default:
		return false
	}
}
