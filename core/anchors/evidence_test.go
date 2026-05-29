package anchors

import (
	"context"
	"testing"
)

func TestRunEvidenceScenario(t *testing.T) {
	t.Setenv("PQ_FABRIC_CRYPTO_SUITE", "dev")
	evidence, err := RunEvidenceScenario(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.ValidatorIdentityAnchors) != 7 {
		t.Fatalf("expected seven identities, got %d", len(evidence.ValidatorIdentityAnchors))
	}
	if len(evidence.CredentialAnchors) != 1 || len(evidence.GovernanceProposals) != 1 || len(evidence.QuorumCertificateAnchors) != 1 {
		t.Fatalf("missing anchor evidence: %+v", evidence)
	}
	if evidence.Status.IdentityCount != 7 || evidence.Status.QuorumCertificateCount != 1 {
		t.Fatalf("unexpected mock status: %+v", evidence.Status)
	}
	if evidence.DuplicateReplayOutcome == "" || evidence.MismatchOutcome == "" {
		t.Fatal("expected duplicate and mismatch evidence")
	}
}
