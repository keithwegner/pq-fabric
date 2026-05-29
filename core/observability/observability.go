package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
	"github.com/keithwegner/pq-fabric/core/storage"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

type Config struct {
	NodeID             string
	ProductionMode     bool
	LogFormat          string
	OTELEnabled        bool
	OTELServiceName    string
	OTELExporterURL    string
	OTELHeaders        string
	OTELAllowInsecure  bool
	LoggerOutput       io.Writer
	UseSynchronousOTEL bool
}

type Runtime struct {
	Logger         *slog.Logger
	Tracer         trace.Tracer
	Metrics        *Metrics
	tracerProvider *sdktrace.TracerProvider
}

func NewRuntime(ctx context.Context, cfg Config) (*Runtime, error) {
	logger, err := NewLogger(cfg)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	metrics := NewMetrics()
	tracer := otel.Tracer("github.com/keithwegner/pq-fabric")
	var provider *sdktrace.TracerProvider
	if cfg.OTELEnabled {
		if strings.TrimSpace(cfg.OTELExporterURL) == "" {
			return nil, errors.New("OpenTelemetry is enabled but PQFABRIC_OTEL_EXPORTER_OTLP_ENDPOINT is empty")
		}
		headers, err := ParseHeaders(cfg.OTELHeaders)
		if err != nil {
			return nil, err
		}
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpointURL(strings.TrimSpace(cfg.OTELExporterURL)),
			otlptracehttp.WithHeaders(headers),
		}
		if cfg.OTELAllowInsecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err := otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
		if err != nil {
			return nil, fmt.Errorf("configure OpenTelemetry exporter: %w", err)
		}
		serviceName := strings.TrimSpace(cfg.OTELServiceName)
		if serviceName == "" {
			serviceName = "pq-fabric-validator"
		}
		resource, err := sdkresource.New(ctx,
			sdkresource.WithAttributes(
				attribute.String("service.name", serviceName),
				attribute.String("pqfabric.node_id", strings.TrimSpace(cfg.NodeID)),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("configure OpenTelemetry resource: %w", err)
		}
		options := []sdktrace.TracerProviderOption{sdktrace.WithResource(resource)}
		if cfg.UseSynchronousOTEL {
			options = append(options, sdktrace.WithSyncer(exporter))
		} else {
			options = append(options, sdktrace.WithBatcher(exporter))
		}
		provider = sdktrace.NewTracerProvider(options...)
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
		tracer = provider.Tracer("github.com/keithwegner/pq-fabric")
	}
	return &Runtime{Logger: logger, Tracer: tracer, Metrics: metrics, tracerProvider: provider}, nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || r.tracerProvider == nil {
		return nil
	}
	return r.tracerProvider.Shutdown(ctx)
}

func (r *Runtime) Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if r == nil || r.Tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return r.Tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func (r *Runtime) EndSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

func (r *Runtime) Inject(req *http.Request) {
	if r == nil || req == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(req.Context(), propagation.HeaderCarrier(req.Header))
}

func NewLogger(cfg Config) (*slog.Logger, error) {
	format := strings.ToLower(strings.TrimSpace(cfg.LogFormat))
	if format == "" {
		if cfg.ProductionMode {
			format = LogFormatJSON
		} else {
			format = LogFormatText
		}
	}
	output := cfg.LoggerOutput
	if output == nil {
		output = os.Stderr
	}
	switch format {
	case LogFormatJSON:
		return slog.New(slog.NewJSONHandler(output, nil)), nil
	case LogFormatText:
		return slog.New(slog.NewTextHandler(output, nil)), nil
	default:
		return nil, fmt.Errorf("unsupported PQFABRIC_LOG_FORMAT %q", cfg.LogFormat)
	}
}

func RedactValue(key, value string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	if strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.Contains(k, "key_file") ||
		strings.Contains(k, "private") || strings.Contains(k, "authorization") || strings.Contains(k, "password") {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return "[redacted]"
	}
	return value
}

func ParseHeaders(raw string) (map[string]string, error) {
	headers := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return headers, nil
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid OTEL header %q; expected key=value", part)
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return headers, nil
}

type Metrics struct {
	mu                         sync.Mutex
	apiRequests                map[string]int
	apiRequestDurationMillis   map[string]int64
	anchorStatuses             map[string]int
	verificationResults        map[string]int
	evidenceSubmissions        int
	duplicateEvidence          int
	consensusProposals         int
	consensusProposalFailures  int
	consensusCommits           int
	commitLatencyMillisTotal   int64
	lastCommitLatencyMillis    int64
	lastQuorumSignerCount      int
	storageErrors              int
	signerErrors               int
	invalidSignatures          int
	peerProbeFailures          int
	peerProbeSuccesses         int
	lastQuorumAvailable        bool
	lastQuorumAvailableUpdated int64
}

type MetricsSnapshot struct {
	APIRequests                map[string]int   `json:"api_requests"`
	APIRequestDurationMillis   map[string]int64 `json:"api_request_duration_millis_total"`
	AnchorStatuses             map[string]int   `json:"anchor_statuses"`
	VerificationResults        map[string]int   `json:"verification_results"`
	EvidenceSubmissions        int              `json:"evidence_submissions_total"`
	DuplicateEvidence          int              `json:"duplicate_evidence_submissions_total"`
	ConsensusProposals         int              `json:"consensus_proposals_total"`
	ConsensusProposalFailures  int              `json:"consensus_proposal_failures_total"`
	ConsensusCommits           int              `json:"consensus_commits_total"`
	CommitLatencyMillisTotal   int64            `json:"commit_latency_millis_total"`
	LastCommitLatencyMillis    int64            `json:"last_commit_latency_millis"`
	LastQuorumSignerCount      int              `json:"last_quorum_signer_count"`
	StorageErrors              int              `json:"storage_errors_total"`
	SignerErrors               int              `json:"signer_errors_total"`
	InvalidSignatures          int              `json:"invalid_signatures_total"`
	PeerProbeFailures          int              `json:"peer_probe_failures_total"`
	PeerProbeSuccesses         int              `json:"peer_probe_successes_total"`
	LastQuorumAvailable        bool             `json:"last_quorum_available"`
	LastQuorumAvailableUpdated int64            `json:"last_quorum_available_updated_unix_milli,omitempty"`
}

func NewMetrics() *Metrics {
	return &Metrics{
		apiRequests:              map[string]int{},
		apiRequestDurationMillis: map[string]int64{},
		anchorStatuses:           map[string]int{},
		verificationResults:      map[string]int{},
	}
}

func (m *Metrics) RecordAPI(method, path string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := method + "|" + RoutePattern(path) + "|" + strconv.Itoa(status)
	m.apiRequests[key]++
	m.apiRequestDurationMillis[key] += duration.Milliseconds()
}

func (m *Metrics) RecordEvidenceSubmission(created bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evidenceSubmissions++
	if !created {
		m.duplicateEvidence++
	}
}

func (m *Metrics) RecordVerification(valid bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if valid {
		m.verificationResults["valid"]++
	} else {
		m.verificationResults["invalid"]++
	}
}

func (m *Metrics) RecordAnchorStatus(status string) {
	if m == nil {
		return
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "unknown"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.anchorStatuses[status]++
}

func (m *Metrics) RecordConsensusProposal(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consensusProposals++
	if err != nil {
		m.consensusProposalFailures++
	}
}

func (m *Metrics) RecordCommit(start time.Time, signerCount int) {
	if m == nil {
		return
	}
	latency := time.Since(start).Milliseconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consensusCommits++
	m.commitLatencyMillisTotal += latency
	m.lastCommitLatencyMillis = latency
	m.lastQuorumSignerCount = signerCount
}

func (m *Metrics) RecordStorageError() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storageErrors++
}

func (m *Metrics) RecordSignerError() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.signerErrors++
}

func (m *Metrics) RecordInvalidSignature() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invalidSignatures++
}

func (m *Metrics) RecordPeerProbe(success bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if success {
		m.peerProbeSuccesses++
	} else {
		m.peerProbeFailures++
	}
}

func (m *Metrics) RecordQuorumAvailable(available bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastQuorumAvailable = available
	m.lastQuorumAvailableUpdated = time.Now().UnixMilli()
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return MetricsSnapshot{
		APIRequests:                copyIntMap(m.apiRequests),
		APIRequestDurationMillis:   copyInt64Map(m.apiRequestDurationMillis),
		AnchorStatuses:             copyIntMap(m.anchorStatuses),
		VerificationResults:        copyIntMap(m.verificationResults),
		EvidenceSubmissions:        m.evidenceSubmissions,
		DuplicateEvidence:          m.duplicateEvidence,
		ConsensusProposals:         m.consensusProposals,
		ConsensusProposalFailures:  m.consensusProposalFailures,
		ConsensusCommits:           m.consensusCommits,
		CommitLatencyMillisTotal:   m.commitLatencyMillisTotal,
		LastCommitLatencyMillis:    m.lastCommitLatencyMillis,
		LastQuorumSignerCount:      m.lastQuorumSignerCount,
		StorageErrors:              m.storageErrors,
		SignerErrors:               m.signerErrors,
		InvalidSignatures:          m.invalidSignatures,
		PeerProbeFailures:          m.peerProbeFailures,
		PeerProbeSuccesses:         m.peerProbeSuccesses,
		LastQuorumAvailable:        m.lastQuorumAvailable,
		LastQuorumAvailableUpdated: m.lastQuorumAvailableUpdated,
	}
}

func copyIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyInt64Map(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func RoutePattern(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case path == "/v1/evidence":
		return "/v1/evidence"
	case strings.HasPrefix(path, "/v1/evidence/"):
		return "/v1/evidence/{id}"
	case strings.HasPrefix(path, "/v1/receipts/"):
		return "/v1/receipts/{receipt_id}"
	case path == "/v1/verify":
		return "/v1/verify"
	case strings.HasPrefix(path, "/v1/anchors/"):
		return "/v1/anchors/{qc_hash}"
	case path == "/v1/audit/recent":
		return "/v1/audit/recent"
	case path == "/v1/ops/report":
		return "/v1/ops/report"
	default:
		return path
	}
}

type ReadinessCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type ReadinessReport struct {
	Status               string           `json:"status"`
	GeneratedAtUnixMilli int64            `json:"generated_at_unix_milli"`
	Checks               []ReadinessCheck `json:"checks"`
}

func NewReadinessReport(checks []ReadinessCheck) ReadinessReport {
	status := "ready"
	for _, check := range checks {
		if !check.OK {
			status = "not_ready"
			break
		}
	}
	return ReadinessReport{Status: status, GeneratedAtUnixMilli: time.Now().UnixMilli(), Checks: checks}
}

func (r ReadinessReport) Ready() bool {
	return r.Status == "ready"
}

type CommitSummary struct {
	Height             uint64 `json:"height"`
	Round              uint64 `json:"round"`
	BlockHash          string `json:"block_hash"`
	StateDigest        string `json:"state_digest,omitempty"`
	ProposerID         string `json:"proposer_id,omitempty"`
	SignerCount        int    `json:"signer_count"`
	CreatedAtUnixMilli int64  `json:"created_at_unix_milli"`
}

type ReceiptSummary struct {
	EvidenceID         string `json:"evidence_id"`
	ReceiptID          string `json:"receipt_id"`
	EventHash          string `json:"event_hash"`
	QCHash             string `json:"qc_hash,omitempty"`
	CommitHeight       uint64 `json:"commit_height"`
	SubmittingOrg      string `json:"submitting_organization,omitempty"`
	SignerCount        int    `json:"signer_count"`
	AnchorStatus       string `json:"anchor_status,omitempty"`
	CreatedAtUnixMilli int64  `json:"created_at_unix_milli"`
}

type QuorumParticipation struct {
	ValidatorID string `json:"validator_id"`
	Votes       int    `json:"votes"`
}

type SignerStatus struct {
	Provider  string `json:"provider"`
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id,omitempty"`
	Status    string `json:"status"`
}

type VerificationSpotCheck struct {
	ReceiptID    string `json:"receipt_id"`
	EvidenceID   string `json:"evidence_id"`
	Valid        bool   `json:"valid"`
	Reason       string `json:"reason,omitempty"`
	QuorumStatus string `json:"quorum_status,omitempty"`
	AnchorStatus string `json:"anchor_status,omitempty"`
}

type OperatorReport struct {
	GeneratedAtUnixMilli int64                   `json:"generated_at_unix_milli"`
	Node                 protocol.StateSnapshot  `json:"node"`
	Readiness            ReadinessReport         `json:"readiness"`
	PeerHealth           any                     `json:"peer_health"`
	Metrics              MetricsSnapshot         `json:"metrics"`
	RecentCommits        []CommitSummary         `json:"recent_commits"`
	RecentReceipts       []ReceiptSummary        `json:"recent_receipts"`
	RecentAudit          []storage.AuditRecord   `json:"recent_audit"`
	QuorumParticipation  []QuorumParticipation   `json:"quorum_participation"`
	Signer               SignerStatus            `json:"signer"`
	VerificationChecks   []VerificationSpotCheck `json:"verification_spot_checks"`
}

func RenderReportText(report OperatorReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric operator report\n")
	fmt.Fprintf(&b, "generated_at_unix_milli=%d readiness=%s node=%s height=%d signer_provider=%s\n",
		report.GeneratedAtUnixMilli, report.Readiness.Status, report.Node.NodeID, report.Node.Height, report.Signer.Provider)
	fmt.Fprintf(&b, "metrics commits=%d evidence=%d duplicates=%d storage_errors=%d signer_errors=%d invalid_signatures=%d\n",
		report.Metrics.ConsensusCommits, report.Metrics.EvidenceSubmissions, report.Metrics.DuplicateEvidence, report.Metrics.StorageErrors, report.Metrics.SignerErrors, report.Metrics.InvalidSignatures)
	fmt.Fprintf(&b, "readiness_checks:\n")
	for _, check := range report.Readiness.Checks {
		status := "ok"
		if !check.OK {
			status = "fail"
		}
		fmt.Fprintf(&b, "  %s=%s %s\n", check.Name, status, check.Message)
	}
	fmt.Fprintf(&b, "recent_commits:\n")
	for _, commit := range report.RecentCommits {
		fmt.Fprintf(&b, "  height=%d round=%d signer_count=%d block=%s state=%s\n", commit.Height, commit.Round, commit.SignerCount, short(commit.BlockHash), short(commit.StateDigest))
	}
	fmt.Fprintf(&b, "recent_receipts:\n")
	for _, receipt := range report.RecentReceipts {
		fmt.Fprintf(&b, "  receipt=%s evidence=%s height=%d signers=%d anchor=%s org=%s\n", receipt.ReceiptID, receipt.EvidenceID, receipt.CommitHeight, receipt.SignerCount, receipt.AnchorStatus, receipt.SubmittingOrg)
	}
	fmt.Fprintf(&b, "verification_spot_checks:\n")
	for _, check := range report.VerificationChecks {
		fmt.Fprintf(&b, "  receipt=%s valid=%t quorum=%s anchor=%s reason=%s\n", check.ReceiptID, check.Valid, check.QuorumStatus, check.AnchorStatus, check.Reason)
	}
	return b.String()
}

func MarshalReportJSON(report OperatorReport, w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func SortedMetricKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func short(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
