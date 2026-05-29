package fault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/health"
	"github.com/keithwegner/pq-fabric/consensus/protocol"
	"github.com/keithwegner/pq-fabric/consensus/validator"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/storage"
)

type Options struct {
	DataRoot         string
	StorageMode      string
	ResetData        bool
	WriteArtifacts   bool
	EvidenceJSONPath string
	EvidenceTextPath string
	RequestTimeout   time.Duration
	ProposalTimeout  time.Duration
	VoteTimeout      time.Duration
	DetectorConfig   health.DetectorConfig
}

type ScenarioReport struct {
	GeneratedAtUnixMilli              int64                    `json:"generated_at_unix_milli"`
	TimingMode                        string                   `json:"timing_mode"`
	ValidatorCount                    int                      `json:"validator_count"`
	FailedValidatorCount              int                      `json:"failed_validator_count"`
	QuorumThreshold                   int                      `json:"quorum_threshold"`
	CommitCountDuringFailure          int                      `json:"commit_count_during_failure"`
	SubmittedTransactionCount         int                      `json:"submitted_transaction_count"`
	CommittedTransactionCount         int                      `json:"committed_transaction_count"`
	DuplicateReplayedTransactionCount int                      `json:"duplicate_replayed_transaction_count"`
	PendingRetriedTransactionCount    int                      `json:"pending_retried_transaction_count"`
	DetectionLatencyTicks             uint64                   `json:"detection_latency_ticks"`
	RemediationLatencyTicks           uint64                   `json:"remediation_latency_ticks"`
	RecoveryCatchUpLatencyTicks       uint64                   `json:"recovery_catch_up_latency_ticks"`
	TotalScenarioDurationTicks        uint64                   `json:"total_scenario_duration_ticks"`
	FinalHeight                       uint64                   `json:"final_height"`
	FinalBlockHash                    string                   `json:"final_block_hash"`
	FinalStateDigest                  string                   `json:"final_state_digest"`
	FinalConvergence                  bool                     `json:"final_convergence"`
	Events                            []health.EvidenceRecord  `json:"events"`
	ValidatorHealth                   []health.ValidatorHealth `json:"validator_health"`
	EvidenceJSONPath                  string                   `json:"evidence_json_path,omitempty"`
	EvidenceTextPath                  string                   `json:"evidence_text_path,omitempty"`
}

type cluster struct {
	opts   Options
	ctx    context.Context
	nodes  map[string]*validator.Node
	cfgs   map[string]validator.Config
	urls   map[string]string
	failed map[string]bool
}

func RunScenario(ctx context.Context, opts Options) (ScenarioReport, error) {
	opts = normalizeOptions(opts)
	if opts.ResetData && opts.DataRoot != "" {
		if err := os.RemoveAll(opts.DataRoot); err != nil {
			return ScenarioReport{}, err
		}
	}
	cluster, err := newCluster(ctx, opts)
	if err != nil {
		return ScenarioReport{}, err
	}
	defer cluster.Close()
	if err := cluster.StartAll(); err != nil {
		return ScenarioReport{}, err
	}

	detector := health.NewDetector(opts.DetectorConfig)
	var events []health.EvidenceRecord
	tick := uint64(1)
	initialCommit, err := cluster.Propose(ctx, "fault-demo: normal baseline commit")
	if err != nil {
		return ScenarioReport{}, err
	}
	if initialCommit.Block.Height != 1 {
		return ScenarioReport{}, fmt.Errorf("expected initial height 1, got %d", initialCommit.Block.Height)
	}
	reference, err := cluster.ReferenceHeartbeat(tick)
	if err != nil {
		return ScenarioReport{}, err
	}
	events = append(events, cluster.ObserveLive(detector, tick, reference)...)

	failedValidators := []string{"validator-6", "validator-7"}
	failureInjectedTick := tick + 1
	for _, id := range failedValidators {
		if err := cluster.Stop(id); err != nil {
			return ScenarioReport{}, err
		}
	}

	tick = failureInjectedTick
	reference, err = cluster.ReferenceHeartbeat(tick)
	if err != nil {
		return ScenarioReport{}, err
	}
	events = append(events, cluster.ObserveLive(detector, tick, reference)...)
	events = append(events, detector.Evaluate(tick, 0, reference)...)

	tick++
	reference, err = cluster.ReferenceHeartbeat(tick)
	if err != nil {
		return ScenarioReport{}, err
	}
	events = append(events, cluster.ObserveLive(detector, tick, reference)...)
	events = append(events, detector.Evaluate(tick, 0, reference)...)

	failurePayloads := []string{
		"fault-demo: accepted message A during two-validator failure",
		"fault-demo: accepted message B during two-validator failure",
		"fault-demo: accepted message A during two-validator failure",
	}
	commitsDuringFailure := make([]protocol.CommitRequest, 0, len(failurePayloads))
	for _, payload := range failurePayloads {
		commit, err := cluster.Propose(ctx, payload)
		if err != nil {
			return ScenarioReport{}, err
		}
		commitsDuringFailure = append(commitsDuringFailure, commit)
	}
	submitted, committed, duplicates, pending := transactionMetrics(commitsDuringFailure)

	tick++
	reference, err = cluster.ReferenceHeartbeat(tick)
	if err != nil {
		return ScenarioReport{}, err
	}
	events = append(events, cluster.ObserveLive(detector, tick, reference)...)

	remediationStartTick := tick + 1
	for _, id := range failedValidators {
		events = append(events, detector.MarkRecovering(id, remediationStartTick, 0, "local harness restart requested"))
	}

	for _, id := range failedValidators {
		if err := cluster.Restart(id); err != nil {
			return ScenarioReport{}, err
		}
	}
	tick = remediationStartTick + 1
	reference, err = cluster.ReferenceHeartbeat(tick)
	if err != nil {
		return ScenarioReport{}, err
	}
	events = append(events, cluster.ObserveLive(detector, tick, reference)...)

	catchUpStartTick := tick
	for _, id := range failedValidators {
		if err := cluster.nodes[id].CatchUpFromPeers(ctx); err != nil {
			return ScenarioReport{}, fmt.Errorf("%s catch-up: %w", id, err)
		}
	}
	tick++
	reference, err = cluster.ReferenceHeartbeat(tick)
	if err != nil {
		return ScenarioReport{}, err
	}
	events = append(events, cluster.ObserveLive(detector, tick, reference)...)
	for _, id := range failedValidators {
		events = append(events, detector.MarkRemediationCompleted(id, tick, 0, "catch-up converged to latest committed state"))
	}

	converged, height, blockHash, stateDigest := cluster.Convergence()
	events = append(events, health.EvidenceRecord{
		EventType:      health.EventConvergenceVerified,
		ObservedStatus: health.StatusRecovered,
		Height:         height,
		BlockHash:      blockHash,
		StateDigest:    stateDigest,
		LogicalTick:    tick,
		Reason:         fmt.Sprintf("all validators converged=%t", converged),
	})
	events = append(events, health.EvidenceRecord{
		EventType:   health.EventMessagePreservation,
		Height:      height,
		BlockHash:   blockHash,
		StateDigest: stateDigest,
		LogicalTick: tick,
		Reason:      fmt.Sprintf("submitted=%d committed_unique=%d duplicate_replayed=%d pending=%d", submitted, committed, duplicates, pending),
	})

	report := ScenarioReport{
		GeneratedAtUnixMilli:              time.Now().UnixMilli(),
		TimingMode:                        "logical_ticks",
		ValidatorCount:                    len(identity.DefaultValidatorIDs()),
		FailedValidatorCount:              len(failedValidators),
		QuorumThreshold:                   5,
		CommitCountDuringFailure:          len(commitsDuringFailure),
		SubmittedTransactionCount:         submitted,
		CommittedTransactionCount:         committed,
		DuplicateReplayedTransactionCount: duplicates,
		PendingRetriedTransactionCount:    pending,
		DetectionLatencyTicks:             latencyFromEvent(events, health.EventFailureDetected, failureInjectedTick),
		RemediationLatencyTicks:           tick - remediationStartTick,
		RecoveryCatchUpLatencyTicks:       tick - catchUpStartTick,
		TotalScenarioDurationTicks:        tick,
		FinalHeight:                       height,
		FinalBlockHash:                    blockHash,
		FinalStateDigest:                  stateDigest,
		FinalConvergence:                  converged,
		Events:                            events,
		ValidatorHealth:                   detector.States(),
	}
	if opts.WriteArtifacts {
		if err := writeArtifacts(opts, report); err != nil {
			return ScenarioReport{}, err
		}
		report.EvidenceJSONPath = opts.EvidenceJSONPath
		report.EvidenceTextPath = opts.EvidenceTextPath
	}
	return report, nil
}

func normalizeOptions(opts Options) Options {
	if opts.StorageMode == "" {
		opts.StorageMode = storage.ModeDurable
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 250 * time.Millisecond
	}
	if opts.ProposalTimeout <= 0 {
		opts.ProposalTimeout = 150 * time.Millisecond
	}
	if opts.VoteTimeout <= 0 {
		opts.VoteTimeout = 250 * time.Millisecond
	}
	if opts.DetectorConfig.SuspectAfterTicks == 0 {
		opts.DetectorConfig.SuspectAfterTicks = 1
	}
	if opts.DetectorConfig.FailAfterTicks == 0 {
		opts.DetectorConfig.FailAfterTicks = 2
	}
	if opts.EvidenceJSONPath == "" {
		opts.EvidenceJSONPath = filepath.Join("tmp", "failure-evidence.json")
	}
	if opts.EvidenceTextPath == "" {
		opts.EvidenceTextPath = filepath.Join("tmp", "failure-evidence.txt")
	}
	return opts
}

func newCluster(ctx context.Context, opts Options) (*cluster, error) {
	ids := identity.DefaultValidatorIDs()
	urls, listenAddrs, err := allocateLocalURLs(ids)
	if err != nil {
		return nil, err
	}
	if opts.StorageMode == storage.ModeDurable && opts.DataRoot == "" {
		return nil, errors.New("fault scenario durable storage requires a data root")
	}
	cfgs := make(map[string]validator.Config, len(ids))
	nodes := make(map[string]*validator.Node, len(ids))
	for _, id := range ids {
		cfg := validator.Config{
			ID:                id,
			Region:            identity.DefaultRegionFor(id),
			ListenAddr:        listenAddrs[id],
			PublicURL:         urls[id],
			PeerURLs:          copyURLs(urls),
			Threshold:         5,
			RequestTimeout:    opts.RequestTimeout,
			ProposalTimeout:   opts.ProposalTimeout,
			VoteTimeout:       opts.VoteTimeout,
			MaxRounds:         uint64(len(ids)),
			EnableHealthProbe: false,
			StorageMode:       opts.StorageMode,
		}
		if opts.StorageMode == storage.ModeDurable {
			cfg.DataDir = filepath.Join(opts.DataRoot, id)
		}
		node, err := validator.NewNode(cfg)
		if err != nil {
			return nil, err
		}
		cfgs[id] = cfg
		nodes[id] = node
	}
	return &cluster{opts: opts, ctx: ctx, nodes: nodes, cfgs: cfgs, urls: urls, failed: make(map[string]bool)}, nil
}

func (c *cluster) StartAll() error {
	for _, id := range identity.DefaultValidatorIDs() {
		if err := c.nodes[id].Start(c.ctx); err != nil {
			return err
		}
	}
	for _, id := range identity.DefaultValidatorIDs() {
		if err := waitForHTTP(c.urls[id] + "/health"); err != nil {
			return err
		}
	}
	return nil
}

func (c *cluster) Stop(id string) error {
	node, ok := c.nodes[id]
	if !ok {
		return fmt.Errorf("unknown validator %s", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := node.Stop(ctx); err != nil {
		return err
	}
	if err := node.Close(); err != nil {
		return err
	}
	c.failed[id] = true
	return nil
}

func (c *cluster) Restart(id string) error {
	cfg, ok := c.cfgs[id]
	if !ok {
		return fmt.Errorf("unknown validator %s", id)
	}
	node, err := validator.NewNode(cfg)
	if err != nil {
		return err
	}
	if err := node.Start(c.ctx); err != nil {
		_ = node.Close()
		return err
	}
	if err := waitForHTTP(c.urls[id] + "/health"); err != nil {
		_ = node.Close()
		return err
	}
	c.nodes[id] = node
	delete(c.failed, id)
	return nil
}

func (c *cluster) Close() {
	for _, id := range identity.DefaultValidatorIDs() {
		node := c.nodes[id]
		if node == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = node.Stop(ctx)
		cancel()
		_ = node.Close()
	}
}

func (c *cluster) Propose(ctx context.Context, payload string) (protocol.CommitRequest, error) {
	for _, id := range identity.DefaultValidatorIDs() {
		node := c.nodes[id]
		if node == nil || c.failed[id] || !node.Snapshot().Running {
			continue
		}
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		commit, err := node.Propose(reqCtx, payload)
		cancel()
		return commit, err
	}
	return protocol.CommitRequest{}, errors.New("no live validator available to propose")
}

func (c *cluster) ObserveLive(detector *health.Detector, tick uint64, reference health.HeartbeatRecord) []health.EvidenceRecord {
	var events []health.EvidenceRecord
	for _, id := range identity.DefaultValidatorIDs() {
		node := c.nodes[id]
		if node == nil || c.failed[id] || !node.Snapshot().Running {
			continue
		}
		hb := health.HeartbeatFromSnapshot(node.Snapshot(), tick, health.StatusHealthy)
		events = append(events, detector.Observe(hb, &reference)...)
	}
	return events
}

func (c *cluster) ReferenceHeartbeat(tick uint64) (health.HeartbeatRecord, error) {
	var best protocol.StateSnapshot
	found := false
	for _, id := range identity.DefaultValidatorIDs() {
		node := c.nodes[id]
		if node == nil || c.failed[id] {
			continue
		}
		snapshot := node.Snapshot()
		if !snapshot.Running {
			continue
		}
		if !found || snapshot.Height > best.Height || (snapshot.Height == best.Height && snapshot.NodeID < best.NodeID) {
			best = snapshot
			found = true
		}
	}
	if !found {
		return health.HeartbeatRecord{}, errors.New("no live validator snapshot available")
	}
	return health.HeartbeatFromSnapshot(best, tick, health.StatusHealthy), nil
}

func (c *cluster) Convergence() (bool, uint64, string, string) {
	var height uint64
	var blockHash string
	var stateDigest string
	converged := true
	for i, id := range identity.DefaultValidatorIDs() {
		node := c.nodes[id]
		if node == nil {
			return false, height, blockHash, stateDigest
		}
		snapshot := node.Snapshot()
		if i == 0 {
			height = snapshot.Height
			blockHash = snapshot.LastHash
			stateDigest = snapshot.StateDigest
		}
		if !snapshot.Running || snapshot.Height != height || snapshot.LastHash != blockHash || snapshot.StateDigest != stateDigest {
			converged = false
		}
	}
	return converged, height, blockHash, stateDigest
}

func allocateLocalURLs(ids []string) (map[string]string, map[string]string, error) {
	listeners := make([]net.Listener, 0, len(ids))
	urls := make(map[string]string, len(ids))
	addrs := make(map[string]string, len(ids))
	for _, id := range ids {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			for _, l := range listeners {
				_ = l.Close()
			}
			return nil, nil, err
		}
		listeners = append(listeners, listener)
		addr := listener.Addr().String()
		addrs[id] = addr
		urls[id] = "http://" + addr
	}
	for _, listener := range listeners {
		if err := listener.Close(); err != nil {
			return nil, nil, err
		}
	}
	return urls, addrs, nil
}

func copyURLs(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for id, url := range in {
		out[id] = strings.TrimRight(url, "/")
	}
	return out
}

func waitForHTTP(url string) error {
	client := http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("%s returned %s", url, resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s: %w", url, lastErr)
}

func transactionMetrics(commits []protocol.CommitRequest) (submitted, committed, duplicates, pending int) {
	seen := make(map[string]struct{})
	for _, commit := range commits {
		for _, tx := range commit.Block.Transactions {
			submitted++
			if _, ok := seen[tx.ID]; ok {
				duplicates++
				continue
			}
			seen[tx.ID] = struct{}{}
			committed++
		}
	}
	return submitted, committed, duplicates, 0
}

func latencyFromEvent(events []health.EvidenceRecord, eventType string, start uint64) uint64 {
	for _, event := range events {
		if event.EventType == eventType && event.LogicalTick >= start {
			return event.LogicalTick - start
		}
	}
	return 0
}

func writeArtifacts(opts Options, report ScenarioReport) error {
	if err := os.MkdirAll(filepath.Dir(opts.EvidenceJSONPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.EvidenceTextPath), 0o755); err != nil {
		return err
	}
	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	jsonData = append(jsonData, '\n')
	if err := os.WriteFile(opts.EvidenceJSONPath, jsonData, 0o644); err != nil {
		return err
	}
	text := renderTextReport(report)
	return os.WriteFile(opts.EvidenceTextPath, []byte(text), 0o644)
}

func renderTextReport(report ScenarioReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric controlled failure evidence\n")
	fmt.Fprintf(&b, "timing_mode=%s validator_count=%d quorum=%d failed=%d\n", report.TimingMode, report.ValidatorCount, report.QuorumThreshold, report.FailedValidatorCount)
	fmt.Fprintf(&b, "commit_count_during_failure=%d final_height=%d convergence=%t\n", report.CommitCountDuringFailure, report.FinalHeight, report.FinalConvergence)
	fmt.Fprintf(&b, "submitted=%d committed_unique=%d duplicate_replayed=%d pending=%d\n", report.SubmittedTransactionCount, report.CommittedTransactionCount, report.DuplicateReplayedTransactionCount, report.PendingRetriedTransactionCount)
	fmt.Fprintf(&b, "detection_latency_ticks=%d remediation_latency_ticks=%d recovery_catch_up_latency_ticks=%d total_ticks=%d\n", report.DetectionLatencyTicks, report.RemediationLatencyTicks, report.RecoveryCatchUpLatencyTicks, report.TotalScenarioDurationTicks)
	fmt.Fprintf(&b, "final_hash=%s final_state_digest=%s\n", short(report.FinalBlockHash), short(report.FinalStateDigest))
	fmt.Fprintf(&b, "\nevents:\n")
	events := append([]health.EvidenceRecord(nil), report.Events...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].LogicalTick == events[j].LogicalTick {
			return events[i].EventType < events[j].EventType
		}
		return events[i].LogicalTick < events[j].LogicalTick
	})
	for _, event := range events {
		fmt.Fprintf(&b, "tick=%d event=%s validator=%s status=%s reason=%s\n", event.LogicalTick, event.EventType, event.ValidatorID, event.ObservedStatus, event.Reason)
	}
	return b.String()
}

func short(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
