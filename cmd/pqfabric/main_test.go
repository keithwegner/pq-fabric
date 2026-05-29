package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
	"github.com/keithwegner/pq-fabric/core/observability"
)

func TestReportCommandRendersTextAndWritesJSON(t *testing.T) {
	report := observability.OperatorReport{
		Node:      protocol.StateSnapshot{NodeID: "validator-1", Height: 3, SignerProvider: "cloud-kms"},
		Readiness: observability.NewReadinessReport([]observability.ReadinessCheck{{Name: "running", OK: true}}),
		Signer:    observability.SignerStatus{Provider: "cloud-kms", Algorithm: "ML-DSA-65", Status: "configured"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ops/report" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("expected bearer token header, got %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(report)
	}))
	defer server.Close()

	output := captureStdout(t, func() {
		if err := run([]string{"report", "--url", server.URL, "--token", "test-token", "--format", "text"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "pq-fabric operator report") || !strings.Contains(output, "validator-1") {
		t.Fatalf("unexpected text report output: %s", output)
	}

	outPath := filepath.Join(t.TempDir(), "report.json")
	if err := run([]string{"report", "--url", server.URL, "--token", "test-token", "--out", outPath}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"node_id": "validator-1"`) {
		t.Fatalf("unexpected JSON report file: %s", string(data))
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
		_ = reader.Close()
	}()
	fn()
	_ = writer.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
