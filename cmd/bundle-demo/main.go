package main

import (
	"fmt"
	"log"

	"github.com/keithwegner/pq-fabric/bundle/evidence"
)

func main() {
	ev, err := evidence.RunScenario("tmp")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("pq-fabric bundle protocol demo")
	fmt.Printf("channels=%d bundles=%d custody=%s quorum=%d\n", len(ev.ChannelDefinitions), len(ev.BundleIDs), ev.CustodyConfirmationStatus, ev.QuorumSize)
	fmt.Printf("scheduled=%d retransmissions=%d duplicates=%d reconciled=%d evicted=%d\n", len(ev.SchedulingDecisions), ev.RetransmissionCount, ev.DuplicateTransactionCount, ev.ReconciledBundleCount, ev.EvictedContextItemCount)
	for _, decision := range ev.SchedulingDecisions {
		fmt.Printf("  schedule[%d] channel=%s seq=%d priority=%d bundle=%s tx=%s\n", decision.Order, decision.ChannelID, decision.Sequence, decision.Priority, decision.BundleID, decision.Transaction)
	}
	fmt.Printf("mock_request_digest=%s\n", ev.MockAIRequestDigest[:16])
	fmt.Printf("mock_response_digest=%s\n", ev.MockAIResponseDigest[:16])
	fmt.Printf("final_state_digest=%s\n", ev.FinalStateDigest[:16])
	fmt.Println("mock provider: local OpenAI-compatible shape only; no external API call")
	fmt.Println("evidence_json=tmp/bundle-evidence.json")
	fmt.Println("evidence_text=tmp/bundle-evidence.txt")
}
