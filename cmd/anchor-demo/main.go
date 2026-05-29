package main

import (
	"context"
	"fmt"
	"log"

	"github.com/keithwegner/pq-fabric/core/anchors"
)

func main() {
	evidence, err := anchors.RunEvidenceScenario(context.Background(), "tmp")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("pq-fabric Polygon anchor demo")
	fmt.Printf("identity_anchors=%d credential_anchors=%d governance_anchors=%d qc_anchors=%d\n", len(evidence.ValidatorIdentityAnchors), len(evidence.CredentialAnchors), len(evidence.GovernanceProposals), len(evidence.QuorumCertificateAnchors))
	fmt.Printf("authorization=%s\n", evidence.AuthorizationModel)
	fmt.Printf("duplicate_replay=%s\n", evidence.DuplicateReplayOutcome)
	fmt.Printf("mismatch=%s\n", evidence.MismatchOutcome)
	if len(evidence.QuorumCertificateAnchors) > 0 {
		qc := evidence.QuorumCertificateAnchors[0]
		fmt.Printf("qc_anchor height=%d round=%d threshold=%d signer_count=%d hash=%s\n", qc.Height, qc.Round, qc.Threshold, qc.SignerCount, short(qc.QCHash))
	}
	fmt.Printf("contract_tests=%s\n", evidence.ContractTestStatus)
	fmt.Println("boundary: on-chain contracts anchor hashes/metadata only; PQ signatures and quorum certificates remain verified off-chain")
	fmt.Println("evidence_json=tmp/anchor-evidence.json")
	fmt.Println("evidence_text=tmp/anchor-evidence.txt")
}

func short(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}
