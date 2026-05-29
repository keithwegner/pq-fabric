package validator

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
	consensusstate "github.com/keithwegner/pq-fabric/consensus/state"
	apiauth "github.com/keithwegner/pq-fabric/core/auth"
	"github.com/keithwegner/pq-fabric/core/consortium"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	"github.com/keithwegner/pq-fabric/core/crypto/mldsa"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	evidencepkg "github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/observability"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func TestStartReturnsBindError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	node := newTestNode(t, "validator-1", nil)
	node.cfg.ListenAddr = listener.Addr().String()
	if err := node.Start(context.Background()); err == nil {
		t.Fatal("expected bind error")
	}
	if node.Snapshot().Running {
		t.Fatal("node should not be running after bind failure")
	}
}

func TestStateAndPeerHandlers(t *testing.T) {
	node := newTestNode(t, "validator-1", map[string]string{"validator-2": "http://validator-2:8080"})
	state := httptest.NewRecorder()
	node.handleState(state, httptest.NewRequest(http.MethodGet, "/state", nil))
	if state.Code != http.StatusOK {
		t.Fatalf("expected state 200, got %d", state.Code)
	}
	var snapshot protocol.StateSnapshot
	if err := json.NewDecoder(state.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.NodeID != "validator-1" || snapshot.LastHash != protocol.GenesisHash {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	peers := httptest.NewRecorder()
	node.handlePeers(peers, httptest.NewRequest(http.MethodGet, "/peers", nil))
	if peers.Code != http.StatusOK {
		t.Fatalf("expected peers 200, got %d", peers.Code)
	}
	var health []PeerHealth
	if err := json.NewDecoder(peers.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if len(health) != 1 || health[0].PeerID != "validator-2" {
		t.Fatalf("unexpected peer health: %+v", health)
	}
}

func TestProposalVotingRejectsSameHeightConflict(t *testing.T) {
	node := newTestNode(t, "validator-2", nil)
	first := signedProposal(t, 1, protocol.GenesisHash, "first", "validator-1")
	vote := postProposal(t, node, first, http.StatusOK)
	retry := postProposal(t, node, first, http.StatusOK)
	if retry.BlockHash != vote.BlockHash {
		t.Fatal("retry for same block should return same vote")
	}
	conflict := signedProposal(t, 1, protocol.GenesisHash, "second", "validator-1")
	postProposal(t, node, conflict, http.StatusConflict)
}

func TestProposeCommitsWithFiveVotes(t *testing.T) {
	peerURLs := make(map[string]string)
	servers := make([]*httptest.Server, 0, 4)
	for _, id := range []string{"validator-2", "validator-3", "validator-4", "validator-5"} {
		peer := newTestNode(t, id, nil)
		server := serveNode(peer)
		t.Cleanup(server.Close)
		servers = append(servers, server)
		peerURLs[id] = server.URL
	}
	_ = servers

	leader := newTestNode(t, "validator-1", peerURLs)
	commit, err := leader.Propose(context.Background(), "network commit")
	if err != nil {
		t.Fatal(err)
	}
	if commit.Block.Height != 1 || len(commit.Certificate.Votes) != 5 {
		t.Fatalf("unexpected commit: height=%d votes=%d", commit.Block.Height, len(commit.Certificate.Votes))
	}
	if leader.Snapshot().Height != 1 {
		t.Fatalf("expected leader height 1, got %d", leader.Snapshot().Height)
	}
}

func TestEvidenceAPIIssuesAndVerifiesReceipt(t *testing.T) {
	peerURLs := make(map[string]string)
	for _, id := range []string{"validator-2", "validator-3", "validator-4", "validator-5"} {
		peer := newTestNode(t, id, nil)
		server := serveNode(peer)
		t.Cleanup(server.Close)
		peerURLs[id] = server.URL
	}
	leader := newTestNode(t, "validator-1", peerURLs)
	submission := evidencepkg.EvidenceSubmission{
		SchemaVersion:          evidencepkg.SchemaVersion,
		EvidenceCategory:       "incident-report",
		ArtifactHash:           "sha256:artifact",
		MetadataHash:           "sha256:metadata",
		SubmittingOrganization: "bestgate",
		IdempotencyKey:         "incident-1",
		AnchorRequested:        true,
	}
	body, err := json.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	leader.handleEvidenceSubmit(recorder, httptest.NewRequest(http.MethodPost, "/v1/evidence", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected evidence submit 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var receipt evidencepkg.EvidenceReceipt
	if err := json.NewDecoder(recorder.Body).Decode(&receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.EvidenceID == "" || receipt.ReceiptID == "" || receipt.SignerCount != 5 || receipt.AnchorStatus != evidencepkg.AnchorPending {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	verifyBody, err := json.Marshal(evidencepkg.VerificationRequest{ReceiptID: receipt.ReceiptID})
	if err != nil {
		t.Fatal(err)
	}
	verify := httptest.NewRecorder()
	leader.handleVerify(verify, httptest.NewRequest(http.MethodPost, "/v1/verify", bytes.NewReader(verifyBody)))
	if verify.Code != http.StatusOK {
		t.Fatalf("expected verify 200, got %d: %s", verify.Code, verify.Body.String())
	}
	var result evidencepkg.VerificationResult
	if err := json.NewDecoder(verify.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.QuorumStatus != "valid" {
		t.Fatalf("unexpected verification result: %+v", result)
	}
}

func TestEvidenceAPIRequiresBearerWhenConfigured(t *testing.T) {
	node := newTestNode(t, "validator-1", nil)
	node.cfg.APIBearerToken = "secret"
	recorder := httptest.NewRecorder()
	node.handleEvidenceGet(recorder, httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized response, got %d", recorder.Code)
	}
	authorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil)
	req.Header.Set("Authorization", "Bearer secret")
	node.handleEvidenceGet(authorized, req)
	if authorized.Code != http.StatusNotFound {
		t.Fatalf("expected authorized request to reach lookup, got %d", authorized.Code)
	}
}

func TestEvidenceAPIRequiresScopedAPIKeyRolesAndAuditsRequests(t *testing.T) {
	node := newTestNode(t, "validator-1", nil)
	node.authn = testAuthenticator(t, []apiauth.APIKeyRecord{
		{ID: "reader", Organization: "bestgate", TokenHash: apiauth.HashToken("reader-token"), Roles: []string{apiauth.RoleEvidenceRead}},
		{ID: "submitter", Organization: "bestgate", TokenHash: apiauth.HashToken("submit-token"), Roles: []string{apiauth.RoleEvidenceSubmit}},
		{ID: "admin", Organization: "bestgate", TokenHash: apiauth.HashToken("admin-token"), Roles: []string{apiauth.RoleAdminRead}},
	})

	missing := httptest.NewRecorder()
	node.handleEvidenceGet(missing, httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil))
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing token to be unauthorized, got %d", missing.Code)
	}

	forbidden := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil)
	req.Header.Set("Authorization", "Bearer submit-token")
	node.handleEvidenceGet(forbidden, req)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected wrong role to be forbidden, got %d", forbidden.Code)
	}

	allowed := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	node.handleEvidenceGet(allowed, req)
	if allowed.Code != http.StatusNotFound {
		t.Fatalf("expected correctly scoped key to reach lookup, got %d", allowed.Code)
	}

	audit := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/audit/recent?limit=10", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	node.handleAuditRecent(audit, req)
	if audit.Code != http.StatusOK {
		t.Fatalf("expected admin audit read to succeed, got %d: %s", audit.Code, audit.Body.String())
	}
	var records []storage.AuditRecord
	if err := json.NewDecoder(audit.Body).Decode(&records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 prior audit records, got %+v", records)
	}
	for _, record := range records {
		if record.PrincipalID == "reader" && record.Organization != "bestgate" {
			t.Fatalf("expected reader organization in audit record: %+v", record)
		}
		if record.DeniedReason == "reader-token" || record.PrincipalID == "reader-token" {
			t.Fatalf("audit record leaked raw token: %+v", record)
		}
	}
}

func TestAuditRecentRequiresAdminRole(t *testing.T) {
	node := newTestNode(t, "validator-1", nil)
	node.authn = testAuthenticator(t, []apiauth.APIKeyRecord{
		{ID: "reader", Organization: "bestgate", TokenHash: apiauth.HashToken("reader-token"), Roles: []string{apiauth.RoleEvidenceRead}},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/recent", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	recorder := httptest.NewRecorder()
	node.handleAuditRecent(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin key to be forbidden, got %d", recorder.Code)
	}
}

func TestReadinessReportsQuorumAndFailureDetails(t *testing.T) {
	node := newTestNode(t, "validator-1", map[string]string{
		"validator-2": "http://validator-2",
		"validator-3": "http://validator-3",
		"validator-4": "http://validator-4",
		"validator-5": "http://validator-5",
	})
	node.mu.Lock()
	node.running = true
	for _, id := range []string{"validator-2", "validator-3", "validator-4", "validator-5"} {
		node.peerHealth[id] = PeerHealth{PeerID: id, Healthy: true}
	}
	node.mu.Unlock()
	recorder := httptest.NewRecorder()
	node.handleReady(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected ready response, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var ready observability.ReadinessReport
	if err := json.NewDecoder(recorder.Body).Decode(&ready); err != nil {
		t.Fatal(err)
	}
	if !ready.Ready() {
		t.Fatalf("expected ready checks to pass: %+v", ready)
	}

	node.mu.Lock()
	for id, peer := range node.peerHealth {
		peer.Healthy = false
		node.peerHealth[id] = peer
	}
	node.mu.Unlock()
	recorder = httptest.NewRecorder()
	node.handleReady(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected lost quorum readiness failure, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "quorum_available") {
		t.Fatalf("expected quorum check in readiness response: %s", recorder.Body.String())
	}
}

func TestOpsProbeSurfaceExposesOnlyLiveAndReadyz(t *testing.T) {
	opsAddr := freeTestAddr(t)
	node, err := NewNode(Config{
		ID:              "validator-1",
		ListenAddr:      "127.0.0.1:0",
		OpsListenAddr:   opsAddr,
		PublicURL:       "http://validator-1",
		PeerURLs:        map[string]string{},
		Threshold:       1,
		RequestTimeout:  time.Second,
		ProposalTimeout: 20 * time.Millisecond,
		VoteTimeout:     time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := node.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = node.Stop(shutdownCtx)
		_ = node.Close()
	}()

	livez, err := http.Get("http://" + opsAddr + "/livez")
	if err != nil {
		t.Fatal(err)
	}
	if livez.StatusCode != http.StatusOK {
		t.Fatalf("expected /livez 200, got %s", livez.Status)
	}
	_ = livez.Body.Close()

	readyz, err := http.Get("http://" + opsAddr + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	if readyz.StatusCode != http.StatusOK {
		t.Fatalf("expected /readyz 200, got %s", readyz.Status)
	}
	_ = readyz.Body.Close()

	metrics, err := http.Get("http://" + opsAddr + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.StatusCode != http.StatusNotFound {
		t.Fatalf("expected ops /metrics to remain unavailable, got %s", metrics.Status)
	}
	_ = metrics.Body.Close()
}

func TestMetricsExposeLowCardinalityOperationalCounters(t *testing.T) {
	node := newTestNode(t, "validator-1", nil)
	node.obs.Metrics.RecordAPI(http.MethodGet, "/v1/evidence/evidence-secret-id", http.StatusNotFound, time.Millisecond)
	node.obs.Metrics.RecordVerification(false)
	node.obs.Metrics.RecordAnchorStatus(evidencepkg.AnchorUnavailable)
	body, err := node.renderPrometheusMetrics()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pq_fabric_api_requests_total",
		"pq_fabric_receipt_verifications_total",
		"pq_fabric_anchor_status_total",
		"pq_fabric_quorum_available",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected metric %s in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "evidence-secret-id") {
		t.Fatalf("metrics leaked high-cardinality evidence id:\n%s", body)
	}
}

func TestOpsReportRequiresAdminAndIncludesIncidentEvidence(t *testing.T) {
	peerURLs := make(map[string]string)
	for _, id := range []string{"validator-2", "validator-3", "validator-4", "validator-5"} {
		peer := newTestNode(t, id, nil)
		server := serveNode(peer)
		t.Cleanup(server.Close)
		peerURLs[id] = server.URL
	}
	leader := newTestNode(t, "validator-1", peerURLs)
	leader.authn = testAuthenticator(t, []apiauth.APIKeyRecord{
		{ID: "submitter", Organization: "bestgate", TokenHash: apiauth.HashToken("submit-token"), Roles: []string{apiauth.RoleEvidenceSubmit}},
		{ID: "admin", Organization: "bestgate", TokenHash: apiauth.HashToken("admin-token"), Roles: []string{apiauth.RoleAdminRead}},
	})
	submission := evidencepkg.EvidenceSubmission{
		SchemaVersion:          evidencepkg.SchemaVersion,
		EvidenceCategory:       "incident-report",
		ArtifactHash:           "sha256:artifact",
		MetadataHash:           "sha256:metadata",
		SubmittingOrganization: "bestgate",
		IdempotencyKey:         "incident-report-1",
	}
	body, err := json.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	submit := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/evidence", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer submit-token")
	leader.handleEvidenceSubmit(submit, req)
	if submit.Code != http.StatusOK {
		t.Fatalf("expected submit success, got %d: %s", submit.Code, submit.Body.String())
	}

	forbidden := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/ops/report", nil)
	req.Header.Set("Authorization", "Bearer submit-token")
	leader.handleOpsReport(forbidden, req)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin report request to be forbidden, got %d", forbidden.Code)
	}

	reportRecorder := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/ops/report?audit_limit=10&receipt_limit=5", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	leader.handleOpsReport(reportRecorder, req)
	if reportRecorder.Code != http.StatusOK {
		t.Fatalf("expected admin report request to succeed, got %d: %s", reportRecorder.Code, reportRecorder.Body.String())
	}
	if strings.Contains(reportRecorder.Body.String(), "admin-token") || strings.Contains(reportRecorder.Body.String(), "submit-token") {
		t.Fatalf("operator report leaked raw API token: %s", reportRecorder.Body.String())
	}
	var report observability.OperatorReport
	if err := json.NewDecoder(reportRecorder.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if len(report.RecentReceipts) == 0 || len(report.VerificationChecks) == 0 || !report.VerificationChecks[0].Valid {
		t.Fatalf("expected report to include valid receipt spot check: %+v", report)
	}
	if report.Signer.Provider == "" || report.Signer.Algorithm == "" {
		t.Fatalf("expected signer status in report: %+v", report.Signer)
	}
}

func TestEvidenceAPIRateLimitIsPerAPIKey(t *testing.T) {
	node := newTestNode(t, "validator-1", nil)
	node.authn = testAuthenticator(t, []apiauth.APIKeyRecord{
		{ID: "reader", Organization: "bestgate", TokenHash: apiauth.HashToken("reader-token"), Roles: []string{apiauth.RoleEvidenceRead}},
		{ID: "other", Organization: "bestgate", TokenHash: apiauth.HashToken("other-token"), Roles: []string{apiauth.RoleEvidenceRead}},
	})
	node.limiter = newRateLimiter(1, 1)

	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	first := httptest.NewRecorder()
	node.handleEvidenceGet(first, req)
	if first.Code != http.StatusNotFound {
		t.Fatalf("expected first request to reach lookup, got %d", first.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	second := httptest.NewRecorder()
	node.handleEvidenceGet(second, req)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", second.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/evidence/example", nil)
	req.Header.Set("Authorization", "Bearer other-token")
	other := httptest.NewRecorder()
	node.handleEvidenceGet(other, req)
	if other.Code != http.StatusNotFound {
		t.Fatalf("expected other key to have independent bucket, got %d", other.Code)
	}
}

func TestProductionModeRejectsDevCryptoAndMissingToken(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	if _, err := NewNode(testProductionConfig(t, "validator-1", cryptosuite.Dev)); err == nil {
		t.Fatal("expected production mode to reject dev crypto")
	}
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	missingAPIKeys := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	missingAPIKeys.APIKeysFile = ""
	missingAPIKeys.APIBearerToken = "legacy"
	if _, err := NewNode(missingAPIKeys); err == nil {
		t.Fatal("expected production mode to require scoped API keys file")
	}
	missingManifest := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	missingManifest.ConsortiumManifest = ""
	if _, err := NewNode(missingManifest); err == nil {
		t.Fatal("expected production mode to require consortium manifest")
	}
	missingHistory := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	missingHistory.ManifestHistory = ""
	if _, err := NewNode(missingHistory); err == nil {
		t.Fatal("expected production mode to require manifest history")
	}
	wrongStorage := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	wrongStorage.StorageMode = storage.ModeDurable
	if _, err := NewNode(wrongStorage); err == nil {
		t.Fatal("expected production mode to require sqlite storage")
	}
	missingTLS := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	missingTLS.PeerTLSCertFile = ""
	if _, err := NewNode(missingTLS); err == nil {
		t.Fatal("expected production mode to require peer mTLS files")
	}
	localSignerRejected := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	localSignerRejected.AllowLocalSigner = false
	if _, err := NewNode(localSignerRejected); err == nil {
		t.Fatal("expected production mode to reject local signing unless explicitly allowed")
	}
	node, err := NewNode(testProductionConfig(t, "validator-1", cryptosuite.PQ))
	if err != nil {
		t.Fatalf("expected production mode to accept scoped API keys, manifest history, sqlite, and mTLS: %v", err)
	}
	if node.membershipVersion != 1 || node.manifestHash == "" || node.signerProvider != "local" {
		t.Fatalf("expected production node to load manifest and signer provider, got version=%d hash=%q provider=%q", node.membershipVersion, node.manifestHash, node.signerProvider)
	}
	if node.peerTLS == nil || node.peerClient == nil {
		t.Fatal("expected production node to configure peer mTLS")
	}
	_ = node.Close()
}

func TestHSMProviderFailsClosed(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	tlsFiles := testPeerTLSFiles(t, "test-consortium", "validator-1")
	manifest := testConsortiumManifestFile(t, cryptosuite.PQ)
	_, err := NewNode(Config{
		ID:                 "validator-1",
		ListenAddr:         "127.0.0.1:0",
		ProductionMode:     true,
		APIKeysFile:        testAPIKeysFile(t),
		ConsortiumManifest: manifest,
		ManifestHistory:    manifest,
		StorageMode:        storage.ModeSQLite,
		DatabaseURL:        filepath.Join(t.TempDir(), "validator.db"),
		SignerProvider:     "hsm",
		PeerTLSCertFile:    tlsFiles.certFile,
		PeerTLSKeyFile:     tlsFiles.keyFile,
		PeerTLSCAFile:      tlsFiles.caFile,
	})
	if err == nil {
		t.Fatal("expected unsupported hsm provider to fail closed")
	}
}

func TestProductionModeAcceptsCloudKMSSigner(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	kmsEndpoint, kmsKeyID, kmsCAFile := testKMSServer(t, cryptosuite.PQ, "validator-1", false)
	cfg := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	cfg.AllowLocalSigner = false
	cfg.SignerProvider = "cloud-kms"
	cfg.KMSEndpoint = kmsEndpoint
	cfg.KMSKeyID = kmsKeyID
	cfg.KMSCAFile = kmsCAFile
	node, err := NewNode(cfg)
	if err != nil {
		t.Fatalf("expected production mode to accept configured cloud-kms signer: %v", err)
	}
	defer node.Close()
	signature, err := node.signer.Sign([]byte("production-key-custody"))
	if err != nil {
		t.Fatal(err)
	}
	if !node.verifier.Verify(node.selfID.SignaturePublicKeyBytes(), []byte("production-key-custody"), signature) {
		t.Fatal("expected cloud-kms signature to verify")
	}
}

func TestProductionModeRejectsBadCloudKMSKeys(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	disabledEndpoint, disabledKeyID, disabledCA := testKMSServer(t, cryptosuite.PQ, "validator-1", true)
	cfg := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	cfg.AllowLocalSigner = false
	cfg.SignerProvider = "cloud-kms"
	cfg.KMSEndpoint = disabledEndpoint
	cfg.KMSKeyID = disabledKeyID
	cfg.KMSCAFile = disabledCA
	if _, err := NewNode(cfg); err == nil {
		t.Fatal("expected disabled KMS key to fail startup")
	}
	wrongEndpoint, wrongKeyID, wrongCA := testKMSServer(t, cryptosuite.PQ, "validator-2", false)
	cfg = testProductionConfig(t, "validator-1", cryptosuite.PQ)
	cfg.AllowLocalSigner = false
	cfg.SignerProvider = "cloud-kms"
	cfg.KMSEndpoint = wrongEndpoint
	cfg.KMSKeyID = wrongKeyID
	cfg.KMSCAFile = wrongCA
	if _, err := NewNode(cfg); err == nil {
		t.Fatal("expected wrong KMS public key to fail startup")
	}
}

func TestProductionPeerMTLSAuthorizesValidatorURISAN(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	authority := newTestTLSAuthority(t)
	serverCert, serverKey := authority.signedPeerCert(t, "test-consortium", "validator-2", false)
	manifest := testConsortiumManifestFile(t, cryptosuite.PQ)
	node, err := NewNode(Config{
		ID:                 "validator-2",
		ListenAddr:         "127.0.0.1:0",
		ProductionMode:     true,
		APIKeysFile:        testAPIKeysFile(t),
		ConsortiumManifest: manifest,
		ManifestHistory:    manifest,
		StorageMode:        storage.ModeSQLite,
		DatabaseURL:        filepath.Join(t.TempDir(), "validator.db"),
		SignerProvider:     "local",
		AllowLocalSigner:   true,
		PeerTLSCertFile:    serverCert,
		PeerTLSKeyFile:     serverKey,
		PeerTLSCAFile:      authority.caFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /consensus/proposal", node.withPeerAuth(node.handleProposal))
	server := httptest.NewUnstartedServer(mux)
	server.TLS = node.peerTLS.Server
	server.StartTLS()
	defer server.Close()

	proposal := signedCurrentProposal(t, node, "validator-1")
	body, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, clientKey := authority.signedPeerCert(t, "test-consortium", "validator-1", false)
	client := authority.client(t, clientCert, clientKey)
	resp, err := client.Post(server.URL+"/consensus/proposal", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected valid peer cert to be authorized, got %s", resp.Status)
	}
}

func TestProductionPeerMTLSRejectsMissingUnknownUntrustedAndExpiredCerts(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	authority := newTestTLSAuthority(t)
	serverCert, serverKey := authority.signedPeerCert(t, "test-consortium", "validator-2", false)
	manifest := testConsortiumManifestFile(t, cryptosuite.PQ)
	node, err := NewNode(Config{
		ID:                 "validator-2",
		ListenAddr:         "127.0.0.1:0",
		ProductionMode:     true,
		APIKeysFile:        testAPIKeysFile(t),
		ConsortiumManifest: manifest,
		ManifestHistory:    manifest,
		StorageMode:        storage.ModeSQLite,
		DatabaseURL:        filepath.Join(t.TempDir(), "validator.db"),
		SignerProvider:     "local",
		AllowLocalSigner:   true,
		PeerTLSCertFile:    serverCert,
		PeerTLSKeyFile:     serverKey,
		PeerTLSCAFile:      authority.caFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /consensus/proposal", node.withPeerAuth(node.handleProposal))
	server := httptest.NewUnstartedServer(mux)
	server.TLS = node.peerTLS.Server
	server.StartTLS()
	defer server.Close()

	proposal := signedCurrentProposal(t, node, "validator-1")
	body, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	missing := authority.clientWithoutCert(t)
	resp, err := missing.Post(server.URL+"/consensus/proposal", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected missing client cert to be unauthorized, got %s", resp.Status)
	}
	_ = resp.Body.Close()

	unknownCert, unknownKey := authority.signedPeerCert(t, "test-consortium", "validator-99", false)
	unknown := authority.client(t, unknownCert, unknownKey)
	resp, err = unknown.Post(server.URL+"/consensus/proposal", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unknown validator cert to be unauthorized, got %s", resp.Status)
	}
	_ = resp.Body.Close()

	untrustedAuthority := newTestTLSAuthority(t)
	untrustedCert, untrustedKey := untrustedAuthority.signedPeerCert(t, "test-consortium", "validator-1", false)
	if _, err := authority.client(t, untrustedCert, untrustedKey).Post(server.URL+"/consensus/proposal", "application/json", bytes.NewReader(body)); err == nil {
		t.Fatal("expected untrusted client cert handshake to fail")
	}

	expiredCert, expiredKey := authority.signedPeerCert(t, "test-consortium", "validator-1", true)
	if _, err := authority.client(t, expiredCert, expiredKey).Post(server.URL+"/consensus/proposal", "application/json", bytes.NewReader(body)); err == nil {
		t.Fatal("expected expired client cert handshake to fail")
	}
}

func TestProductionV1APIUsesAPIKeyWithoutClientCertificate(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	authority := newTestTLSAuthority(t)
	serverCert, serverKey := authority.signedPeerCert(t, "test-consortium", "validator-1", false)
	manifest := testConsortiumManifestFile(t, cryptosuite.PQ)
	node, err := NewNode(Config{
		ID:                 "validator-1",
		ListenAddr:         "127.0.0.1:0",
		ProductionMode:     true,
		APIKeysFile:        testAPIKeysFile(t),
		ConsortiumManifest: manifest,
		ManifestHistory:    manifest,
		StorageMode:        storage.ModeSQLite,
		DatabaseURL:        filepath.Join(t.TempDir(), "validator.db"),
		SignerProvider:     "local",
		AllowLocalSigner:   true,
		PeerTLSCertFile:    serverCert,
		PeerTLSKeyFile:     serverKey,
		PeerTLSCAFile:      authority.caFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/evidence/", node.handleEvidenceGet)
	server := httptest.NewUnstartedServer(mux)
	server.TLS = node.peerTLS.Server
	server.StartTLS()
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/evidence/missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer admin-token")
	resp, err := authority.clientWithoutCert(t).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected API-key auth to reach /v1 handler without client cert, got %s", resp.Status)
	}
}

func TestProductionModeRejectsHTTPPeerURLs(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	cfg := testProductionConfig(t, "validator-1", cryptosuite.PQ)
	cfg.ConsortiumManifest = testConsortiumManifestFileWithURLTemplate(t, cryptosuite.PQ, "http://{id}:8080")
	cfg.ManifestHistory = cfg.ConsortiumManifest
	if _, err := NewNode(cfg); err == nil {
		t.Fatal("expected production mode to reject HTTP peer URLs")
	}
}

func TestManifestHistoryVerifiesOldReceiptAfterReplacement(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	v1Path, v2Path, v1, _ := testReplacementManifestFiles(t, cryptosuite.Dev)
	node := newManifestTestNode(t, "validator-1", v2Path, v1Path+","+v2Path, nil)
	defer node.Close()
	receipt := makeEvidenceReceiptForManifest(t, v1, []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5"}, "legacy-idem")
	result := node.VerifyEvidenceReceipt(receipt)
	if !result.Valid || result.QuorumStatus != "valid" {
		t.Fatalf("expected old receipt to verify through manifest history, got %+v", result)
	}
}

func TestCurrentManifestReceiptUsesV2AndInactiveValidatorRejected(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	v1Path, v2Path, _, v2 := testReplacementManifestFiles(t, cryptosuite.Dev)
	peerURLs := make(map[string]string)
	for _, id := range []string{"validator-2", "validator-3", "validator-4", "validator-5"} {
		peer := newManifestTestNode(t, id, v2Path, v1Path+","+v2Path, nil)
		server := serveNode(peer)
		t.Cleanup(server.Close)
		peerURLs[id] = server.URL
	}
	leader := newManifestTestNode(t, "validator-1", v2Path, v1Path+","+v2Path, peerURLs)
	defer leader.Close()
	submission := evidencepkg.EvidenceSubmission{
		SchemaVersion:          evidencepkg.SchemaVersion,
		EvidenceCategory:       "replacement-test",
		ArtifactHash:           "sha256:artifact",
		MetadataHash:           "sha256:metadata",
		SubmittingOrganization: "bestgate",
		IdempotencyKey:         "replacement-v2",
	}
	receipt, err := leader.SubmitEvidence(context.Background(), submission)
	if err != nil {
		t.Fatal(err)
	}
	v2Hash, err := v2.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.MembershipVersion != 2 || receipt.ValidatorSetHash != v2Hash || receipt.Commit.Certificate.MembershipVersion != 2 {
		t.Fatalf("expected v2 receipt membership binding, got version=%d set=%s cert_version=%d", receipt.MembershipVersion, receipt.ValidatorSetHash, receipt.Commit.Certificate.MembershipVersion)
	}

	bad := makeMembershipCommitWithState(t, consensusstate.NewMachine(), 1, 0, protocol.GenesisHash, "bad inactive voter", "validator-1", []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-7"}, v2)
	if err := leader.verifyCommit(bad); err == nil {
		t.Fatal("expected inactive validator-7 vote to be rejected under v2 manifest")
	}
}

func TestProposerOneDownRoundAdvanceCommits(t *testing.T) {
	peerURLs := make(map[string]string)
	for _, id := range []string{"validator-3", "validator-4", "validator-5", "validator-6"} {
		peer := newTestNode(t, id, nil)
		server := serveNode(peer)
		t.Cleanup(server.Close)
		peerURLs[id] = server.URL
	}

	node := newTestNode(t, "validator-2", peerURLs)
	commit, err := node.Propose(context.Background(), "round advance")
	if err != nil {
		t.Fatal(err)
	}
	if commit.Block.Height != 1 || commit.Block.Round != 1 || commit.Block.ProposerID != "validator-2" {
		t.Fatalf("expected validator-2 to commit height 1 round 1, got height=%d round=%d proposer=%s", commit.Block.Height, commit.Block.Round, commit.Block.ProposerID)
	}
	if len(commit.Certificate.Votes) != 5 {
		t.Fatalf("expected 5 precommits, got %d", len(commit.Certificate.Votes))
	}
}

func TestThreeValidatorsDownDoesNotCommit(t *testing.T) {
	peerURLs := make(map[string]string)
	for _, id := range []string{"validator-2", "validator-3", "validator-4"} {
		peer := newTestNode(t, id, nil)
		server := serveNode(peer)
		t.Cleanup(server.Close)
		peerURLs[id] = server.URL
	}

	node := newTestNode(t, "validator-1", peerURLs)
	if _, err := node.Propose(context.Background(), "insufficient quorum"); err == nil {
		t.Fatal("expected 4-of-7 not to commit")
	}
}

func TestWrongProposerAndStaleProposalRejected(t *testing.T) {
	node := newTestNode(t, "validator-2", nil)
	wrongProposer := signedProposal(t, 1, protocol.GenesisHash, "wrong proposer", "validator-2")
	postProposal(t, node, wrongProposer, http.StatusConflict)

	voters := []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5"}
	commit := makeCommitWithState(t, consensusstate.NewMachine(), 1, 0, protocol.GenesisHash, "one", "validator-1", voters)
	if err := node.ApplyCommit(commit); err != nil {
		t.Fatal(err)
	}
	stale := signedProposal(t, 1, protocol.GenesisHash, "stale", "validator-1")
	postProposal(t, node, stale, http.StatusConflict)
}

func TestNewNodeSupportsPQSuiteSelection(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ))
	node, err := NewNode(Config{
		ID:             "validator-1",
		ListenAddr:     "127.0.0.1:0",
		PublicURL:      "http://validator-1",
		PeerURLs:       map[string]string{},
		Threshold:      5,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Snapshot().CryptoSigner != mldsa.Algorithm {
		t.Fatalf("expected PQ signer %s, got %s", mldsa.Algorithm, node.Snapshot().CryptoSigner)
	}
}

func TestCatchUpSkipsInvalidLongerCandidate(t *testing.T) {
	voters := []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5"}
	stateMachine := consensusstate.NewMachine()
	commit1 := makeCommitWithState(t, stateMachine, 1, 0, protocol.GenesisHash, "one", "validator-1", voters)
	hash1, err := commit1.Block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	commit2 := makeCommitWithState(t, stateMachine, 2, 0, hash1, "two", "validator-2", voters)
	hash2, err := commit2.Block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	commit3 := makeCommitWithState(t, stateMachine, 3, 0, hash2, "three", "validator-3", voters)
	badCommit2 := commit2
	badCommit2.Block.PreviousHash = "wrong"

	goodPeer := serveCommits([]protocol.CommitRequest{commit1, commit2})
	defer goodPeer.Close()
	badPeer := serveCommits([]protocol.CommitRequest{commit1, badCommit2, commit3})
	defer badPeer.Close()

	node := newTestNode(t, "validator-6", map[string]string{"validator-1": badPeer.URL, "validator-2": goodPeer.URL})
	if err := node.ApplyCommit(commit1); err != nil {
		t.Fatal(err)
	}
	if err := node.CatchUpFromPeers(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := node.Snapshot()
	if snapshot.Height != 2 || snapshot.LastHash != hash2 {
		t.Fatalf("expected catch-up to apply valid shorter chain, got %+v", snapshot)
	}
}

func TestDurableNodeRecoversCommittedStateAfterRestart(t *testing.T) {
	dir := t.TempDir()
	voters := []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5"}
	commit := makeCommitWithState(t, consensusstate.NewMachine(), 1, 0, protocol.GenesisHash, "one", "validator-1", voters)
	blockHash, err := commit.Block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	node := newDurableTestNode(t, "validator-1", dir, nil)
	if err := node.ApplyCommit(commit); err != nil {
		t.Fatal(err)
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newDurableTestNode(t, "validator-1", dir, nil)
	defer reopened.Close()
	snapshot := reopened.Snapshot()
	if snapshot.Height != 1 || snapshot.LastHash != blockHash {
		t.Fatalf("expected durable height/hash after restart, got %+v", snapshot)
	}
	commits := reopened.Commits()
	if len(commits) != 1 {
		t.Fatalf("expected committed block log after restart, got %d", len(commits))
	}
	record, ok, err := reopened.store.Commit(1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.BlockHash != blockHash || len(record.CertificateJSON) == 0 {
		t.Fatalf("expected quorum certificate record after restart, got %+v ok=%t", record, ok)
	}
}

func TestDurableNodeRejectsCorruptedStateOnStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "validator_state.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	if _, err := NewNode(Config{
		ID:             "validator-1",
		ListenAddr:     "127.0.0.1:0",
		PublicURL:      "http://validator-1",
		PeerURLs:       map[string]string{},
		Threshold:      5,
		RequestTimeout: time.Second,
		StorageMode:    storage.ModeDurable,
		DataDir:        dir,
	}); err == nil {
		t.Fatal("expected corrupted durable state to fail startup")
	}
}

func TestDurableNodeReplayedCommittedBlockDoesNotCorruptState(t *testing.T) {
	dir := t.TempDir()
	voters := []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5"}
	commit := makeCommitWithState(t, consensusstate.NewMachine(), 1, 0, protocol.GenesisHash, "one", "validator-1", voters)
	blockHash, err := commit.Block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	node := newDurableTestNode(t, "validator-1", dir, nil)
	if err := node.ApplyCommit(commit); err != nil {
		t.Fatal(err)
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newDurableTestNode(t, "validator-1", dir, nil)
	defer reopened.Close()
	if err := reopened.ApplyCommit(commit); err != nil {
		t.Fatal(err)
	}
	snapshot := reopened.Snapshot()
	if snapshot.Height != 1 || snapshot.LastHash != blockHash || len(reopened.Commits()) != 1 {
		t.Fatalf("replayed commit corrupted durable state: %+v commits=%d", snapshot, len(reopened.Commits()))
	}
}

func TestDurableNodeCatchUpPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	voters := []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5"}
	stateMachine := consensusstate.NewMachine()
	commit1 := makeCommitWithState(t, stateMachine, 1, 0, protocol.GenesisHash, "one", "validator-1", voters)
	hash1, err := commit1.Block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	commit2 := makeCommitWithState(t, stateMachine, 2, 0, hash1, "two", "validator-2", voters)
	hash2, err := commit2.Block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	peer := serveCommits([]protocol.CommitRequest{commit1, commit2})
	defer peer.Close()

	node := newDurableTestNode(t, "validator-6", dir, map[string]string{"validator-1": peer.URL})
	if err := node.ApplyCommit(commit1); err != nil {
		t.Fatal(err)
	}
	if err := node.CatchUpFromPeers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newDurableTestNode(t, "validator-6", dir, map[string]string{"validator-1": peer.URL})
	defer reopened.Close()
	snapshot := reopened.Snapshot()
	if snapshot.Height != 2 || snapshot.LastHash != hash2 || len(reopened.Commits()) != 2 {
		t.Fatalf("expected catch-up state after restart, got %+v commits=%d", snapshot, len(reopened.Commits()))
	}
}

func newTestNode(t *testing.T, id string, peerURLs map[string]string) *Node {
	t.Helper()
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	if peerURLs == nil {
		peerURLs = map[string]string{}
	}
	node, err := NewNode(Config{
		ID:              id,
		ListenAddr:      "127.0.0.1:0",
		PublicURL:       "http://" + id,
		PeerURLs:        peerURLs,
		Threshold:       5,
		RequestTimeout:  time.Second,
		ProposalTimeout: 20 * time.Millisecond,
		VoteTimeout:     time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func newDurableTestNode(t *testing.T, id, dir string, peerURLs map[string]string) *Node {
	t.Helper()
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	if peerURLs == nil {
		peerURLs = map[string]string{}
	}
	node, err := NewNode(Config{
		ID:              id,
		ListenAddr:      "127.0.0.1:0",
		PublicURL:       "http://" + id,
		PeerURLs:        peerURLs,
		Threshold:       5,
		RequestTimeout:  time.Second,
		ProposalTimeout: 20 * time.Millisecond,
		VoteTimeout:     time.Second,
		StorageMode:     storage.ModeDurable,
		DataDir:         dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func newManifestTestNode(t *testing.T, id, manifestPath, history string, peerURLs map[string]string) *Node {
	t.Helper()
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	if peerURLs == nil {
		peerURLs = map[string]string{}
	}
	node, err := NewNode(Config{
		ID:                 id,
		ListenAddr:         "127.0.0.1:0",
		PeerURLs:           peerURLs,
		Threshold:          5,
		RequestTimeout:     time.Second,
		ProposalTimeout:    20 * time.Millisecond,
		VoteTimeout:        time.Second,
		ConsortiumManifest: manifestPath,
		ManifestHistory:    history,
	})
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func signedProposal(t *testing.T, height uint64, previousHash, payload, proposerID string) protocol.Proposal {
	t.Helper()
	transactions := consensusstate.TransactionsFromPayload(payload)
	machine := consensusstate.NewMachine()
	stateDigest, _, err := machine.Apply(transactions)
	if err != nil {
		t.Fatal(err)
	}
	block := protocol.NewRoundBlock(height, 0, previousHash, payload, proposerID, stateDigest)
	block.Transactions = transactions
	proposal, err := protocol.SignProposal(block, pqcrypto.NewDeterministicDevSigner(proposerID))
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func postProposal(t *testing.T, node *Node, proposal protocol.Proposal, status int) protocol.Vote {
	t.Helper()
	body, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	node.handleProposal(recorder, httptest.NewRequest(http.MethodPost, "/consensus/proposal", bytes.NewReader(body)))
	if recorder.Code != status {
		t.Fatalf("expected status %d, got %d with body %s", status, recorder.Code, recorder.Body.String())
	}
	if status >= 300 {
		return protocol.Vote{}
	}
	var vote protocol.Vote
	if err := json.NewDecoder(recorder.Body).Decode(&vote); err != nil {
		t.Fatal(err)
	}
	return vote
}

func serveNode(node *Node) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", node.handleHealth)
	mux.HandleFunc("GET /commits", node.handleCommits)
	mux.HandleFunc("POST /consensus/proposal", node.handleProposal)
	mux.HandleFunc("POST /consensus/precommit", node.handlePrecommit)
	mux.HandleFunc("POST /consensus/commit", node.handleCommit)
	return httptest.NewServer(mux)
}

func freeTestAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func serveCommits(commits []protocol.CommitRequest) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commits" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commits)
	}))
}

func testAuthenticator(t *testing.T, records []apiauth.APIKeyRecord) *apiauth.Authenticator {
	t.Helper()
	authn, err := apiauth.NewAuthenticator(records)
	if err != nil {
		t.Fatal(err)
	}
	return authn
}

func testAPIKeysFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-keys.json")
	data := `{"keys":[{"id":"admin","organization":"bestgate","token_hash":"` + apiauth.HashToken("admin-token") + `","roles":["admin:read","evidence:read","evidence:submit","evidence:verify","anchor:read"]}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testConsortiumManifestFile(t *testing.T, suite cryptosuite.Name) string {
	t.Helper()
	return testConsortiumManifestFileWithURLTemplate(t, suite, "https://{id}.example.test:8443")
}

func testConsortiumManifestFileWithURLTemplate(t *testing.T, suite cryptosuite.Name, publicURLTemplate string) string {
	t.Helper()
	selected := cryptosuite.MustLookup(string(suite))
	urls := map[string]string{}
	for _, id := range identity.DefaultValidatorIDs() {
		urls[id] = strings.ReplaceAll(publicURLTemplate, "{id}", id)
	}
	identities, err := identity.ValidatorIdentitiesForSuite(urls, selected)
	if err != nil {
		t.Fatal(err)
	}
	manifest := consortium.ManifestFromIdentities("test-consortium", 1, 5, identities, identity.DefaultValidatorIDs())
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "consortium.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testReplacementManifestFiles(t *testing.T, suite cryptosuite.Name) (string, string, consortium.Manifest, consortium.Manifest) {
	t.Helper()
	selected := cryptosuite.MustLookup(string(suite))
	urls := map[string]string{}
	for _, id := range append(identity.DefaultValidatorIDs(), "validator-8") {
		urls[id] = "https://" + id + ".example.test:8443"
	}
	identities, err := identity.ValidatorIdentitiesForSuite(urls, selected)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := identity.ValidatorIdentityForSuite("validator-8", "seattle", urls["validator-8"], selected)
	if err != nil {
		t.Fatal(err)
	}
	identities["validator-8"] = replacement
	v1 := consortium.ManifestFromIdentities("test-consortium", 1, 5, identities, identity.DefaultValidatorIDs())
	v2Order := []string{"validator-1", "validator-2", "validator-3", "validator-4", "validator-5", "validator-6", "validator-8"}
	v2 := consortium.ManifestFromIdentities("test-consortium", 2, 5, identities, v2Order)
	inactive := consortium.ValidatorRecordFromIdentity(identities["validator-7"], "operator-validator-7", false)
	inactive.TLSURISAN = consortium.ExpectedTLSURISAN("test-consortium", "validator-7")
	v2.Validators = append(v2.Validators, inactive)
	return testManifestFile(t, v1, "consortium-v1.json"), testManifestFile(t, v2, "consortium-v2.json"), v1, v2
}

func testManifestFile(t *testing.T, manifest consortium.Manifest, name string) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testProductionConfig(t *testing.T, id string, suite cryptosuite.Name) Config {
	t.Helper()
	manifest := testConsortiumManifestFile(t, suite)
	tlsFiles := testPeerTLSFiles(t, "test-consortium", id)
	return Config{
		ID:                 id,
		ListenAddr:         "127.0.0.1:0",
		ProductionMode:     true,
		APIKeysFile:        testAPIKeysFile(t),
		ConsortiumManifest: manifest,
		ManifestHistory:    manifest,
		StorageMode:        storage.ModeSQLite,
		DatabaseURL:        filepath.Join(t.TempDir(), "validator.db"),
		SignerProvider:     "local",
		AllowLocalSigner:   true,
		PeerTLSCertFile:    tlsFiles.certFile,
		PeerTLSKeyFile:     tlsFiles.keyFile,
		PeerTLSCAFile:      tlsFiles.caFile,
	}
}

type testTLSFiles struct {
	certFile string
	keyFile  string
	caFile   string
}

func testPeerTLSFiles(t *testing.T, consortiumID, validatorID string) testTLSFiles {
	t.Helper()
	authority := newTestTLSAuthority(t)
	certFile, keyFile := authority.signedPeerCert(t, consortiumID, validatorID, false)
	return testTLSFiles{certFile: certFile, keyFile: keyFile, caFile: authority.caFile}
}

func testKMSServer(t *testing.T, suite cryptosuite.Name, nodeID string, disabled bool) (string, string, string) {
	t.Helper()
	selected := cryptosuite.MustLookup(string(suite))
	signer, err := selected.NewSigner(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "kms://" + nodeID + "/" + identity.ValidatorKeyID(nodeID, signer.Algorithm(), signer.PublicKey(), selected.KEMAlgorithm, []byte("kms-test-kem-placeholder"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/keys/", func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.URL.Path, "/v1/keys/") == "" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key_id":     keyID,
			"algorithm":  signer.Algorithm(),
			"public_key": base64.StdEncoding.EncodeToString(signer.PublicKey()),
			"disabled":   disabled,
		})
	})
	mux.HandleFunc("POST /v1/sign", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			KeyID     string `json:"key_id"`
			Algorithm string `json:"algorithm"`
			Message   string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.KeyID != keyID {
			http.Error(w, "wrong key", http.StatusNotFound)
			return
		}
		message, err := base64.StdEncoding.DecodeString(req.Message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		signature, err := signer.Sign(message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"key_id":    keyID,
			"algorithm": signer.Algorithm(),
			"signature": base64.StdEncoding.EncodeToString(signature),
		})
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	caFile := filepath.Join(t.TempDir(), "kms-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return server.URL, keyID, caFile
}

type testTLSAuthority struct {
	dir    string
	ca     *x509.Certificate
	caKey  *rsa.PrivateKey
	caFile string
}

func newTestTLSAuthority(t *testing.T) testTLSAuthority {
	t.Helper()
	dir := t.TempDir()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "pq-fabric test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return testTLSAuthority{dir: dir, ca: caTemplate, caKey: caKey, caFile: caFile}
}

func (a testTLSAuthority) signedPeerCert(t *testing.T, consortiumID, validatorID string, expired bool) (string, string) {
	t.Helper()
	return testSignedPeerCert(t, a.dir, a.ca, a.caKey, consortiumID, validatorID, expired)
}

func (a testTLSAuthority) client(t *testing.T, certFile, keyFile string) *http.Client {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	caPEM, err := os.ReadFile(a.caFile)
	if err != nil {
		t.Fatal(err)
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to load test CA")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, Certificates: []tls.Certificate{cert}}}}
}

func (a testTLSAuthority) clientWithoutCert(t *testing.T) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	caPEM, err := os.ReadFile(a.caFile)
	if err != nil {
		t.Fatal(err)
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to load test CA")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
}

func testSignedPeerCert(t *testing.T, dir string, ca *x509.Certificate, caKey *rsa.PrivateKey, consortiumID, validatorID string, expired bool) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(consortium.ExpectedTLSURISAN(consortiumID, validatorID))
	if err != nil {
		t.Fatal(err)
	}
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	if expired {
		notBefore = time.Now().Add(-2 * time.Hour)
		notAfter = time.Now().Add(-time.Hour)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: validatorID},
		DNSNames:     []string{"127.0.0.1", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		URIs:         []*url.URL{uri},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certFile := filepath.Join(dir, validatorID+".crt")
	keyFile := filepath.Join(dir, validatorID+".key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func signedCurrentProposal(t *testing.T, node *Node, proposerID string) protocol.Proposal {
	t.Helper()
	transactions := consensusstate.TransactionsFromPayload("peer mTLS proposal")
	machine := consensusstate.NewMachine()
	stateDigest, _, err := machine.Apply(transactions)
	if err != nil {
		t.Fatal(err)
	}
	block := protocol.NewRoundBlock(1, 0, protocol.GenesisHash, "peer mTLS proposal", proposerID, stateDigest)
	block.Transactions = transactions
	block.MembershipVersion = node.membershipVersion
	block.ValidatorSetHash = node.manifestHash
	signer, err := mldsa.NewDeterministicSigner(proposerID)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := protocol.SignProposal(block, signer)
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func makeCommitWithState(t *testing.T, machine *consensusstate.Machine, height, round uint64, previousHash, payload, proposerID string, voterIDs []string) protocol.CommitRequest {
	t.Helper()
	expectedProposer, err := protocol.ProposerFor(height, round, identity.DefaultValidatorIDs())
	if err != nil {
		t.Fatal(err)
	}
	if expectedProposer != proposerID {
		t.Fatalf("proposer %s is not scheduled for height %d round %d; expected %s", proposerID, height, round, expectedProposer)
	}
	transactions := consensusstate.TransactionsFromPayload(payload)
	stateDigest, _, err := machine.Apply(transactions)
	if err != nil {
		t.Fatal(err)
	}
	block := protocol.NewRoundBlock(height, round, previousHash, payload, proposerID, stateDigest)
	block.Transactions = transactions
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	votes := make([]protocol.Vote, 0, len(voterIDs))
	for _, id := range voterIDs {
		vote, err := protocol.SignStageVote(height, round, protocol.StagePrecommit, blockHash, id, pqcrypto.NewDeterministicDevSigner(id))
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	cert, err := protocol.FormStageQuorumCertificate(height, round, protocol.StagePrecommit, blockHash, votes, 5)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.CommitRequest{Block: block, Certificate: cert}
}

func makeEvidenceReceiptForManifest(t *testing.T, manifest consortium.Manifest, voterIDs []string, idempotencyKey string) evidencepkg.EvidenceReceipt {
	t.Helper()
	submission := evidencepkg.EvidenceSubmission{
		SchemaVersion:          evidencepkg.SchemaVersion,
		EvidenceCategory:       "history-test",
		ArtifactHash:           "sha256:artifact-" + idempotencyKey,
		MetadataHash:           "sha256:metadata-" + idempotencyKey,
		SubmittingOrganization: "bestgate",
		IdempotencyKey:         idempotencyKey,
	}
	payload, err := evidencepkg.SubmissionPayload(submission)
	if err != nil {
		t.Fatal(err)
	}
	commit := makeMembershipCommitWithState(t, consensusstate.NewMachine(), 1, 0, protocol.GenesisHash, payload, "validator-1", voterIDs, manifest)
	receipt, err := evidencepkg.NewReceipt(submission, commit)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func makeMembershipCommitWithState(t *testing.T, machine *consensusstate.Machine, height, round uint64, previousHash, payload, proposerID string, voterIDs []string, manifest consortium.Manifest) protocol.CommitRequest {
	t.Helper()
	expectedProposer, err := protocol.ProposerFor(height, round, manifest.ActiveValidatorIDs())
	if err != nil {
		t.Fatal(err)
	}
	if expectedProposer != proposerID {
		t.Fatalf("proposer %s is not scheduled for height %d round %d; expected %s", proposerID, height, round, expectedProposer)
	}
	transactions := consensusstate.TransactionsFromPayload(payload)
	stateDigest, _, err := machine.Apply(transactions)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := manifest.Hash()
	if err != nil {
		t.Fatal(err)
	}
	block := protocol.NewRoundBlock(height, round, previousHash, payload, proposerID, stateDigest)
	block.Transactions = transactions
	block.MembershipVersion = manifest.MembershipVersion
	block.ValidatorSetHash = hash
	blockHash, err := block.Hash()
	if err != nil {
		t.Fatal(err)
	}
	votes := make([]protocol.Vote, 0, len(voterIDs))
	for _, id := range voterIDs {
		vote, err := protocol.SignStageVote(height, round, protocol.StagePrecommit, blockHash, id, pqcrypto.NewDeterministicDevSigner(id))
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	cert, err := protocol.FormStageQuorumCertificate(height, round, protocol.StagePrecommit, blockHash, votes, manifest.QuorumThreshold)
	if err != nil {
		t.Fatal(err)
	}
	cert.MembershipVersion = manifest.MembershipVersion
	cert.ValidatorSetHash = hash
	return protocol.CommitRequest{Block: block, Certificate: cert}
}

func TestNewNodeRejectsUnknownValidator(t *testing.T) {
	t.Setenv(cryptosuite.EnvVar, string(cryptosuite.Dev))
	if _, err := NewNode(Config{ID: "validator-99", ListenAddr: "127.0.0.1:0"}); err == nil {
		t.Fatal("expected unknown validator to fail")
	}
}
