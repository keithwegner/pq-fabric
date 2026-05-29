package fault

import (
	"context"
	"testing"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/health"
	"github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func TestRunScenarioProducesRecoveryAndMessageEvidence(t *testing.T) {
	t.Setenv(suite.EnvVar, string(suite.Dev))
	report, err := RunScenario(context.Background(), Options{
		DataRoot:    t.TempDir(),
		StorageMode: storage.ModeDurable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.FinalConvergence {
		t.Fatalf("expected final convergence: %+v", report)
	}
	if report.ValidatorCount != 7 || report.FailedValidatorCount != 2 || report.QuorumThreshold != 5 {
		t.Fatalf("unexpected validator/quorum counts: %+v", report)
	}
	if report.CommitCountDuringFailure != 3 || report.FinalHeight != 4 {
		t.Fatalf("expected three commits during failure and final height 4, got commits=%d height=%d", report.CommitCountDuringFailure, report.FinalHeight)
	}
	if report.SubmittedTransactionCount != 3 || report.CommittedTransactionCount != 2 || report.DuplicateReplayedTransactionCount != 1 || report.PendingRetriedTransactionCount != 0 {
		t.Fatalf("unexpected transaction preservation metrics: submitted=%d committed=%d duplicates=%d pending=%d", report.SubmittedTransactionCount, report.CommittedTransactionCount, report.DuplicateReplayedTransactionCount, report.PendingRetriedTransactionCount)
	}
	requireEvent(t, report, health.EventFailureDetected)
	requireEvent(t, report, health.EventRemediationStarted)
	requireEvent(t, report, health.EventRemediationCompleted)
	requireEvent(t, report, health.EventConvergenceVerified)
	requireEvent(t, report, health.EventMessagePreservation)
}

func TestOneValidatorFailureContinuesCommitting(t *testing.T) {
	t.Setenv(suite.EnvVar, string(suite.Dev))
	cluster := newMemoryTestCluster(t)
	defer cluster.Close()
	if err := cluster.StartAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Propose(context.Background(), "baseline"); err != nil {
		t.Fatal(err)
	}
	if err := cluster.Stop("validator-7"); err != nil {
		t.Fatal(err)
	}
	commit, err := cluster.Propose(context.Background(), "one validator failed")
	if err != nil {
		t.Fatal(err)
	}
	if commit.Block.Height != 2 {
		t.Fatalf("expected height 2 commit, got %d", commit.Block.Height)
	}
	if len(commit.Certificate.Votes) < 5 {
		t.Fatalf("expected at least 5 votes, got %d", len(commit.Certificate.Votes))
	}
}

func TestThreeValidatorFailureDoesNotCommit(t *testing.T) {
	t.Setenv(suite.EnvVar, string(suite.Dev))
	cluster := newMemoryTestCluster(t)
	defer cluster.Close()
	if err := cluster.StartAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Propose(context.Background(), "baseline"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"validator-5", "validator-6", "validator-7"} {
		if err := cluster.Stop(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cluster.Propose(context.Background(), "three validators failed"); err == nil {
		t.Fatal("expected commit to fail with only four live validators")
	}
	for _, id := range []string{"validator-1", "validator-2", "validator-3", "validator-4"} {
		if got := cluster.nodes[id].Snapshot().Height; got != 1 {
			t.Fatalf("%s should remain at height 1, got %d", id, got)
		}
	}
}

func newMemoryTestCluster(t *testing.T) *cluster {
	t.Helper()
	opts := normalizeOptions(Options{
		StorageMode:     storage.ModeMemory,
		RequestTimeout:  120 * time.Millisecond,
		ProposalTimeout: 80 * time.Millisecond,
		VoteTimeout:     120 * time.Millisecond,
	})
	cluster, err := newCluster(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return cluster
}

func requireEvent(t *testing.T, report ScenarioReport, eventType string) {
	t.Helper()
	for _, event := range report.Events {
		if event.EventType == eventType {
			return
		}
	}
	t.Fatalf("expected event %s in %+v", eventType, report.Events)
}
