package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/keithwegner/pq-fabric/consensus/fault"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func main() {
	report, err := fault.RunScenario(context.Background(), fault.Options{
		DataRoot:         filepath.Join("tmp", "fault-demo-data"),
		StorageMode:      storage.ModeDurable,
		ResetData:        true,
		WriteArtifacts:   true,
		EvidenceJSONPath: filepath.Join("tmp", "failure-evidence.json"),
		EvidenceTextPath: filepath.Join("tmp", "failure-evidence.txt"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("pq-fabric controlled fault demo")
	fmt.Printf("validators=%d threshold=%d failed=%d timing=%s\n", report.ValidatorCount, report.QuorumThreshold, report.FailedValidatorCount, report.TimingMode)
	fmt.Printf("commits_during_failure=%d final_height=%d converged=%t\n", report.CommitCountDuringFailure, report.FinalHeight, report.FinalConvergence)
	fmt.Printf("transactions submitted=%d committed_unique=%d duplicate_replayed=%d pending=%d\n", report.SubmittedTransactionCount, report.CommittedTransactionCount, report.DuplicateReplayedTransactionCount, report.PendingRetriedTransactionCount)
	fmt.Printf("latency_ticks detection=%d remediation=%d catch_up=%d total=%d\n", report.DetectionLatencyTicks, report.RemediationLatencyTicks, report.RecoveryCatchUpLatencyTicks, report.TotalScenarioDurationTicks)
	fmt.Printf("final hash=%s state=%s\n", short(report.FinalBlockHash), short(report.FinalStateDigest))
	fmt.Printf("evidence_json=%s\n", report.EvidenceJSONPath)
	fmt.Printf("evidence_text=%s\n", report.EvidenceTextPath)
	fmt.Println("key evidence events:")
	for _, event := range report.Events {
		switch event.EventType {
		case "failure_detected", "remediation_started", "remediation_completed", "convergence_verified", "message_preservation":
			fmt.Printf("  tick=%d event=%s validator=%s status=%s reason=%s\n", event.LogicalTick, event.EventType, event.ValidatorID, event.ObservedStatus, event.Reason)
		}
	}
}

func short(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
