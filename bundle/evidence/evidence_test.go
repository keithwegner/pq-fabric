package evidence

import "testing"

func TestRunScenarioProducesBundleEvidence(t *testing.T) {
	t.Setenv("PQ_FABRIC_CRYPTO_SUITE", "dev")
	ev, err := RunScenario(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.ChannelDefinitions) != 4 {
		t.Fatalf("expected four AI context channels, got %d", len(ev.ChannelDefinitions))
	}
	if len(ev.BundleIDs) == 0 || len(ev.SchedulingDecisions) != len(ev.BundleIDs) {
		t.Fatalf("expected bundles and scheduling decisions, got %+v", ev)
	}
	if ev.CustodyConfirmationStatus != "confirmed" || len(ev.CustodyEvents) != len(ev.BundleIDs) {
		t.Fatalf("custody confirmation missing: %+v", ev.CustodyEvents)
	}
	if ev.QuorumSize != 5 {
		t.Fatalf("expected 5-of-7 quorum evidence, got %d", ev.QuorumSize)
	}
	if ev.RetransmissionCount == 0 {
		t.Fatal("scenario should include retransmission after interruption")
	}
	if ev.DuplicateTransactionCount == 0 {
		t.Fatal("scenario should include duplicate transaction deduplication")
	}
	if ev.ReconciledBundleCount == 0 {
		t.Fatal("scenario should include reconciliation repair")
	}
	if ev.MockAIRequestDigest == "" || ev.MockAIResponseDigest == "" || ev.FinalStateDigest == "" {
		t.Fatalf("missing mock AI or final digest evidence: %+v", ev)
	}
}
