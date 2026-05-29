package health

import "testing"

func TestDetectorTransitionsSuspectedThenFailed(t *testing.T) {
	detector := NewDetector(DetectorConfig{SuspectAfterTicks: 1, FailAfterTicks: 2})
	reference := HeartbeatRecord{
		ValidatorID: "validator-1",
		Region:      "nyc",
		Height:      1,
		Round:       0,
		BlockHash:   "hash-1",
		StateDigest: "state-1",
		LogicalTick: 1,
		Status:      StatusHealthy,
	}
	detector.Observe(reference, &reference)

	events := detector.Evaluate(2, 0, reference)
	if len(events) != 1 || events[0].ObservedStatus != StatusSuspectedFailed {
		t.Fatalf("expected suspected_failed event, got %+v", events)
	}
	events = detector.Evaluate(3, 0, reference)
	if len(events) != 1 || events[0].ObservedStatus != StatusFailed || events[0].EventType != EventFailureDetected {
		t.Fatalf("expected failed event, got %+v", events)
	}
	state, ok := detector.State("validator-1")
	if !ok || state.FailureDetectedTick != 3 {
		t.Fatalf("expected failure tick 3, got %+v ok=%t", state, ok)
	}
}

func TestDetectorDetectsLaggingAndInconsistentState(t *testing.T) {
	detector := NewDetector(DetectorConfig{SuspectAfterTicks: 2, FailAfterTicks: 3, LagHeightThreshold: 0})
	reference := HeartbeatRecord{
		ValidatorID: "validator-1",
		Region:      "nyc",
		Height:      5,
		Round:       0,
		BlockHash:   "hash-5",
		StateDigest: "state-5",
		LogicalTick: 5,
		Status:      StatusHealthy,
	}
	lagging := HeartbeatRecord{
		ValidatorID: "validator-2",
		Region:      "nyc",
		Height:      4,
		Round:       0,
		BlockHash:   "hash-4",
		StateDigest: "state-4",
		LogicalTick: 5,
		Status:      StatusHealthy,
	}
	events := detector.Observe(lagging, &reference)
	if len(events) != 1 || events[0].ObservedStatus != StatusDegraded || events[0].EventType != EventLaggingDetected {
		t.Fatalf("expected lagging degraded event, got %+v", events)
	}

	inconsistent := HeartbeatRecord{
		ValidatorID: "validator-3",
		Region:      "london",
		Height:      5,
		Round:       0,
		BlockHash:   "different-hash",
		StateDigest: "state-5",
		LogicalTick: 5,
		Status:      StatusHealthy,
	}
	events = detector.Observe(inconsistent, &reference)
	if len(events) != 1 || events[0].ObservedStatus != StatusFailed || events[0].EventType != EventInconsistentDetected {
		t.Fatalf("expected inconsistent failed event, got %+v", events)
	}
}

func TestDetectorRecordsRecovery(t *testing.T) {
	detector := NewDetector(DetectorConfig{SuspectAfterTicks: 1, FailAfterTicks: 2})
	reference := HeartbeatRecord{
		ValidatorID: "validator-1",
		Region:      "nyc",
		Height:      2,
		Round:       0,
		BlockHash:   "hash-2",
		StateDigest: "state-2",
		LogicalTick: 1,
		Status:      StatusHealthy,
	}
	detector.Observe(reference, &reference)
	_ = detector.Evaluate(3, 0, reference)

	started := detector.MarkRecovering("validator-1", 4, 0, "restart requested by local harness")
	if started.EventType != EventRemediationStarted || started.ObservedStatus != StatusRecovering {
		t.Fatalf("expected remediation start event, got %+v", started)
	}
	recoveredHeartbeat := reference
	recoveredHeartbeat.LogicalTick = 5
	events := detector.Observe(recoveredHeartbeat, &reference)
	if len(events) != 1 || events[0].ObservedStatus != StatusRecovered {
		t.Fatalf("expected recovered event, got %+v", events)
	}
	completed := detector.MarkRemediationCompleted("validator-1", 6, 0, "catch-up converged to reference")
	if completed.EventType != EventRemediationCompleted || completed.ObservedStatus != StatusRecovered {
		t.Fatalf("expected remediation completion event, got %+v", completed)
	}
}
