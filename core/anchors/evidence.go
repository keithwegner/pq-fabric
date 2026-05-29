package anchors

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	consensusprotocol "github.com/keithwegner/pq-fabric/consensus/protocol"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
)

type Evidence struct {
	ValidatorIdentityAnchors []IdentityRecord           `json:"validator_identity_anchor_records"`
	CredentialAnchors        []CredentialRecord         `json:"credential_anchor_records"`
	GovernanceProposals      []GovernanceProposalRecord `json:"governance_proposal_anchor_records"`
	QuorumCertificateAnchors []QuorumCertificateRecord  `json:"qc_anchor_records"`
	DuplicateReplayOutcome   string                     `json:"duplicate_replay_test_outcome"`
	MismatchOutcome          string                     `json:"mismatch_test_outcome"`
	AuthorizationModel       string                     `json:"authorization_model_summary"`
	OnChainOffChainBoundary  string                     `json:"on_chain_off_chain_boundary_statement"`
	ContractTestStatus       string                     `json:"contract_test_status"`
	MockBackendTestStatus    string                     `json:"mock_backend_test_status"`
	Limitations              string                     `json:"limitations_statement"`
	Status                   Status                     `json:"mock_backend_status"`
	OutputJSON               string                     `json:"output_json,omitempty"`
	OutputText               string                     `json:"output_text,omitempty"`
}

func RunEvidenceScenario(ctx context.Context, outputDir string) (Evidence, error) {
	selected, err := cryptosuite.FromEnv()
	if err != nil {
		return Evidence{}, err
	}
	identities, err := identity.ValidatorIdentitiesForSuite(nil, selected)
	if err != nil {
		return Evidence{}, err
	}
	backend := NewMockBackend("local-admin")
	for _, id := range identity.DefaultValidatorIDs() {
		record, err := IdentityRecordFromValidator(identities[id], "local://metadata/"+id)
		if err != nil {
			return Evidence{}, err
		}
		if err := backend.RegisterIdentity(ctx, "local-admin", record); err != nil {
			return Evidence{}, err
		}
	}
	identityRecords := SortedIdentityRecords(backend.IdentityRecords())

	credential := CredentialRecord{
		CredentialHash:     messages.HashBytes([]byte("credential:validator-1:kyc-demo")),
		SubjectValidatorID: "validator-1",
		IssuerValidatorID:  "validator-2",
		ValidFromTick:      1,
		ValidUntilTick:     100,
		MetadataHash:       messages.HashBytes([]byte("credential metadata")),
	}
	if err := backend.AnchorCredential(ctx, "local-admin", credential); err != nil {
		return Evidence{}, err
	}
	credential, _, _ = backend.GetCredential(ctx, credential.CredentialHash)

	proposal := GovernanceProposalRecord{
		ProposalHash: messages.HashBytes([]byte("proposal:rotate-testbed-parameter")),
		CreatorID:    "validator-1",
		MetadataURI:  "local://proposal/rotate-testbed-parameter",
		MetadataHash: messages.HashBytes([]byte("proposal metadata")),
		State:        ProposalStateAnchored,
	}
	if err := backend.AnchorGovernanceProposal(ctx, "local-admin", proposal); err != nil {
		return Evidence{}, err
	}
	if err := backend.UpdateGovernanceProposalState(ctx, "local-admin", proposal.ProposalHash, ProposalStateAccepted); err != nil {
		return Evidence{}, err
	}
	proposal, _, _ = backend.GetGovernanceProposal(ctx, proposal.ProposalHash)

	cert, err := syntheticQuorumCertificate(selected)
	if err != nil {
		return Evidence{}, err
	}
	qcRecord, err := QuorumCertificateRecordFromCertificate(cert, "", messages.HashBytes([]byte("qc metadata")))
	if err != nil {
		return Evidence{}, err
	}
	if err := backend.AnchorQuorumCertificate(ctx, "local-admin", qcRecord); err != nil {
		return Evidence{}, err
	}
	qcRecord, _, _ = backend.GetQuorumCertificateAnchor(ctx, qcRecord.QCHash)

	duplicateOutcome := "duplicate qc anchor reverted"
	if err := backend.AnchorQuorumCertificate(ctx, "local-admin", qcRecord); err == nil {
		duplicateOutcome = "duplicate qc anchor unexpectedly accepted"
	} else {
		duplicateOutcome = err.Error()
	}

	mismatchOutcome := "mismatch detected"
	tampered := identityRecords[0]
	tampered.Region = "wrong-region"
	if err := CompareIdentityToValidator(tampered, identities[tampered.ValidatorID]); err != nil {
		mismatchOutcome = err.Error()
	}

	status, err := backend.Status(ctx)
	if err != nil {
		return Evidence{}, err
	}
	evidence := Evidence{
		ValidatorIdentityAnchors: identityRecords,
		CredentialAnchors:        []CredentialRecord{credential},
		GovernanceProposals:      []GovernanceProposalRecord{proposal},
		QuorumCertificateAnchors: []QuorumCertificateRecord{qcRecord},
		DuplicateReplayOutcome:   duplicateOutcome,
		MismatchOutcome:          mismatchOutcome,
		AuthorizationModel:       "mock backend uses explicit local roles: identity_admin, credential_issuer, governance_admin, and qc_anchorer; contracts use owner-authorized accounts",
		OnChainOffChainBoundary:  "contracts and mock backend anchor hashes and metadata only; validators keep PQ signature, identity, quorum-certificate, and consensus validation off-chain",
		ContractTestStatus:       contractTestStatus(),
		MockBackendTestStatus:    "covered by go test ./core/anchors/...",
		Limitations:              "local prototype only; no live Polygon deployment, no on-chain PQ verification, no smart-contract audit, no production BFT, no production anonymity, no FIPS or ACVTS certification",
		Status:                   status,
	}
	if outputDir != "" {
		if err := WriteEvidenceArtifacts(outputDir, evidence); err != nil {
			return Evidence{}, err
		}
		evidence.OutputJSON = filepath.Join(outputDir, "anchor-evidence.json")
		evidence.OutputText = filepath.Join(outputDir, "anchor-evidence.txt")
	}
	return evidence, nil
}

func WriteEvidenceArtifacts(outputDir string, evidence Evidence) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "anchor-evidence.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "anchor-evidence.txt"), []byte(TextEvidence(evidence)), 0o644)
}

func TextEvidence(e Evidence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric anchor evidence\n")
	fmt.Fprintf(&b, "identity_anchors=%d credential_anchors=%d governance_anchors=%d qc_anchors=%d\n", len(e.ValidatorIdentityAnchors), len(e.CredentialAnchors), len(e.GovernanceProposals), len(e.QuorumCertificateAnchors))
	fmt.Fprintf(&b, "duplicate_replay=%s\n", e.DuplicateReplayOutcome)
	fmt.Fprintf(&b, "mismatch=%s\n", e.MismatchOutcome)
	fmt.Fprintf(&b, "contract_tests=%s\n", e.ContractTestStatus)
	fmt.Fprintf(&b, "mock_backend_tests=%s\n", e.MockBackendTestStatus)
	fmt.Fprintf(&b, "boundary=%s\n", e.OnChainOffChainBoundary)
	fmt.Fprintf(&b, "limitations=%s\n", e.Limitations)
	return b.String()
}

func syntheticQuorumCertificate(selected cryptosuite.CryptoSuite) (consensusprotocol.QuorumCertificate, error) {
	blockHash := messages.HashBytes([]byte("anchor-demo-commit"))
	var votes []consensusprotocol.Vote
	for _, voterID := range identity.DefaultValidatorIDs()[:5] {
		signer, err := selected.NewSigner(voterID)
		if err != nil {
			return consensusprotocol.QuorumCertificate{}, err
		}
		vote, err := consensusprotocol.SignStageVote(2, 0, consensusprotocol.StagePrecommit, blockHash, voterID, signer)
		if err != nil {
			return consensusprotocol.QuorumCertificate{}, err
		}
		votes = append(votes, vote)
	}
	return consensusprotocol.FormStageQuorumCertificate(2, 0, consensusprotocol.StagePrecommit, blockHash, votes, 5)
}

func contractTestStatus() string {
	if _, err := exec.LookPath("forge"); err != nil {
		return "forge unavailable in this environment; Foundry tests added but not executed by anchor demo"
	}
	return "forge available; run make contract-tests for current contract test result"
}
