package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

func TestStructuredLoggerDefaultsAndRedaction(t *testing.T) {
	var buf bytes.Buffer
	logger, err := NewLogger(Config{ProductionMode: true, LoggerOutput: &buf})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("startup", "node_id", "validator-1", "token", RedactValue("token", "secret"))
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("expected JSON log output, got %q: %v", buf.String(), err)
	}
	if record["token"] != "[redacted]" {
		t.Fatalf("expected token redaction, got %+v", record)
	}
	if RedactValue("organization", "bestgate") != "bestgate" {
		t.Fatal("expected non-secret value to pass through")
	}
	if _, err := NewLogger(Config{LogFormat: "xml"}); err == nil {
		t.Fatal("expected invalid log format to fail")
	}
}

func TestMetricsSnapshotAndRoutePatternsAreLowCardinality(t *testing.T) {
	metrics := NewMetrics()
	metrics.RecordAPI(http.MethodGet, "/v1/evidence/evidence-123", http.StatusOK, 0)
	metrics.RecordEvidenceSubmission(true)
	metrics.RecordEvidenceSubmission(false)
	metrics.RecordVerification(true)
	metrics.RecordVerification(false)
	metrics.RecordAnchorStatus("pending_testnet_anchor")
	metrics.RecordCommit(timeZero(), 5)
	snapshot := metrics.Snapshot()
	if snapshot.APIRequests["GET|/v1/evidence/{id}|200"] != 1 {
		t.Fatalf("expected route-pattern API metric, got %+v", snapshot.APIRequests)
	}
	if snapshot.EvidenceSubmissions != 2 || snapshot.DuplicateEvidence != 1 {
		t.Fatalf("unexpected evidence counters: %+v", snapshot)
	}
	if snapshot.VerificationResults["valid"] != 1 || snapshot.VerificationResults["invalid"] != 1 {
		t.Fatalf("unexpected verification counters: %+v", snapshot.VerificationResults)
	}
	if strings.Contains(strings.Join(SortedMetricKeys(snapshot.APIRequests), ","), "evidence-123") {
		t.Fatal("metrics should not include high-cardinality evidence IDs")
	}
}

func TestOTELConfigDisabledAndEnabledExporter(t *testing.T) {
	if _, err := NewRuntime(context.Background(), Config{OTELEnabled: true}); err == nil {
		t.Fatal("expected enabled OpenTelemetry without endpoint to fail")
	}
	disabled, err := NewRuntime(context.Background(), Config{NodeID: "validator-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := disabled.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int64
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("unexpected OTLP path %s", r.URL.Path)
		}
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	runtime, err := NewRuntime(context.Background(), Config{
		NodeID:             "validator-1",
		OTELEnabled:        true,
		OTELServiceName:    "pq-fabric-test",
		OTELExporterURL:    collector.URL + "/v1/traces",
		OTELAllowInsecure:  true,
		UseSynchronousOTEL: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, span := runtime.Start(context.Background(), "test.span", attribute.String("safe", "value"))
	_ = ctx
	runtime.EndSpan(span, nil)
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() == 0 {
		t.Fatal("expected fake OTLP collector to receive a trace export")
	}
}

func TestParseHeaders(t *testing.T) {
	headers, err := ParseHeaders("Authorization=Bearer secret,x-tenant=pilot")
	if err != nil {
		t.Fatal(err)
	}
	if headers["Authorization"] != "Bearer secret" || headers["x-tenant"] != "pilot" {
		t.Fatalf("unexpected headers: %+v", headers)
	}
	if _, err := ParseHeaders("broken"); err == nil {
		t.Fatal("expected malformed header config to fail")
	}
}

func timeZero() time.Time {
	return time.Unix(0, 0)
}
