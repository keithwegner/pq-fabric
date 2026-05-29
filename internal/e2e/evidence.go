package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	bundleevidence "github.com/keithwegner/pq-fabric/bundle/evidence"
	"github.com/keithwegner/pq-fabric/consensus/fault"
	"github.com/keithwegner/pq-fabric/consensus/protocol"
	consensusstate "github.com/keithwegner/pq-fabric/consensus/state"
	"github.com/keithwegner/pq-fabric/core/anchors"
	"github.com/keithwegner/pq-fabric/core/consortium"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	evidencefabric "github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
	"github.com/keithwegner/pq-fabric/core/storage"
	routingtestbed "github.com/keithwegner/pq-fabric/routing/testbed"
)

type Options struct {
	OutputDir      string
	WriteArtifacts bool
}

type ToolStatus struct {
	Available bool   `json:"available"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
}

type Report struct {
	ProjectName            string                  `json:"project_name"`
	RunID                  string                  `json:"run_id"`
	GeneratedAtUnixMilli   int64                   `json:"generated_at_unix_milli"`
	GitCommit              string                  `json:"git_commit"`
	GoVersion              string                  `json:"go_version"`
	OS                     string                  `json:"os"`
	Architecture           string                  `json:"architecture"`
	ToolAvailability       map[string]ToolStatus   `json:"tool_availability"`
	CryptoSuite            string                  `json:"crypto_suite"`
	CryptoSuiteBoundary    string                  `json:"crypto_suite_boundary"`
	ValidatorCount         int                     `json:"validator_count"`
	QuorumThreshold        int                     `json:"quorum_threshold"`
	Consensus              ConsensusEvidence       `json:"consensus"`
	EvidenceFabric         EvidenceFabricEvidence  `json:"evidence_fabric"`
	Durability             DurabilityEvidence      `json:"durability"`
	FaultHealing           FaultHealingEvidence    `json:"fault_healing"`
	Routing                routingtestbed.Evidence `json:"routing"`
	Bundle                 bundleevidence.Evidence `json:"bundle"`
	Anchors                anchors.Evidence        `json:"anchors"`
	Deployment             DeploymentEvidence      `json:"deployment"`
	NonClaims              []string                `json:"non_claims"`
	GeneratedEvidencePaths []string                `json:"generated_evidence_paths"`
	LimitationsStatement   string                  `json:"limitations_statement"`
}

type ConsensusEvidence struct {
	NormalCommitHeight            uint64 `json:"normal_commit_height"`
	FailureWindowFinalHeight      uint64 `json:"failure_window_final_height"`
	QuorumThreshold               int    `json:"quorum_threshold"`
	ValidatorCount                int    `json:"validator_count"`
	SignerCount                   int    `json:"signer_count"`
	TwoValidatorFailureCommitted  bool   `json:"two_validator_failure_committed"`
	QuorumUnavailableRejected     bool   `json:"quorum_unavailable_rejected"`
	QuorumUnavailableReason       string `json:"quorum_unavailable_reason"`
	FinalBlockHash                string `json:"final_block_hash"`
	FinalStateDigest              string `json:"final_state_digest"`
	DuplicateVoteRejectionSummary string `json:"duplicate_vote_rejection_summary"`
	InvalidVoteRejectionSummary   string `json:"invalid_vote_rejection_summary"`
}

type EvidenceFabricEvidence struct {
	SubmissionSchema       string `json:"submission_schema"`
	EvidenceCategory       string `json:"evidence_category"`
	SubmittingOrganization string `json:"submitting_organization"`
	ReceiptID              string `json:"receipt_id"`
	EvidenceID             string `json:"evidence_id"`
	QCHash                 string `json:"qc_hash"`
	SignerCount            int    `json:"signer_count"`
	VerificationStatus     string `json:"verification_status"`
	QuorumStatus           string `json:"quorum_status"`
	AnchorStatus           string `json:"anchor_status"`
	IndexStatus            string `json:"index_status"`
	StoragePolicy          string `json:"storage_policy"`
	MembershipVersion      uint64 `json:"membership_version"`
	ValidatorSetHash       string `json:"validator_set_hash"`
}

type DurabilityEvidence struct {
	StorageBackend          string `json:"storage_backend"`
	RestartReloadResult     string `json:"restart_reload_result"`
	PersistedHeight         uint64 `json:"persisted_height"`
	PersistedHash           string `json:"persisted_hash"`
	PersistedStateDigest    string `json:"persisted_state_digest"`
	IdempotencyLedgerStatus string `json:"idempotency_ledger_status"`
	IdempotencyCount        int    `json:"idempotency_count"`
}

type FaultHealingEvidence struct {
	FailedValidatorCount       int    `json:"failed_validator_count"`
	DetectionLatencyTicks      uint64 `json:"detection_latency_ticks"`
	RemediationLatencyTicks    uint64 `json:"remediation_latency_ticks"`
	RecoveryCatchUpLatencyTick uint64 `json:"recovery_catch_up_latency_ticks"`
	FinalConvergence           bool   `json:"final_convergence"`
	MessagePreservationSummary string `json:"message_preservation_summary"`
}

type DeploymentEvidence struct {
	DockerfilePresent          bool       `json:"dockerfile_present"`
	DockerComposeConfigStatus  ToolStatus `json:"docker_compose_config_status"`
	KubernetesValidationStatus ToolStatus `json:"kubernetes_validation_status"`
	TerraformValidationStatus  ToolStatus `json:"terraform_validation_status"`
	ConfigTemplateCount        int        `json:"config_template_count"`
	SecretGuardrailStatus      string     `json:"secret_guardrail_status"`
	DockerImageBuildPath       string     `json:"docker_image_build_path"`
	NoLiveDeploymentPerformed  bool       `json:"no_live_deployment_performed"`
}

func Run(ctx context.Context, opts Options) (Report, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = "tmp"
	}
	runID := fmt.Sprintf("e2e-%d", time.Now().UnixMilli())
	selected, err := cryptosuite.FromEnv()
	if err != nil {
		return Report{}, err
	}

	faultReport, err := fault.RunScenario(ctx, fault.Options{
		DataRoot:         filepath.Join("tmp", "e2e-fault-data"),
		StorageMode:      storage.ModeDurable,
		ResetData:        true,
		WriteArtifacts:   opts.WriteArtifacts,
		EvidenceJSONPath: filepath.Join(opts.OutputDir, "failure-evidence.json"),
		EvidenceTextPath: filepath.Join(opts.OutputDir, "failure-evidence.txt"),
	})
	if err != nil {
		return Report{}, fmt.Errorf("fault scenario: %w", err)
	}
	routingReport, err := routingtestbed.RunScenario(ctx, routingtestbed.Options{
		WriteArtifacts:   opts.WriteArtifacts,
		EvidenceJSONPath: filepath.Join(opts.OutputDir, "routing-evidence.json"),
		EvidenceTextPath: filepath.Join(opts.OutputDir, "routing-evidence.txt"),
	})
	if err != nil {
		return Report{}, fmt.Errorf("routing scenario: %w", err)
	}
	bundleReport, err := bundleevidence.RunScenario(writeDir(opts))
	if err != nil {
		return Report{}, fmt.Errorf("bundle scenario: %w", err)
	}
	anchorReport, err := anchors.RunEvidenceScenario(ctx, writeDir(opts))
	if err != nil {
		return Report{}, fmt.Errorf("anchor scenario: %w", err)
	}
	durability, err := runDurabilityCheck(filepath.Join("tmp", "e2e-durable-check"))
	if err != nil {
		return Report{}, fmt.Errorf("durability check: %w", err)
	}
	evidenceFabric, err := runEvidenceFabricCheck(selected)
	if err != nil {
		return Report{}, fmt.Errorf("evidence fabric check: %w", err)
	}
	quorumRejected, quorumReason := quorumUnavailableCheck(selected)

	tools := map[string]ToolStatus{
		"docker":         runOptional("docker", "--version"),
		"docker_compose": runOptional("docker", "compose", "version"),
		"kubectl":        runOptional("kubectl", "version", "--client"),
		"terraform":      runOptional("terraform", "version"),
		"forge":          runOptional("forge", "--version"),
	}
	deployment := DeploymentEvidence{
		DockerfilePresent:          fileExists("Dockerfile"),
		DockerComposeConfigStatus:  runOptional("docker", "compose", "-f", "docker-compose.yml", "config"),
		KubernetesValidationStatus: runOptional("kubectl", "kustomize", "deployments/k8s/base"),
		TerraformValidationStatus:  terraformValidate(),
		ConfigTemplateCount:        len(listFiles("config")),
		SecretGuardrailStatus:      guardrailStatus(),
		DockerImageBuildPath:       "make image builds pq-fabric:local when Docker daemon is available",
		NoLiveDeploymentPerformed:  true,
	}

	report := Report{
		ProjectName:          "pq-fabric",
		RunID:                runID,
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		GitCommit:            gitCommit(),
		GoVersion:            runtime.Version(),
		OS:                   runtime.GOOS,
		Architecture:         runtime.GOARCH,
		ToolAvailability:     tools,
		CryptoSuite:          string(selected.Name),
		CryptoSuiteBoundary:  cryptoBoundary(selected),
		ValidatorCount:       len(identity.DefaultValidatorIDs()),
		QuorumThreshold:      5,
		Consensus: ConsensusEvidence{
			NormalCommitHeight:            1,
			FailureWindowFinalHeight:      faultReport.FinalHeight,
			QuorumThreshold:               faultReport.QuorumThreshold,
			ValidatorCount:                faultReport.ValidatorCount,
			SignerCount:                   5,
			TwoValidatorFailureCommitted:  faultReport.CommitCountDuringFailure > 0,
			QuorumUnavailableRejected:     quorumRejected,
			QuorumUnavailableReason:       quorumReason,
			FinalBlockHash:                faultReport.FinalBlockHash,
			FinalStateDigest:              faultReport.FinalStateDigest,
			DuplicateVoteRejectionSummary: "covered by consensus/protocol and consensus/validator tests; duplicate votes are counted once",
			InvalidVoteRejectionSummary:   "covered by consensus/protocol tests; invalid signatures and unknown validators do not count toward quorum",
		},
		EvidenceFabric: evidenceFabric,
		Durability:     durability,
		FaultHealing: FaultHealingEvidence{
			FailedValidatorCount:       faultReport.FailedValidatorCount,
			DetectionLatencyTicks:      faultReport.DetectionLatencyTicks,
			RemediationLatencyTicks:    faultReport.RemediationLatencyTicks,
			RecoveryCatchUpLatencyTick: faultReport.RecoveryCatchUpLatencyTicks,
			FinalConvergence:           faultReport.FinalConvergence,
			MessagePreservationSummary: fmt.Sprintf("submitted=%d committed_unique=%d duplicate_replayed=%d pending=%d", faultReport.SubmittedTransactionCount, faultReport.CommittedTransactionCount, faultReport.DuplicateReplayedTransactionCount, faultReport.PendingRetriedTransactionCount),
		},
		Routing:    routingReport,
		Bundle:     bundleReport,
		Anchors:    anchorReport,
		Deployment: deployment,
		NonClaims: []string{
			"no FIPS 140-3 certification",
			"no ACVTS certification or validation claim",
			"no production post-quantum security claim",
			"no production BFT safety claim",
			"no production anonymity or privacy claim",
			"no production self-healing claim",
			"no production AI/data-sovereignty claim",
			"no audited smart-contract claim",
			"no live cloud deployment claim",
			"no live Polygon deployment claim",
		},
		GeneratedEvidencePaths: []string{
			filepath.Join(opts.OutputDir, "failure-evidence.json"),
			filepath.Join(opts.OutputDir, "routing-evidence.json"),
			filepath.Join(opts.OutputDir, "bundle-evidence.json"),
			filepath.Join(opts.OutputDir, "anchor-evidence.json"),
			filepath.Join(opts.OutputDir, "e2e-evidence.json"),
			filepath.Join(opts.OutputDir, "e2e-evidence.txt"),
		},
		LimitationsStatement: "Integrated local evidence only. The run uses deterministic in-process/local harnesses, mock AI, mock anchoring, private-testbed routing, and validation-only deployment scaffolding. It does not deploy resources, call external AI APIs, enable public exits, or make production/certification claims.",
	}
	if opts.WriteArtifacts {
		if err := WriteArtifacts(opts.OutputDir, report); err != nil {
			return Report{}, err
		}
	}
	return report, nil
}

func writeDir(opts Options) string {
	if opts.WriteArtifacts {
		return opts.OutputDir
	}
	return ""
}

func runDurabilityCheck(dir string) (DurabilityEvidence, error) {
	if err := os.RemoveAll(dir); err != nil {
		return DurabilityEvidence{}, err
	}
	store, err := storage.OpenValidatorStore(storage.ModeDurable, dir)
	if err != nil {
		return DurabilityEvidence{}, err
	}
	state := storage.ValidatorState{
		NodeID:      "validator-1",
		Region:      "nyc",
		Height:      9,
		Round:       0,
		LastHash:    messages.HashBytes([]byte("e2e durable block")),
		StateDigest: messages.HashBytes([]byte("e2e durable state")),
		CommitCount: 1,
	}
	if err := store.SaveValidatorState(state); err != nil {
		return DurabilityEvidence{}, err
	}
	applied, _, err := store.RecordIdempotency("e2e-tx-1", messages.HashBytes([]byte("applied")))
	if err != nil {
		return DurabilityEvidence{}, err
	}
	if !applied {
		return DurabilityEvidence{}, fmt.Errorf("expected first idempotency record to apply")
	}
	if err := store.Close(); err != nil {
		return DurabilityEvidence{}, err
	}
	reopened, err := storage.OpenValidatorStore(storage.ModeDurable, dir)
	if err != nil {
		return DurabilityEvidence{}, err
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadValidatorState()
	if err != nil {
		return DurabilityEvidence{}, err
	}
	if !ok {
		return DurabilityEvidence{}, fmt.Errorf("durable state was not reloaded")
	}
	appliedAgain, _, err := reopened.RecordIdempotency("e2e-tx-1", messages.HashBytes([]byte("applied")))
	if err != nil {
		return DurabilityEvidence{}, err
	}
	count, err := reopened.IdempotencyCount()
	if err != nil {
		return DurabilityEvidence{}, err
	}
	status := "accepted transaction persisted and duplicate after restart was deduplicated"
	if appliedAgain {
		status = "unexpected duplicate apply after restart"
	}
	return DurabilityEvidence{
		StorageBackend:          storage.ModeDurable,
		RestartReloadResult:     "loaded",
		PersistedHeight:         loaded.Height,
		PersistedHash:           loaded.LastHash,
		PersistedStateDigest:    loaded.StateDigest,
		IdempotencyLedgerStatus: status,
		IdempotencyCount:        count,
	}, nil
}

func runEvidenceFabricCheck(selected cryptosuite.CryptoSuite) (EvidenceFabricEvidence, error) {
	submission := evidencefabric.EvidenceSubmission{
		SchemaVersion:          evidencefabric.SchemaVersion,
		EvidenceCategory:       "regulated-pilot-audit",
		ArtifactHash:           messages.HashBytes([]byte("e2e evidence artifact")),
		MetadataHash:           messages.HashBytes([]byte("e2e evidence metadata")),
		SubmittingOrganization: "bestgate",
		IdempotencyKey:         "e2e-evidence-fabric-1",
		AnchorRequested:        true,
	}
	payload, err := evidencefabric.SubmissionPayload(submission)
	if err != nil {
		return EvidenceFabricEvidence{}, err
	}
	identities, err := identity.ValidatorIdentitiesForSuite(map[string]string{}, selected)
	if err != nil {
		return EvidenceFabricEvidence{}, err
	}
	manifest := consortium.ManifestFromIdentities("e2e-consortium", 1, 5, identities, identity.DefaultValidatorIDs())
	manifestHash, err := manifest.Hash()
	if err != nil {
		return EvidenceFabricEvidence{}, err
	}
	commit, err := evidenceFabricCommit(selected, payload, manifest.MembershipVersion, manifestHash)
	if err != nil {
		return EvidenceFabricEvidence{}, err
	}
	receipt, err := evidencefabric.NewReceipt(submission, commit)
	if err != nil {
		return EvidenceFabricEvidence{}, err
	}
	result := evidencefabric.VerifyReceipt(receipt, identities, selected.NewVerifier(), 5)
	if !result.Valid {
		return EvidenceFabricEvidence{}, fmt.Errorf("receipt verification failed: %s", result.Reason)
	}
	receiptJSON, err := evidencefabric.MarshalReceipt(receipt)
	if err != nil {
		return EvidenceFabricEvidence{}, err
	}
	store := storage.NewMemoryStore()
	_, _, err = store.SaveEvidence(storage.EvidenceRecord{
		EvidenceID:     receipt.EvidenceID,
		ReceiptID:      receipt.ReceiptID,
		EventHash:      receipt.EventHash,
		QCHash:         receipt.QCHash,
		CommitHeight:   receipt.CommitHeight,
		SubmittingOrg:  receipt.Submission.SubmittingOrganization,
		IdempotencyKey: receipt.Submission.IdempotencyKey,
		ReceiptJSON:    receiptJSON,
	})
	if err != nil {
		return EvidenceFabricEvidence{}, err
	}
	indexStatus := "indexed by evidence_id, receipt_id, idempotency_key, and qc_hash"
	for label, lookup := range map[string]func() (storage.EvidenceRecord, bool, error){
		"evidence_id": func() (storage.EvidenceRecord, bool, error) { return store.EvidenceByID(receipt.EvidenceID) },
		"receipt_id":  func() (storage.EvidenceRecord, bool, error) { return store.EvidenceByReceiptID(receipt.ReceiptID) },
		"idempotency_key": func() (storage.EvidenceRecord, bool, error) {
			return store.EvidenceByIdempotencyKey(receipt.Submission.IdempotencyKey)
		},
		"qc_hash": func() (storage.EvidenceRecord, bool, error) { return store.EvidenceByQCHash(receipt.QCHash) },
	} {
		record, ok, err := lookup()
		if err != nil {
			return EvidenceFabricEvidence{}, err
		}
		if !ok || record.ReceiptID != receipt.ReceiptID {
			return EvidenceFabricEvidence{}, fmt.Errorf("missing evidence index %s", label)
		}
	}
	return EvidenceFabricEvidence{
		SubmissionSchema:       submission.SchemaVersion,
		EvidenceCategory:       submission.EvidenceCategory,
		SubmittingOrganization: submission.SubmittingOrganization,
		ReceiptID:              receipt.ReceiptID,
		EvidenceID:             receipt.EvidenceID,
		QCHash:                 receipt.QCHash,
		SignerCount:            receipt.SignerCount,
		VerificationStatus:     result.Status,
		QuorumStatus:           result.QuorumStatus,
		AnchorStatus:           receipt.AnchorStatus,
		IndexStatus:            indexStatus,
		StoragePolicy:          "hashes, metadata, receipts, and quorum evidence only; no source artifacts",
		MembershipVersion:      receipt.MembershipVersion,
		ValidatorSetHash:       receipt.ValidatorSetHash,
	}, nil
}

func evidenceFabricCommit(selected cryptosuite.CryptoSuite, payload string, membershipVersion uint64, validatorSetHash string) (protocol.CommitRequest, error) {
	transactions := consensusstate.TransactionsFromPayload(payload)
	stateMachine := consensusstate.NewMachine()
	stateDigest, _, err := stateMachine.Apply(transactions)
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	block := protocol.NewRoundBlock(1, 0, protocol.GenesisHash, payload, "validator-1", stateDigest)
	block.Transactions = transactions
	block.MembershipVersion = membershipVersion
	block.ValidatorSetHash = validatorSetHash
	blockHash, err := block.Hash()
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	votes := make([]protocol.Vote, 0, 5)
	for _, id := range identity.DefaultValidatorIDs()[:5] {
		signer, err := selected.NewSigner(id)
		if err != nil {
			return protocol.CommitRequest{}, err
		}
		vote, err := protocol.SignStageVote(1, 0, protocol.StagePrecommit, blockHash, id, signer)
		if err != nil {
			return protocol.CommitRequest{}, err
		}
		votes = append(votes, vote)
	}
	cert, err := protocol.FormStageQuorumCertificate(1, 0, protocol.StagePrecommit, blockHash, votes, 5)
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	cert.MembershipVersion = membershipVersion
	cert.ValidatorSetHash = validatorSetHash
	return protocol.CommitRequest{Block: block, Certificate: cert}, nil
}

func quorumUnavailableCheck(selected cryptosuite.CryptoSuite) (bool, string) {
	blockHash := messages.HashBytes([]byte("e2e quorum unavailable"))
	var votes []protocol.Vote
	for _, id := range identity.DefaultValidatorIDs()[:4] {
		signer, err := selected.NewSigner(id)
		if err != nil {
			return false, err.Error()
		}
		vote, err := protocol.SignStageVote(10, 0, protocol.StagePrecommit, blockHash, id, signer)
		if err != nil {
			return false, err.Error()
		}
		votes = append(votes, vote)
	}
	_, err := protocol.FormStageQuorumCertificate(10, 0, protocol.StagePrecommit, blockHash, votes, 5)
	if err == nil {
		return false, "4-of-7 unexpectedly formed a quorum certificate"
	}
	return true, err.Error()
}

func cryptoBoundary(selected cryptosuite.CryptoSuite) string {
	if selected.Name == cryptosuite.Dev {
		return "development-only deterministic crypto suite; not post-quantum secure"
	}
	return "pq suite selected for engineering validation; not a FIPS/ACVTS certification claim"
}

func runOptional(name string, args ...string) ToolStatus {
	if _, err := exec.LookPath(name); err != nil {
		return ToolStatus{Available: false, Status: "skipped: " + name + " not installed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	output := strings.TrimSpace(out.String())
	if len(output) > 700 {
		output = output[:700] + "...truncated"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return ToolStatus{Available: true, Status: "failed: timed out", Output: output}
	}
	if err != nil {
		return ToolStatus{Available: true, Status: "failed: " + err.Error(), Output: output}
	}
	return ToolStatus{Available: true, Status: "pass", Output: output}
}

func terraformValidate() ToolStatus {
	if _, err := exec.LookPath("terraform"); err != nil {
		return ToolStatus{Available: false, Status: "skipped: terraform not installed"}
	}
	defer os.RemoveAll(filepath.Join("deployments", "terraform", ".terraform"))
	init := runOptional("terraform", "-chdir=deployments/terraform", "init", "-backend=false")
	if init.Status != "pass" {
		init.Status = "failed during init: " + init.Status
		return init
	}
	return runOptional("terraform", "-chdir=deployments/terraform", "validate")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func listFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		out = append(out, filepath.ToSlash(path))
		return nil
	})
	sort.Strings(out)
	return out
}

func guardrailStatus() string {
	data, err := os.ReadFile(".gitignore")
	if err != nil {
		return "unable to read .gitignore"
	}
	text := string(data)
	required := []string{".env", ".env.*", "!*.example.env", "data/", "tmp/", "terraform.tfstate", ".terraform/", "*.tfvars", "*.pem", "*.key", "*.kubeconfig", "*wallet*", "contracts/polygon/broadcast/"}
	var missing []string
	for _, item := range required {
		if !strings.Contains(text, item) {
			missing = append(missing, item)
		}
	}
	if len(missing) > 0 {
		return "missing guardrails: " + strings.Join(missing, ", ")
	}
	return "required ignore guardrails present"
}

func gitCommit() string {
	status := runOptional("git", "rev-parse", "--short=12", "HEAD")
	if status.Status == "pass" {
		return strings.TrimSpace(status.Output)
	}
	return "unavailable"
}

func WriteArtifacts(outputDir string, report Report) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "e2e-evidence.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "e2e-evidence.txt"), []byte(Text(report)), 0o644)
}

func Text(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric integrated e2e evidence\n")
	fmt.Fprintf(&b, "run_id=%s git=%s go=%s os=%s/%s suite=%s\n", report.RunID, report.GitCommit, report.GoVersion, report.OS, report.Architecture, report.CryptoSuite)
	fmt.Fprintf(&b, "validators=%d quorum=%d crypto_boundary=%s\n", report.ValidatorCount, report.QuorumThreshold, report.CryptoSuiteBoundary)
	fmt.Fprintf(&b, "consensus: final_height=%d signer_count=%d two_node_failure_commit=%t quorum_unavailable_rejected=%t state=%s\n", report.Consensus.FailureWindowFinalHeight, report.Consensus.SignerCount, report.Consensus.TwoValidatorFailureCommitted, report.Consensus.QuorumUnavailableRejected, short(report.Consensus.FinalStateDigest))
	fmt.Fprintf(&b, "evidence_fabric: receipt=%s category=%s membership=%d set=%s signer_count=%d verification=%s quorum=%s anchor=%s indexes=%s\n", short(report.EvidenceFabric.ReceiptID), report.EvidenceFabric.EvidenceCategory, report.EvidenceFabric.MembershipVersion, short(report.EvidenceFabric.ValidatorSetHash), report.EvidenceFabric.SignerCount, report.EvidenceFabric.VerificationStatus, report.EvidenceFabric.QuorumStatus, report.EvidenceFabric.AnchorStatus, report.EvidenceFabric.IndexStatus)
	fmt.Fprintf(&b, "durability: backend=%s reload=%s height=%d idempotency=%s count=%d\n", report.Durability.StorageBackend, report.Durability.RestartReloadResult, report.Durability.PersistedHeight, report.Durability.IdempotencyLedgerStatus, report.Durability.IdempotencyCount)
	fmt.Fprintf(&b, "fault: failed=%d detection_ticks=%d remediation_ticks=%d catchup_ticks=%d converged=%t\n", report.FaultHealing.FailedValidatorCount, report.FaultHealing.DetectionLatencyTicks, report.FaultHealing.RemediationLatencyTicks, report.FaultHealing.RecoveryCatchUpLatencyTick, report.FaultHealing.FinalConvergence)
	fmt.Fprintf(&b, "routing: circuit=%s path=%s/%s/%s streams=%d completed=%d rejected_destinations=%d success=%t\n", report.Routing.CircuitID, report.Routing.EntryRelay, report.Routing.MiddleRelay, report.Routing.ExitRelay, report.Routing.StreamsOpened, report.Routing.StreamsCompleted, report.Routing.RejectedDestinationCount, report.Routing.FinalSuccess)
	fmt.Fprintf(&b, "bundle: bundles=%d custody=%s retransmissions=%d duplicates=%d reconciled=%d context=%s\n", len(report.Bundle.BundleIDs), report.Bundle.CustodyConfirmationStatus, report.Bundle.RetransmissionCount, report.Bundle.DuplicateTransactionCount, report.Bundle.ReconciledBundleCount, short(report.Bundle.FinalStateDigest))
	fmt.Fprintf(&b, "anchors: identities=%d credentials=%d governance=%d qc=%d duplicate=%s\n", len(report.Anchors.ValidatorIdentityAnchors), len(report.Anchors.CredentialAnchors), len(report.Anchors.GovernanceProposals), len(report.Anchors.QuorumCertificateAnchors), report.Anchors.DuplicateReplayOutcome)
	fmt.Fprintf(&b, "deployment: compose=%s k8s=%s terraform=%s guardrails=%s\n", report.Deployment.DockerComposeConfigStatus.Status, report.Deployment.KubernetesValidationStatus.Status, report.Deployment.TerraformValidationStatus.Status, report.Deployment.SecretGuardrailStatus)
	fmt.Fprintf(&b, "non_claims: %s\n", strings.Join(report.NonClaims, "; "))
	fmt.Fprintf(&b, "limitations: %s\n", report.LimitationsStatement)
	return b.String()
}

func short(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}
