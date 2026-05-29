package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/protocol"
	consensusstate "github.com/keithwegner/pq-fabric/consensus/state"
	apiauth "github.com/keithwegner/pq-fabric/core/auth"
	"github.com/keithwegner/pq-fabric/core/consortium"
	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	evidencepkg "github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/observability"
	"github.com/keithwegner/pq-fabric/core/signing"
	"github.com/keithwegner/pq-fabric/core/storage"
	"go.opentelemetry.io/otel/attribute"
)

type Config struct {
	ID                 string
	Region             string
	ListenAddr         string
	OpsListenAddr      string
	PublicURL          string
	PeerURLs           map[string]string
	Threshold          int
	RequestTimeout     time.Duration
	ProposalTimeout    time.Duration
	VoteTimeout        time.Duration
	MaxRounds          uint64
	EnableHealthProbe  bool
	StorageMode        string
	DataDir            string
	DatabaseURL        string
	ProductionMode     bool
	APIBearerToken     string
	APIKeysFile        string
	RateLimitPerMin    int
	RateLimitBurst     int
	ConsortiumManifest string
	ManifestHistory    string
	SignerProvider     string
	AllowLocalSigner   bool
	KMSKeyID           string
	KMSEndpoint        string
	KMSToken           string
	KMSCAFile          string
	KMSAllowInsecure   bool
	PeerTLSCertFile    string
	PeerTLSKeyFile     string
	PeerTLSCAFile      string
	LogFormat          string
	OTELEnabled        bool
	OTELServiceName    string
	OTELExporterURL    string
	OTELHeaders        string
	OTELAllowInsecure  bool
	OTELSync           bool
}

type PeerHealth struct {
	PeerID       string `json:"peer_id"`
	URL          string `json:"url"`
	Healthy      bool   `json:"healthy"`
	LastChecked  int64  `json:"last_checked_unix_milli"`
	LastError    string `json:"last_error,omitempty"`
	LastHeight   uint64 `json:"last_height"`
	LastHash     string `json:"last_hash,omitempty"`
	DetectedDown int64  `json:"detected_down_unix_milli,omitempty"`
}

type Node struct {
	cfg               Config
	signer            pqcrypto.Signer
	verifier          pqcrypto.SignatureVerifier
	identities        map[string]identity.ValidatorIdentity
	store             storage.ValidatorStore
	selfID            identity.ValidatorIdentity
	authn             *apiauth.Authenticator
	limiter           *rateLimiter
	validatorIDs      []string
	manifest          *consortium.Manifest
	manifestHistory   consortium.History
	identitySets      map[string]membershipIdentitySet
	manifestHash      string
	membershipVersion uint64
	signerProvider    string
	peerClient        *http.Client
	peerTLS           *peerTLSConfig
	obs               *observability.Runtime
	logger            *slog.Logger

	proposeMu    sync.Mutex
	mu           sync.RWMutex
	server       *http.Server
	opsServer    *http.Server
	running      bool
	lastHash     string
	commits      []protocol.CommitRequest
	state        *consensusstate.Machine
	currentRound map[uint64]uint64
	lock         protocol.LockState
	votes        map[voteKey]protocol.Vote
	proposals    map[proposalKey]string
	events       []string
	peerHealth   map[string]PeerHealth
	healthCancel context.CancelFunc
}

type membershipIdentitySet struct {
	manifest     consortium.Manifest
	identities   map[string]identity.ValidatorIdentity
	validatorIDs []string
	threshold    int
	hash         string
}

type voteKey struct {
	height uint64
	round  uint64
	stage  string
}

type proposalKey struct {
	height     uint64
	round      uint64
	proposerID string
}

func NewNode(cfg Config) (*Node, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return nil, errors.New("node id is required")
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = 5
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 700 * time.Millisecond
	}
	if cfg.ProposalTimeout <= 0 {
		cfg.ProposalTimeout = cfg.RequestTimeout
	}
	if cfg.VoteTimeout <= 0 {
		cfg.VoteTimeout = cfg.RequestTimeout
	}
	if cfg.Region == "" {
		cfg.Region = identity.DefaultRegionFor(cfg.ID)
	}
	if cfg.PeerURLs == nil {
		cfg.PeerURLs = map[string]string{}
	}
	if cfg.StorageMode == "" {
		cfg.StorageMode = storage.ModeMemory
	}
	publicURLs := make(map[string]string, len(cfg.PeerURLs)+1)
	for id, url := range cfg.PeerURLs {
		publicURLs[id] = strings.TrimRight(url, "/")
	}
	if cfg.PublicURL != "" {
		publicURLs[cfg.ID] = strings.TrimRight(cfg.PublicURL, "/")
	}
	selected, err := cryptosuite.FromEnv()
	if err != nil {
		return nil, err
	}
	if cfg.ProductionMode && selected.Name == cryptosuite.Dev {
		return nil, errors.New("production mode cannot use development crypto suite")
	}
	if cfg.ProductionMode && strings.TrimSpace(cfg.APIKeysFile) == "" {
		return nil, errors.New("production mode requires a scoped API keys file")
	}
	if cfg.ProductionMode && strings.TrimSpace(cfg.ConsortiumManifest) == "" {
		return nil, errors.New("production mode requires a consortium manifest")
	}
	if cfg.ProductionMode && strings.TrimSpace(cfg.ManifestHistory) == "" {
		return nil, errors.New("production mode requires a consortium manifest history")
	}
	if cfg.ProductionMode && cfg.StorageMode != storage.ModeSQLite {
		return nil, errors.New("production mode requires sqlite storage")
	}
	if cfg.ProductionMode && strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, errors.New("production mode requires PQFABRIC_DATABASE_URL")
	}
	if cfg.ProductionMode && (strings.TrimSpace(cfg.PeerTLSCertFile) == "" || strings.TrimSpace(cfg.PeerTLSKeyFile) == "" || strings.TrimSpace(cfg.PeerTLSCAFile) == "") {
		return nil, errors.New("production mode requires peer mTLS cert, key, and CA files")
	}
	signerProvider := signing.NormalizeProvider(cfg.SignerProvider)
	if cfg.ProductionMode && signerProvider == signing.ProviderLocal && !cfg.AllowLocalSigner {
		return nil, errors.New("production mode requires an external signer provider unless local signing is explicitly allowed")
	}
	var authn *apiauth.Authenticator
	if strings.TrimSpace(cfg.APIKeysFile) != "" {
		authn, err = apiauth.LoadAPIKeyFile(cfg.APIKeysFile)
		if err != nil {
			return nil, fmt.Errorf("load API keys file: %w", err)
		}
	}
	var manifest *consortium.Manifest
	var manifestHistory consortium.History
	var manifestHash string
	var membershipVersion uint64
	var identitySets map[string]membershipIdentitySet
	if strings.TrimSpace(cfg.ConsortiumManifest) != "" {
		loaded, err := consortium.LoadManifest(cfg.ConsortiumManifest)
		if err != nil {
			return nil, fmt.Errorf("load consortium manifest: %w", err)
		}
		manifest = &loaded
		for id, url := range manifest.PublicURLs() {
			publicURLs[id] = url
		}
	}
	var identities map[string]identity.ValidatorIdentity
	validatorIDs := identity.DefaultValidatorIDs()
	if manifest != nil {
		manifestHash, err = manifest.Hash()
		if err != nil {
			return nil, err
		}
		membershipVersion = manifest.MembershipVersion
		identities, err = manifest.ActiveIdentities()
		if err != nil {
			return nil, err
		}
		validatorIDs = manifest.ActiveValidatorIDs()
		if cfg.ProductionMode && len(validatorIDs) != len(identity.DefaultValidatorIDs()) {
			return nil, fmt.Errorf("production mode requires %d active validators in consortium manifest, got %d", len(identity.DefaultValidatorIDs()), len(validatorIDs))
		}
		if cfg.ProductionMode && manifest.QuorumThreshold != 5 {
			return nil, fmt.Errorf("production mode requires fixed 5-of-7 threshold, got %d", manifest.QuorumThreshold)
		}
		if strings.TrimSpace(cfg.ManifestHistory) != "" {
			manifestHistory, err = consortium.LoadManifestHistory(cfg.ManifestHistory)
			if err != nil {
				return nil, err
			}
		}
		manifestHistory, err = manifestHistory.WithManifest(*manifest)
		if err != nil {
			return nil, err
		}
		identitySets, err = buildMembershipIdentitySets(manifestHistory)
		if err != nil {
			return nil, err
		}
	} else {
		identities, err = identity.ValidatorIdentitiesForSuite(publicURLs, selected)
		if err != nil {
			return nil, err
		}
	}
	if cfg.MaxRounds == 0 {
		cfg.MaxRounds = uint64(len(validatorIDs))
	}
	selfIdentity, ok := identities[cfg.ID]
	if !ok {
		return nil, fmt.Errorf("validator %s is not active in current identity set", cfg.ID)
	}
	kmsKeyID := cfg.KMSKeyID
	if strings.TrimSpace(kmsKeyID) == "" && manifest != nil {
		if record, ok := manifest.ValidatorByID(cfg.ID); ok {
			kmsKeyID = record.SigningKeyRef
		}
	}
	signerResult, err := signing.NewSigner(signing.Config{
		Provider:          signerProvider,
		NodeID:            cfg.ID,
		Suite:             selected,
		ExpectedAlgorithm: selfIdentity.SignatureAlgorithmName(),
		ExpectedPublicKey: selfIdentity.SignaturePublicKeyBytes(),
		KMS: signing.KMSConfig{
			Endpoint:      cfg.KMSEndpoint,
			KeyID:         kmsKeyID,
			AuthToken:     cfg.KMSToken,
			CAFile:        cfg.KMSCAFile,
			AllowInsecure: cfg.KMSAllowInsecure,
		},
	})
	if err != nil {
		return nil, err
	}
	signer := signerResult.Signer
	if !bytes.Equal(selfIdentity.SignaturePublicKeyBytes(), signer.PublicKey()) {
		return nil, fmt.Errorf("signer public key does not match current manifest identity for %s", cfg.ID)
	}
	if manifest != nil {
		if err := validateLocalKEMIdentity(cfg.ID, selfIdentity, selected); err != nil {
			return nil, err
		}
		if !containsString(validatorIDs, cfg.ID) {
			return nil, fmt.Errorf("validator %s is not active in consortium manifest", cfg.ID)
		}
		cfg.Region = selfIdentity.Region
	}
	if cfg.ProductionMode {
		if err := validateProductionPeerURLs(publicURLs, cfg.ID); err != nil {
			return nil, err
		}
	}
	peerTLS, peerClient, err := loadPeerTLS(cfg)
	if err != nil {
		return nil, err
	}
	store, err := storage.OpenValidatorStore(cfg.StorageMode, cfg.DataDir, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	obs, err := observability.NewRuntime(context.Background(), observability.Config{
		NodeID:             cfg.ID,
		ProductionMode:     cfg.ProductionMode,
		LogFormat:          cfg.LogFormat,
		OTELEnabled:        cfg.OTELEnabled,
		OTELServiceName:    cfg.OTELServiceName,
		OTELExporterURL:    cfg.OTELExporterURL,
		OTELHeaders:        cfg.OTELHeaders,
		OTELAllowInsecure:  cfg.OTELAllowInsecure,
		UseSynchronousOTEL: cfg.OTELSync,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	logger := obs.Logger.With("node_id", cfg.ID)
	peerHealth := make(map[string]PeerHealth)
	for id, url := range publicURLs {
		if id == cfg.ID {
			continue
		}
		peerHealth[id] = PeerHealth{PeerID: id, URL: strings.TrimRight(url, "/")}
	}
	node := &Node{
		cfg:               cfg,
		signer:            signer,
		verifier:          selected.NewVerifier(),
		identities:        identities,
		store:             store,
		selfID:            selfIdentity,
		authn:             authn,
		limiter:           newRateLimiter(cfg.RateLimitPerMin, cfg.RateLimitBurst),
		validatorIDs:      validatorIDs,
		manifest:          manifest,
		manifestHistory:   manifestHistory,
		identitySets:      identitySets,
		manifestHash:      manifestHash,
		membershipVersion: membershipVersion,
		signerProvider:    signerResult.Provider,
		peerClient:        peerClient,
		peerTLS:           peerTLS,
		obs:               obs,
		logger:            logger,
		lastHash:          protocol.GenesisHash,
		state:             consensusstate.NewMachine(),
		currentRound:      make(map[uint64]uint64),
		votes:             make(map[voteKey]protocol.Vote),
		proposals:         make(map[proposalKey]string),
		peerHealth:        peerHealth,
	}
	if err := node.loadPersistedState(); err != nil {
		_ = store.Close()
		_ = obs.Shutdown(context.Background())
		return nil, err
	}
	return node, nil
}

func (n *Node) ID() string { return n.cfg.ID }

func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", n.withPeerAuth(n.handleHealth))
	mux.HandleFunc("GET /ready", n.withPeerAuth(n.handleReady))
	mux.HandleFunc("GET /metrics", n.withPeerAuth(n.handleMetrics))
	mux.HandleFunc("GET /state", n.withPeerAuth(n.handleState))
	mux.HandleFunc("GET /commits", n.withPeerAuth(n.handleCommits))
	mux.HandleFunc("GET /peers", n.withPeerAuth(n.handlePeers))
	mux.HandleFunc("POST /propose", n.withPeerAuth(n.handlePropose))
	mux.HandleFunc("POST /consensus/proposal", n.withPeerAuth(n.handleProposal))
	mux.HandleFunc("POST /consensus/precommit", n.withPeerAuth(n.handlePrecommit))
	mux.HandleFunc("POST /consensus/commit", n.withPeerAuth(n.handleCommit))
	mux.HandleFunc("POST /v1/evidence", n.handleEvidenceSubmit)
	mux.HandleFunc("GET /v1/evidence/", n.handleEvidenceGet)
	mux.HandleFunc("GET /v1/receipts/", n.handleReceiptGet)
	mux.HandleFunc("POST /v1/verify", n.handleVerify)
	mux.HandleFunc("GET /v1/anchors/", n.handleAnchorGet)
	mux.HandleFunc("GET /v1/audit/recent", n.handleAuditRecent)
	mux.HandleFunc("GET /v1/ops/report", n.handleOpsReport)
	server := &http.Server{Addr: n.cfg.ListenAddr, Handler: mux}
	if n.peerTLS != nil {
		server.TLSConfig = n.peerTLS.Server
	}
	listener, err := net.Listen("tcp", n.cfg.ListenAddr)
	if err != nil {
		n.mu.Unlock()
		return fmt.Errorf("listen on %s: %w", n.cfg.ListenAddr, err)
	}
	n.server = server
	n.running = true
	n.appendEventLocked("node started on " + n.cfg.ListenAddr)
	n.mu.Unlock()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = n.Stop(shutdownCtx)
	}()

	go func() {
		serveListener := listener
		if n.peerTLS != nil {
			serveListener = n.peerTLS.Listener(listener)
		}
		if err := server.Serve(serveListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			n.logger.Error("server stopped unexpectedly", "error", err)
			n.mu.Lock()
			if n.server == server && n.running {
				n.running = false
				n.appendEventLocked("server stopped unexpectedly: " + err.Error())
			}
			n.mu.Unlock()
		}
	}()
	if err := n.startOpsServer(ctx); err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		n.mu.Lock()
		if n.server == server {
			n.server = nil
			n.running = false
			n.appendEventLocked("ops probe startup failed: " + err.Error())
		}
		n.mu.Unlock()
		return err
	}

	if n.cfg.EnableHealthProbe {
		healthCtx, cancel := context.WithCancel(context.Background())
		n.mu.Lock()
		n.healthCancel = cancel
		n.mu.Unlock()
		go n.healthLoop(healthCtx)
	}
	return nil
}

func (n *Node) startOpsServer(ctx context.Context) error {
	addr := strings.TrimSpace(n.cfg.OpsListenAddr)
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", n.handleOpsLivez)
	mux.HandleFunc("GET /readyz", n.handleOpsReadyz)
	server := &http.Server{Addr: addr, Handler: mux}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on ops probe address %s: %w", addr, err)
	}
	n.mu.Lock()
	n.opsServer = server
	n.appendEventLocked("ops probe started on " + addr)
	n.mu.Unlock()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			n.logger.Error("ops probe stopped unexpectedly", "error", err)
			n.mu.Lock()
			if n.opsServer == server {
				n.opsServer = nil
				n.appendEventLocked("ops probe stopped unexpectedly: " + err.Error())
			}
			n.mu.Unlock()
		}
	}()
	return nil
}

func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	if !n.running {
		n.mu.Unlock()
		return nil
	}
	if n.healthCancel != nil {
		n.healthCancel()
		n.healthCancel = nil
	}
	server := n.server
	opsServer := n.opsServer
	n.running = false
	n.opsServer = nil
	n.appendEventLocked("node stopped")
	n.mu.Unlock()
	if opsServer != nil {
		_ = opsServer.Shutdown(ctx)
	}
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

func (n *Node) Close() error {
	if n.obs != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = n.obs.Shutdown(shutdownCtx)
	}
	if n.store == nil {
		return nil
	}
	return n.store.Close()
}

func (n *Node) Snapshot() protocol.StateSnapshot {
	n.mu.RLock()
	defer n.mu.RUnlock()
	lastEvent := ""
	if len(n.events) > 0 {
		lastEvent = n.events[len(n.events)-1]
	}
	return protocol.StateSnapshot{
		NodeID:            n.cfg.ID,
		Region:            n.cfg.Region,
		Height:            uint64(len(n.commits)),
		Round:             n.currentRound[uint64(len(n.commits)+1)],
		LastHash:          n.lastHash,
		StateDigest:       n.state.Digest(),
		Lock:              n.lock,
		CommitCount:       len(n.commits),
		Running:           n.running,
		LastEvent:         lastEvent,
		CryptoSigner:      n.signer.Algorithm(),
		SignerProvider:    n.signerProvider,
		MembershipVersion: n.membershipVersion,
		ValidatorSetHash:  n.manifestHash,
	}
}

func (n *Node) PeerHealth() []PeerHealth {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make([]PeerHealth, 0, len(n.peerHealth))
	for _, h := range n.peerHealth {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeerID < out[j].PeerID })
	return out
}

func (n *Node) Events() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]string(nil), n.events...)
}

func (n *Node) Commits() []protocol.CommitRequest {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]protocol.CommitRequest(nil), n.commits...)
}

func (n *Node) SubmitEvidence(ctx context.Context, submission evidencepkg.EvidenceSubmission) (receipt evidencepkg.EvidenceReceipt, err error) {
	submission = evidencepkg.NormalizeSubmission(submission)
	ctx, span := n.obs.Start(ctx, "evidence.submit",
		attribute.String("evidence.category", submission.EvidenceCategory),
		attribute.String("submitting.organization", submission.SubmittingOrganization),
		attribute.Bool("anchor.requested", submission.AnchorRequested),
	)
	defer func() { n.obs.EndSpan(span, err) }()
	if err := evidencepkg.ValidateSubmission(submission); err != nil {
		return evidencepkg.EvidenceReceipt{}, err
	}
	if existing, ok, err := n.store.EvidenceByIdempotencyKey(submission.IdempotencyKey); err != nil {
		n.recordStorageError()
		return evidencepkg.EvidenceReceipt{}, err
	} else if ok {
		n.obs.Metrics.RecordEvidenceSubmission(false)
		return evidencepkg.UnmarshalReceipt(existing.ReceiptJSON)
	}
	evidenceID, err := evidencepkg.EventHash(submission)
	if err != nil {
		return evidencepkg.EvidenceReceipt{}, err
	}
	if existing, ok, err := n.store.EvidenceByID(evidenceID); err != nil {
		n.recordStorageError()
		return evidencepkg.EvidenceReceipt{}, err
	} else if ok {
		n.obs.Metrics.RecordEvidenceSubmission(false)
		return evidencepkg.UnmarshalReceipt(existing.ReceiptJSON)
	}
	payload, err := evidencepkg.SubmissionPayload(submission)
	if err != nil {
		return evidencepkg.EvidenceReceipt{}, err
	}
	commit, err := n.Propose(ctx, payload)
	if err != nil {
		return evidencepkg.EvidenceReceipt{}, err
	}
	receipt, err = evidencepkg.NewReceipt(submission, commit)
	if err != nil {
		return evidencepkg.EvidenceReceipt{}, err
	}
	n.annotateEvidenceReceipt(&receipt)
	if created, existing, err := n.saveEvidenceReceipt(receipt); err != nil {
		return evidencepkg.EvidenceReceipt{}, err
	} else if existing.ReceiptID != "" && existing.ReceiptID != receipt.ReceiptID {
		n.obs.Metrics.RecordEvidenceSubmission(false)
		return evidencepkg.UnmarshalReceipt(existing.ReceiptJSON)
	} else {
		n.obs.Metrics.RecordEvidenceSubmission(created)
	}
	return receipt, nil
}

func (n *Node) EvidenceReceiptByID(evidenceID string) (evidencepkg.EvidenceReceipt, bool, error) {
	record, ok, err := n.store.EvidenceByID(strings.TrimSpace(evidenceID))
	if err != nil || !ok {
		if err != nil {
			n.recordStorageError()
		}
		return evidencepkg.EvidenceReceipt{}, ok, err
	}
	receipt, err := evidencepkg.UnmarshalReceipt(record.ReceiptJSON)
	return receipt, true, err
}

func (n *Node) EvidenceReceiptByReceiptID(receiptID string) (evidencepkg.EvidenceReceipt, bool, error) {
	record, ok, err := n.store.EvidenceByReceiptID(strings.TrimSpace(receiptID))
	if err != nil || !ok {
		if err != nil {
			n.recordStorageError()
		}
		return evidencepkg.EvidenceReceipt{}, ok, err
	}
	receipt, err := evidencepkg.UnmarshalReceipt(record.ReceiptJSON)
	return receipt, true, err
}

func (n *Node) EvidenceReceiptByQCHash(qcHash string) (evidencepkg.EvidenceReceipt, bool, error) {
	record, ok, err := n.store.EvidenceByQCHash(strings.TrimSpace(qcHash))
	if err != nil || !ok {
		if err != nil {
			n.recordStorageError()
		}
		return evidencepkg.EvidenceReceipt{}, ok, err
	}
	receipt, err := evidencepkg.UnmarshalReceipt(record.ReceiptJSON)
	return receipt, true, err
}

func (n *Node) VerifyEvidenceReceipt(receipt evidencepkg.EvidenceReceipt) evidencepkg.VerificationResult {
	return n.verifyEvidenceReceipt(receipt, true)
}

func (n *Node) verifyEvidenceReceipt(receipt evidencepkg.EvidenceReceipt, recordMetrics bool) evidencepkg.VerificationResult {
	memberSet, err := n.membershipSetForReceipt(receipt)
	if err != nil {
		result := evidencepkg.VerificationResult{
			Valid:        false,
			Status:       evidencepkg.VerificationInvalid,
			Reason:       err.Error(),
			ReceiptID:    receipt.ReceiptID,
			EvidenceID:   receipt.EvidenceID,
			QuorumStatus: "invalid",
			AnchorStatus: receipt.AnchorStatus,
			SignerCount:  receipt.SignerCount,
			Threshold:    n.cfg.Threshold,
		}
		if recordMetrics {
			n.obs.Metrics.RecordVerification(false)
		}
		return result
	}
	result := evidencepkg.VerifyReceipt(receipt, memberSet.identities, n.verifier, memberSet.threshold)
	if recordMetrics {
		n.obs.Metrics.RecordVerification(result.Valid)
		if !result.Valid {
			n.obs.Metrics.RecordInvalidSignature()
		}
	}
	return result
}

func (n *Node) Propose(ctx context.Context, payload string) (commit protocol.CommitRequest, err error) {
	n.proposeMu.Lock()
	defer n.proposeMu.Unlock()
	ctx, span := n.obs.Start(ctx, "consensus.propose")
	defer func() {
		n.obs.Metrics.RecordConsensusProposal(err)
		n.obs.EndSpan(span, err)
	}()

	n.mu.RLock()
	height := uint64(len(n.commits) + 1)
	n.mu.RUnlock()

	var lastErr error
	for round := uint64(0); round < n.cfg.MaxRounds; round++ {
		n.markRound(height, round)
		proposerID, err := n.proposerFor(height, round)
		if err != nil {
			return protocol.CommitRequest{}, err
		}
		if proposerID != n.cfg.ID {
			commit, err := n.requestProposalFromProposer(ctx, proposerID, payload)
			if err != nil {
				n.recordPeerError(proposerID, err)
				lastErr = fmt.Errorf("round %d proposer %s unavailable: %w", round, proposerID, err)
				continue
			}
			if err := n.ApplyCommit(commit); err != nil {
				return protocol.CommitRequest{}, err
			}
			return commit, nil
		}
		commit, err := n.proposeRound(ctx, height, round, payload)
		if err != nil {
			lastErr = err
			continue
		}
		return commit, nil
	}
	if lastErr != nil {
		return protocol.CommitRequest{}, lastErr
	}
	return protocol.CommitRequest{}, fmt.Errorf("no proposal reached quorum within %d round(s)", n.cfg.MaxRounds)
}

func (n *Node) proposeRound(ctx context.Context, height, round uint64, payload string) (commit protocol.CommitRequest, err error) {
	ctx, span := n.obs.Start(ctx, "consensus.propose_round",
		attribute.Int64("consensus.height", int64(height)),
		attribute.Int64("consensus.round", int64(round)),
	)
	defer func() { n.obs.EndSpan(span, err) }()
	block, err := n.newBlock(height, round, payload)
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	proposal, err := protocol.SignProposal(block, n.signer)
	if err != nil {
		n.obs.Metrics.RecordSignerError()
		return protocol.CommitRequest{}, err
	}
	blockHash, err := block.Hash()
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	selfPrevote, err := n.voteForProposal(proposal, protocol.StagePrevote, nil)
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	prevotes := []protocol.Vote{selfPrevote}

	for peerID, peerURL := range n.livePeerURLs() {
		if peerID == n.cfg.ID {
			continue
		}
		vote, err := n.requestPrevote(ctx, peerID, peerURL, proposal)
		if err != nil {
			n.recordPeerError(peerID, err)
			continue
		}
		if err := protocol.VerifyVote(vote, n.identities, n.verifier); err != nil {
			n.obs.Metrics.RecordInvalidSignature()
			n.recordPeerError(peerID, err)
			continue
		}
		prevotes = append(prevotes, vote)
	}

	prevoteQC, err := protocol.FormStageQuorumCertificate(block.Height, block.Round, protocol.StagePrevote, blockHash, prevotes, n.cfg.Threshold)
	if err != nil {
		n.appendEvent("proposal failed at height " + fmt.Sprint(block.Height) + " round " + fmt.Sprint(block.Round) + ": " + err.Error())
		return protocol.CommitRequest{}, err
	}
	n.attachMembershipToQC(&prevoteQC)

	selfPrecommit, err := n.voteForProposal(proposal, protocol.StagePrecommit, &prevoteQC)
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	precommits := []protocol.Vote{selfPrecommit}
	for peerID, peerURL := range n.livePeerURLs() {
		if peerID == n.cfg.ID {
			continue
		}
		vote, err := n.requestPrecommit(ctx, peerID, peerURL, protocol.PrecommitRequest{Proposal: proposal, PrevoteQC: prevoteQC})
		if err != nil {
			n.recordPeerError(peerID, err)
			continue
		}
		if err := protocol.VerifyVote(vote, n.identities, n.verifier); err != nil {
			n.obs.Metrics.RecordInvalidSignature()
			n.recordPeerError(peerID, err)
			continue
		}
		precommits = append(precommits, vote)
	}
	precommitQC, err := protocol.FormStageQuorumCertificate(block.Height, block.Round, protocol.StagePrecommit, blockHash, precommits, n.cfg.Threshold)
	if err != nil {
		n.appendEvent("precommit failed at height " + fmt.Sprint(block.Height) + " round " + fmt.Sprint(block.Round) + ": " + err.Error())
		return protocol.CommitRequest{}, err
	}
	n.attachMembershipToQC(&precommitQC)
	commit = protocol.CommitRequest{Block: block, Certificate: precommitQC}
	if err := n.ApplyCommit(commit); err != nil {
		return protocol.CommitRequest{}, err
	}
	n.broadcastCommit(ctx, commit)
	return commit, nil
}

func (n *Node) newBlock(height, round uint64, payload string) (protocol.Block, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if height != uint64(len(n.commits)+1) {
		return protocol.Block{}, fmt.Errorf("expected height %d, got %d", len(n.commits)+1, height)
	}
	transactions := consensusstate.TransactionsFromPayload(payload)
	nextState := n.state.Clone()
	stateDigest, _, err := nextState.Apply(transactions)
	if err != nil {
		return protocol.Block{}, err
	}
	block := protocol.NewRoundBlock(height, round, n.lastHash, payload, n.cfg.ID, stateDigest)
	block.Transactions = transactions
	block.MembershipVersion = n.membershipVersion
	block.ValidatorSetHash = n.manifestHash
	return block, nil
}

func (n *Node) markRound(height, round uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if current := n.currentRound[height]; round > current {
		n.currentRound[height] = round
		n.appendEventLocked(fmt.Sprintf("advanced to height %d round %d proposer=%s", height, round, n.mustProposerFor(height, round)))
	}
}

func (n *Node) proposerFor(height, round uint64) (string, error) {
	return protocol.ProposerFor(height, round, n.validatorIDs)
}

func (n *Node) mustProposerFor(height, round uint64) string {
	proposer, err := n.proposerFor(height, round)
	if err != nil {
		return "unknown"
	}
	return proposer
}

func (n *Node) ApplyCommit(commit protocol.CommitRequest) (err error) {
	commitStart := time.Now()
	if commit.Block.CreatedAtUnixMilli > 0 {
		commitStart = time.UnixMilli(commit.Block.CreatedAtUnixMilli)
	}
	_, span := n.obs.Start(context.Background(), "consensus.apply_commit",
		attribute.Int64("consensus.height", int64(commit.Block.Height)),
		attribute.Int64("consensus.round", int64(commit.Block.Round)),
	)
	defer func() { n.obs.EndSpan(span, err) }()
	if err := n.verifyCommit(commit); err != nil {
		n.obs.Metrics.RecordInvalidSignature()
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	currentHeight := uint64(len(n.commits))
	blockHash, _ := commit.Block.Hash()
	if commit.Block.Height <= currentHeight {
		if commit.Block.Height == 0 || int(commit.Block.Height) > len(n.commits) {
			return fmt.Errorf("invalid historical commit height: %d", commit.Block.Height)
		}
		known := n.commits[commit.Block.Height-1]
		knownHash, _ := known.Block.Hash()
		if knownHash == blockHash {
			return nil
		}
		return fmt.Errorf("conflicting historical commit at height %d", commit.Block.Height)
	}
	if commit.Block.Height != currentHeight+1 {
		return fmt.Errorf("cannot apply height %d at current height %d", commit.Block.Height, currentHeight)
	}
	if commit.Block.Round < n.currentRound[commit.Block.Height] {
		return fmt.Errorf("stale commit round %d for height %d; local round is %d", commit.Block.Round, commit.Block.Height, n.currentRound[commit.Block.Height])
	}
	if commit.Block.PreviousHash != n.lastHash {
		return fmt.Errorf("previous hash mismatch: block=%s local=%s", commit.Block.PreviousHash, n.lastHash)
	}
	nextState := n.state.Clone()
	stateDigest, _, err := nextState.Apply(commit.Block.Transactions)
	if err != nil {
		return err
	}
	if commit.Block.StateDigest != stateDigest {
		return fmt.Errorf("state digest mismatch: block=%s computed=%s", commit.Block.StateDigest, stateDigest)
	}
	n.lock = protocol.LockState{
		Height:             commit.Block.Height,
		Round:              commit.Block.Round,
		BlockHash:          blockHash,
		SourceQCBlockHash:  commit.Certificate.BlockHash,
		SourceQCRound:      commit.Certificate.Round,
		SourceQCStage:      commit.Certificate.Stage,
		SourceVoteCount:    len(commit.Certificate.Votes),
		UpdatedAtUnixMilli: time.Now().UnixMilli(),
	}
	state := n.validatorStateLocked(commit.Block.Height, commit.Block.Round, blockHash, stateDigest)
	record, err := n.commitRecord(commit, blockHash)
	if err != nil {
		return err
	}
	if err := n.store.SaveCommit(record, state); err != nil {
		n.recordStorageError()
		return err
	}
	n.commits = append(n.commits, commit)
	n.lastHash = blockHash
	n.state = nextState
	n.currentRound[commit.Block.Height+1] = 0
	if err := n.persistEvidenceReceiptFromCommitLocked(commit); err != nil {
		n.appendEventLocked("evidence receipt persistence failed: " + err.Error())
	}
	n.obs.Metrics.RecordCommit(commitStart, len(commit.Certificate.Votes))
	n.appendEventLocked(fmt.Sprintf("committed height %d round %d with %d/%d precommits state=%s", commit.Block.Height, commit.Block.Round, len(commit.Certificate.Votes), len(n.identities), shortDigest(stateDigest)))
	return nil
}

func (n *Node) CatchUpFromPeers(ctx context.Context) error {
	best := n.Commits()
	for peerID, peerURL := range n.livePeerURLs() {
		commits, err := n.fetchCommits(ctx, peerURL)
		if err != nil {
			n.recordPeerError(peerID, err)
			continue
		}
		if len(commits) <= len(best) {
			continue
		}
		if err := n.validateCatchUpCandidate(commits); err != nil {
			n.recordPeerError(peerID, err)
			continue
		}
		best = commits
	}
	localHeight := len(n.Commits())
	for i := localHeight; i < len(best); i++ {
		if err := n.ApplyCommit(best[i]); err != nil {
			return fmt.Errorf("catch-up failed at commit index %d: %w", i, err)
		}
	}
	if len(best) == localHeight {
		n.appendEvent("catch-up checked peers; no newer commits found")
	} else {
		n.appendEvent(fmt.Sprintf("catch-up applied %d commit(s)", len(best)-localHeight))
	}
	return nil
}

func (n *Node) validateCatchUpCandidate(commits []protocol.CommitRequest) error {
	n.mu.RLock()
	localCommits := append([]protocol.CommitRequest(nil), n.commits...)
	localLastHash := n.lastHash
	stateMachine := n.state.Clone()
	n.mu.RUnlock()
	if len(commits) < len(localCommits) {
		return fmt.Errorf("candidate height %d is behind local height %d", len(commits), len(localCommits))
	}
	for i, localCommit := range localCommits {
		localHash, err := localCommit.Block.Hash()
		if err != nil {
			return err
		}
		candidateHash, err := commits[i].Block.Hash()
		if err != nil {
			return err
		}
		if localHash != candidateHash {
			return fmt.Errorf("candidate diverges from local chain at height %d", i+1)
		}
	}
	previousHash := localLastHash
	for i := len(localCommits); i < len(commits); i++ {
		commit := commits[i]
		expectedHeight := uint64(i + 1)
		if commit.Block.Height != expectedHeight {
			return fmt.Errorf("candidate commit index %d has height %d", i, commit.Block.Height)
		}
		if commit.Block.PreviousHash != previousHash {
			return fmt.Errorf("candidate previous hash mismatch at height %d", commit.Block.Height)
		}
		if err := n.verifyCommit(commit); err != nil {
			return err
		}
		stateDigest, _, err := stateMachine.Apply(commit.Block.Transactions)
		if err != nil {
			return err
		}
		if commit.Block.StateDigest != stateDigest {
			return fmt.Errorf("candidate state digest mismatch at height %d", commit.Block.Height)
		}
		blockHash, err := commit.Block.Hash()
		if err != nil {
			return err
		}
		previousHash = blockHash
	}
	return nil
}

func (n *Node) verifyCommit(commit protocol.CommitRequest) error {
	blockHash, err := commit.Block.Hash()
	if err != nil {
		return err
	}
	if blockHash != commit.Certificate.BlockHash {
		return fmt.Errorf("commit block hash %s does not match certificate %s", blockHash, commit.Certificate.BlockHash)
	}
	if commit.Block.Height != commit.Certificate.Height {
		return fmt.Errorf("commit height %d does not match certificate height %d", commit.Block.Height, commit.Certificate.Height)
	}
	if commit.Block.Round != commit.Certificate.Round {
		return fmt.Errorf("commit round %d does not match certificate round %d", commit.Block.Round, commit.Certificate.Round)
	}
	if commit.Certificate.Stage != protocol.StagePrecommit {
		return fmt.Errorf("commit certificate must be %s, got %s", protocol.StagePrecommit, commit.Certificate.Stage)
	}
	if commit.Certificate.Threshold != n.cfg.Threshold {
		return fmt.Errorf("certificate threshold %d does not match local threshold %d", commit.Certificate.Threshold, n.cfg.Threshold)
	}
	memberSet, err := n.membershipSetForCommit(commit)
	if err != nil {
		return err
	}
	expectedProposer, err := protocol.ProposerFor(commit.Block.Height, commit.Block.Round, memberSet.validatorIDs)
	if err != nil {
		return err
	}
	if commit.Block.ProposerID != expectedProposer {
		return fmt.Errorf("proposer %s is not scheduled for height %d round %d; expected %s", commit.Block.ProposerID, commit.Block.Height, commit.Block.Round, expectedProposer)
	}
	if strings.TrimSpace(commit.Block.StateDigest) == "" {
		return errors.New("commit block state digest is required")
	}
	return protocol.VerifyQuorumCertificate(commit.Certificate, memberSet.identities, n.verifier)
}

func (n *Node) loadPersistedState() error {
	records, err := n.store.ListCommits()
	if err != nil {
		return err
	}
	state, hasState, err := n.store.LoadValidatorState()
	if err != nil {
		return err
	}
	previousHash := protocol.GenesisHash
	stateMachine := consensusstate.NewMachine()
	commits := make([]protocol.CommitRequest, 0, len(records))
	for i, record := range records {
		expectedHeight := uint64(i + 1)
		if record.Height != expectedHeight {
			return fmt.Errorf("durable commit log has height %d at index %d; expected %d", record.Height, i, expectedHeight)
		}
		if record.IdentityKeyID != "" && !n.keyIDKnownForSelf(record.IdentityKeyID) {
			return fmt.Errorf("durable commit at height %d identity key id is not known for %s", record.Height, n.cfg.ID)
		}
		var commit protocol.CommitRequest
		if err := json.Unmarshal(record.CommitJSON, &commit); err != nil {
			return fmt.Errorf("parse durable commit at height %d: %w", record.Height, err)
		}
		if commit.Block.Height != record.Height {
			return fmt.Errorf("durable commit height mismatch: record=%d commit=%d", record.Height, commit.Block.Height)
		}
		blockHash, err := commit.Block.Hash()
		if err != nil {
			return err
		}
		if blockHash != record.BlockHash {
			return fmt.Errorf("durable commit hash mismatch at height %d: record=%s computed=%s", record.Height, record.BlockHash, blockHash)
		}
		if record.StateDigest != "" && record.StateDigest != commit.Block.StateDigest {
			return fmt.Errorf("durable commit state digest mismatch at height %d", record.Height)
		}
		if commit.Block.PreviousHash != previousHash {
			return fmt.Errorf("durable commit previous hash mismatch at height %d", commit.Block.Height)
		}
		if err := n.verifyCommit(commit); err != nil {
			return fmt.Errorf("verify durable commit at height %d: %w", commit.Block.Height, err)
		}
		stateDigest, _, err := stateMachine.Apply(commit.Block.Transactions)
		if err != nil {
			return fmt.Errorf("replay durable state at height %d: %w", commit.Block.Height, err)
		}
		if commit.Block.StateDigest != stateDigest {
			return fmt.Errorf("durable commit state digest invalid at height %d: block=%s computed=%s", commit.Block.Height, commit.Block.StateDigest, stateDigest)
		}
		commits = append(commits, commit)
		previousHash = blockHash
	}
	if hasState {
		if state.NodeID != "" && state.NodeID != n.cfg.ID {
			return fmt.Errorf("durable state node id %s does not match config %s", state.NodeID, n.cfg.ID)
		}
		if state.IdentityKeyID != "" && !n.keyIDKnownForSelf(state.IdentityKeyID) {
			return fmt.Errorf("durable state identity key id is not known for %s", n.cfg.ID)
		}
		if state.Height != uint64(len(commits)) {
			return fmt.Errorf("durable state height %d does not match commit log height %d", state.Height, len(commits))
		}
		if len(commits) == 0 && state.LastHash != "" && state.LastHash != protocol.GenesisHash {
			return fmt.Errorf("durable state has non-genesis hash with empty commit log: %s", state.LastHash)
		}
		if len(commits) > 0 && state.LastHash != previousHash {
			return fmt.Errorf("durable state last hash %s does not match commit log hash %s", state.LastHash, previousHash)
		}
		if state.StateDigest != "" && state.StateDigest != stateMachine.Digest() {
			return fmt.Errorf("durable state digest %s does not match replayed digest %s", state.StateDigest, stateMachine.Digest())
		}
		n.lock = protocol.LockState{Height: state.LockedHeight, Round: state.LockedRound, BlockHash: state.LockedBlockHash}
	}
	n.commits = commits
	n.lastHash = previousHash
	n.state = stateMachine
	if len(commits) > 0 {
		n.appendEventLocked(fmt.Sprintf("loaded %d durable commit(s)", len(commits)))
	}
	return nil
}

func (n *Node) validatorStateLocked(height, round uint64, lastHash, stateDigest string) storage.ValidatorState {
	return storage.ValidatorState{
		NodeID:             n.cfg.ID,
		Region:             n.cfg.Region,
		Height:             height,
		Round:              round,
		LastHash:           lastHash,
		StateDigest:        stateDigest,
		LockedHeight:       n.lock.Height,
		LockedRound:        n.lock.Round,
		LockedBlockHash:    n.lock.BlockHash,
		CommitCount:        int(height),
		IdentityKeyID:      n.selfID.KeyID,
		SignatureAlgorithm: n.selfID.SignatureAlgorithm,
		KEMAlgorithm:       n.selfID.KEMAlgorithm,
		UpdatedAtUnixMilli: time.Now().UnixMilli(),
	}
}

func (n *Node) commitRecord(commit protocol.CommitRequest, blockHash string) (storage.CommitRecord, error) {
	commitJSON, err := json.Marshal(commit)
	if err != nil {
		return storage.CommitRecord{}, err
	}
	certificateJSON, err := json.Marshal(commit.Certificate)
	if err != nil {
		return storage.CommitRecord{}, err
	}
	return storage.CommitRecord{
		Height:             commit.Block.Height,
		Round:              commit.Block.Round,
		BlockHash:          blockHash,
		StateDigest:        commit.Block.StateDigest,
		CommitJSON:         commitJSON,
		CertificateJSON:    certificateJSON,
		IdentityKeyID:      n.selfID.KeyID,
		CreatedAtUnixMilli: time.Now().UnixMilli(),
	}, nil
}

func (n *Node) persistEvidenceReceiptFromCommitLocked(commit protocol.CommitRequest) error {
	submission, ok, err := evidencepkg.SubmissionFromPayload(commit.Block.Payload)
	if err != nil || !ok {
		return err
	}
	receipt, err := evidencepkg.NewReceipt(submission, commit)
	if err != nil {
		return err
	}
	n.annotateEvidenceReceipt(&receipt)
	_, _, err = n.saveEvidenceReceipt(receipt)
	return err
}

func (n *Node) annotateEvidenceReceipt(receipt *evidencepkg.EvidenceReceipt) {
	if receipt == nil {
		return
	}
	if receipt.MembershipVersion == 0 {
		receipt.MembershipVersion = n.membershipVersion
	}
	if receipt.ValidatorSetHash == "" {
		receipt.ValidatorSetHash = n.manifestHash
	}
}

func (n *Node) saveEvidenceReceipt(receipt evidencepkg.EvidenceReceipt) (bool, storage.EvidenceRecord, error) {
	receiptJSON, err := evidencepkg.MarshalReceipt(receipt)
	if err != nil {
		return false, storage.EvidenceRecord{}, err
	}
	record := storage.EvidenceRecord{
		EvidenceID:         receipt.EvidenceID,
		ReceiptID:          receipt.ReceiptID,
		EventHash:          receipt.EventHash,
		QCHash:             receipt.QCHash,
		CommitHeight:       receipt.CommitHeight,
		SubmittingOrg:      receipt.Submission.SubmittingOrganization,
		IdempotencyKey:     receipt.Submission.IdempotencyKey,
		ReceiptJSON:        receiptJSON,
		CreatedAtUnixMilli: receipt.CreatedAtUnixMilli,
	}
	created, existing, err := n.store.SaveEvidence(record)
	if err != nil {
		n.recordStorageError()
	}
	return created, existing, err
}

func (n *Node) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, n.Snapshot())
}

func (n *Node) handleReady(w http.ResponseWriter, _ *http.Request) {
	report := n.ReadinessReport()
	status := http.StatusOK
	if !report.Ready() {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, report)
}

func (n *Node) handleOpsLivez(w http.ResponseWriter, _ *http.Request) {
	snapshot := n.Snapshot()
	status := "live"
	if !snapshot.Running {
		status = "not_live"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                  status,
		"node_id":                 snapshot.NodeID,
		"running":                 snapshot.Running,
		"generated_at_unix_milli": time.Now().UnixMilli(),
	})
}

func (n *Node) handleOpsReadyz(w http.ResponseWriter, _ *http.Request) {
	report := n.ReadinessReport()
	status := http.StatusOK
	if !report.Ready() {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, report)
}

func (n *Node) ReadinessReport() observability.ReadinessReport {
	snapshot := n.Snapshot()
	peers := n.PeerHealth()
	healthyPeers := 0
	for _, peer := range peers {
		if peer.Healthy {
			healthyPeers++
		}
	}
	quorumAvailable := 1+healthyPeers >= n.cfg.Threshold
	n.obs.Metrics.RecordQuorumAvailable(quorumAvailable)
	checks := []observability.ReadinessCheck{
		{Name: "running", OK: snapshot.Running, Message: boolMessage(snapshot.Running, "node is running", "node is not running")},
		{Name: "quorum_available", OK: quorumAvailable, Message: fmt.Sprintf("reachable voting power=%d threshold=%d", 1+healthyPeers, n.cfg.Threshold)},
	}
	if _, err := n.store.ListCommits(); err != nil {
		n.recordStorageError()
		checks = append(checks, observability.ReadinessCheck{Name: "storage", OK: false, Message: err.Error()})
	} else {
		checks = append(checks, observability.ReadinessCheck{Name: "storage", OK: true, Message: "storage reachable"})
	}
	if n.manifest != nil {
		active := containsString(n.validatorIDs, n.cfg.ID)
		checks = append(checks, observability.ReadinessCheck{Name: "active_membership", OK: active, Message: boolMessage(active, "validator is active in current manifest", "validator is not active in current manifest")})
	} else {
		checks = append(checks, observability.ReadinessCheck{Name: "active_membership", OK: !n.cfg.ProductionMode, Message: "no consortium manifest configured"})
	}
	if err := n.checkSigner(); err != nil {
		n.obs.Metrics.RecordSignerError()
		checks = append(checks, observability.ReadinessCheck{Name: "signer", OK: false, Message: err.Error()})
	} else {
		checks = append(checks, observability.ReadinessCheck{Name: "signer", OK: true, Message: "signer reachable"})
	}
	apiAuthOK := !n.cfg.ProductionMode || n.authn != nil
	checks = append(checks, observability.ReadinessCheck{Name: "api_auth_config", OK: apiAuthOK, Message: boolMessage(apiAuthOK, "API auth configured", "production API key file is not loaded")})
	peerTLSOK := !n.cfg.ProductionMode || n.peerTLS != nil
	checks = append(checks, observability.ReadinessCheck{Name: "peer_mtls_config", OK: peerTLSOK, Message: boolMessage(peerTLSOK, "peer mTLS configured", "production peer mTLS is not configured")})
	peerReachable := len(peers) == 0 || healthyPeers > 0
	if n.cfg.ProductionMode {
		peerReachable = healthyPeers > 0
	}
	checks = append(checks, observability.ReadinessCheck{Name: "peer_reachability", OK: peerReachable, Message: fmt.Sprintf("healthy_peers=%d total_peers=%d", healthyPeers, len(peers))})
	return observability.NewReadinessReport(checks)
}

func (n *Node) checkSigner() error {
	challenge := []byte("pq-fabric readiness signer check")
	signature, err := n.signer.Sign(challenge)
	if err != nil {
		return err
	}
	if !n.verifier.Verify(n.selfID.SignaturePublicKeyBytes(), challenge, signature) {
		return errors.New("signer produced a signature that does not verify against the current manifest key")
	}
	return nil
}

func boolMessage(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func (n *Node) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	body, err := n.renderPrometheusMetrics()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body)
}

func (n *Node) renderPrometheusMetrics() (string, error) {
	snapshot := n.Snapshot()
	peers := n.PeerHealth()
	healthyPeers := 0
	for _, peer := range peers {
		if peer.Healthy {
			healthyPeers++
		}
	}
	evidenceRecords, err := n.store.ListEvidence()
	if err != nil {
		n.recordStorageError()
		return "", err
	}
	metrics := n.obs.Metrics.Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "pq_fabric_validator_height %d\n", snapshot.Height)
	fmt.Fprintf(&b, "pq_fabric_validator_commit_count %d\n", snapshot.CommitCount)
	fmt.Fprintf(&b, "pq_fabric_validator_peer_count %d\n", len(peers))
	fmt.Fprintf(&b, "pq_fabric_validator_peer_healthy_count %d\n", healthyPeers)
	fmt.Fprintf(&b, "pq_fabric_readiness_ready %d\n", boolInt(snapshot.Running && 1+healthyPeers >= n.cfg.Threshold))
	for _, peer := range peers {
		healthy := 0
		if peer.Healthy {
			healthy = 1
		}
		lag := int64(snapshot.Height) - int64(peer.LastHeight)
		if lag < 0 {
			lag = 0
		}
		fmt.Fprintf(&b, "pq_fabric_validator_peer_healthy{peer_id=%q} %d\n", peer.PeerID, healthy)
		fmt.Fprintf(&b, "pq_fabric_validator_peer_lag{peer_id=%q} %d\n", peer.PeerID, lag)
	}
	fmt.Fprintf(&b, "pq_fabric_evidence_receipts_total %d\n", len(evidenceRecords))
	fmt.Fprintf(&b, "pq_fabric_evidence_submissions_total %d\n", metrics.EvidenceSubmissions)
	fmt.Fprintf(&b, "pq_fabric_evidence_duplicate_submissions_total %d\n", metrics.DuplicateEvidence)
	fmt.Fprintf(&b, "pq_fabric_consensus_proposals_total %d\n", metrics.ConsensusProposals)
	fmt.Fprintf(&b, "pq_fabric_consensus_proposal_failures_total %d\n", metrics.ConsensusProposalFailures)
	fmt.Fprintf(&b, "pq_fabric_consensus_commits_total %d\n", metrics.ConsensusCommits)
	fmt.Fprintf(&b, "pq_fabric_consensus_commit_latency_millis_total %d\n", metrics.CommitLatencyMillisTotal)
	fmt.Fprintf(&b, "pq_fabric_consensus_last_commit_latency_millis %d\n", metrics.LastCommitLatencyMillis)
	fmt.Fprintf(&b, "pq_fabric_quorum_last_signer_count %d\n", metrics.LastQuorumSignerCount)
	fmt.Fprintf(&b, "pq_fabric_quorum_available %d\n", boolInt(1+healthyPeers >= n.cfg.Threshold))
	fmt.Fprintf(&b, "pq_fabric_storage_errors_total %d\n", metrics.StorageErrors)
	fmt.Fprintf(&b, "pq_fabric_signer_errors_total %d\n", metrics.SignerErrors)
	fmt.Fprintf(&b, "pq_fabric_invalid_signatures_total %d\n", metrics.InvalidSignatures)
	fmt.Fprintf(&b, "pq_fabric_peer_probe_successes_total %d\n", metrics.PeerProbeSuccesses)
	fmt.Fprintf(&b, "pq_fabric_peer_probe_failures_total %d\n", metrics.PeerProbeFailures)
	for _, key := range observability.SortedMetricKeys(metrics.APIRequests) {
		method, path, status := splitMetricKey(key)
		fmt.Fprintf(&b, "pq_fabric_api_requests_total{method=%q,path=%q,status=%q} %d\n", method, path, status, metrics.APIRequests[key])
		fmt.Fprintf(&b, "pq_fabric_api_request_duration_millis_total{method=%q,path=%q,status=%q} %d\n", method, path, status, metrics.APIRequestDurationMillis[key])
	}
	for _, key := range observability.SortedMetricKeys(metrics.VerificationResults) {
		fmt.Fprintf(&b, "pq_fabric_receipt_verifications_total{result=%q} %d\n", key, metrics.VerificationResults[key])
	}
	for _, key := range observability.SortedMetricKeys(metrics.AnchorStatuses) {
		fmt.Fprintf(&b, "pq_fabric_anchor_status_total{status=%q} %d\n", key, metrics.AnchorStatuses[key])
	}
	return b.String(), nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func splitMetricKey(key string) (string, string, string) {
	parts := strings.Split(key, "|")
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2]
}

func (n *Node) handleState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, n.Snapshot())
}

func (n *Node) handleCommits(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, n.Commits())
}

func (n *Node) handlePeers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, n.PeerHealth())
}

func (n *Node) handlePropose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Payload string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Payload) == "" {
		req.Payload = fmt.Sprintf("manual proposal from %s at %s", n.cfg.ID, time.Now().Format(time.RFC3339))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	commit, err := n.Propose(ctx, req.Payload)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, commit)
}

func (n *Node) handleEvidenceSubmit(w http.ResponseWriter, r *http.Request) {
	n.withExternalAPI(apiauth.RoleEvidenceSubmit, n.handleEvidenceSubmitAuthorized)(w, r)
}

func (n *Node) handleEvidenceSubmitAuthorized(w http.ResponseWriter, r *http.Request) {
	var submission evidencepkg.EvidenceSubmission
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&submission); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	receipt, err := n.SubmitEvidence(ctx, submission)
	if err != nil {
		status := http.StatusBadRequest
		if !errors.Is(err, evidencepkg.ErrInvalid) {
			status = http.StatusBadGateway
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (n *Node) handleEvidenceGet(w http.ResponseWriter, r *http.Request) {
	n.withExternalAPI(apiauth.RoleEvidenceRead, n.handleEvidenceGetAuthorized)(w, r)
}

func (n *Node) handleEvidenceGetAuthorized(w http.ResponseWriter, r *http.Request) {
	evidenceID := strings.TrimPrefix(r.URL.Path, "/v1/evidence/")
	if strings.TrimSpace(evidenceID) == "" || evidenceID == r.URL.Path {
		writeError(w, http.StatusBadRequest, errors.New("evidence id required"))
		return
	}
	receipt, ok, err := n.EvidenceReceiptByID(evidenceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, storage.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (n *Node) handleReceiptGet(w http.ResponseWriter, r *http.Request) {
	n.withExternalAPI(apiauth.RoleEvidenceRead, n.handleReceiptGetAuthorized)(w, r)
}

func (n *Node) handleReceiptGetAuthorized(w http.ResponseWriter, r *http.Request) {
	receiptID := strings.TrimPrefix(r.URL.Path, "/v1/receipts/")
	if strings.TrimSpace(receiptID) == "" || receiptID == r.URL.Path {
		writeError(w, http.StatusBadRequest, errors.New("receipt id required"))
		return
	}
	receipt, ok, err := n.EvidenceReceiptByReceiptID(receiptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, storage.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (n *Node) handleVerify(w http.ResponseWriter, r *http.Request) {
	n.withExternalAPI(apiauth.RoleEvidenceVerify, n.handleVerifyAuthorized)(w, r)
}

func (n *Node) handleVerifyAuthorized(w http.ResponseWriter, r *http.Request) {
	var req evidencepkg.VerificationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var receipt evidencepkg.EvidenceReceipt
	var ok bool
	var err error
	switch {
	case req.Receipt != nil:
		receipt = *req.Receipt
		ok = true
	case strings.TrimSpace(req.ReceiptID) != "":
		receipt, ok, err = n.EvidenceReceiptByReceiptID(req.ReceiptID)
	case strings.TrimSpace(req.EvidenceID) != "":
		receipt, ok, err = n.EvidenceReceiptByID(req.EvidenceID)
	default:
		writeError(w, http.StatusBadRequest, errors.New("receipt, receipt_id, or evidence_id required"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, storage.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, n.VerifyEvidenceReceipt(receipt))
}

func (n *Node) handleAnchorGet(w http.ResponseWriter, r *http.Request) {
	n.withExternalAPI(apiauth.RoleAnchorRead, n.handleAnchorGetAuthorized)(w, r)
}

func (n *Node) handleAnchorGetAuthorized(w http.ResponseWriter, r *http.Request) {
	qcHash := strings.TrimPrefix(r.URL.Path, "/v1/anchors/")
	if strings.TrimSpace(qcHash) == "" || qcHash == r.URL.Path {
		writeError(w, http.StatusBadRequest, errors.New("qc hash required"))
		return
	}
	receipt, ok, err := n.EvidenceReceiptByQCHash(qcHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		n.obs.Metrics.RecordAnchorStatus(string(evidencepkg.AnchorUnavailable))
		writeJSON(w, http.StatusOK, evidencepkg.AnchorStatus{
			QCHash:  qcHash,
			Status:  evidencepkg.AnchorUnavailable,
			Message: "no local receipt found for quorum certificate hash",
		})
		return
	}
	status := receipt.AnchorStatus
	if status == "" {
		status = evidencepkg.AnchorNotRequested
	}
	n.obs.Metrics.RecordAnchorStatus(string(status))
	writeJSON(w, http.StatusOK, evidencepkg.AnchorStatus{
		QCHash:            qcHash,
		Status:            status,
		ReceiptID:         receipt.ReceiptID,
		EvidenceID:        receipt.EvidenceID,
		AnchorTransaction: receipt.AnchorTransaction,
		Message:           "testnet anchoring is optional and not required for receipt validity",
	})
}

func (n *Node) handleAuditRecent(w http.ResponseWriter, r *http.Request) {
	n.withExternalAPI(apiauth.RoleAdminRead, n.handleAuditRecentAuthorized)(w, r)
}

func (n *Node) handleAuditRecentAuthorized(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, errors.New("limit must be a positive integer"))
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}
	records, err := n.store.ListAudit(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (n *Node) handleOpsReport(w http.ResponseWriter, r *http.Request) {
	n.withExternalAPI(apiauth.RoleAdminRead, n.handleOpsReportAuthorized)(w, r)
}

func (n *Node) handleOpsReportAuthorized(w http.ResponseWriter, r *http.Request) {
	auditLimit, err := positiveLimit(r, "audit_limit", 50, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	receiptLimit, err := positiveLimit(r, "receipt_limit", 20, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	report, err := n.OperatorReport(auditLimit, receiptLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func positiveLimit(r *http.Request, key string, fallback, max int) (int, error) {
	limit := fallback
	if raw := strings.TrimSpace(r.URL.Query().Get(key)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, fmt.Errorf("%s must be a positive integer", key)
		}
		limit = parsed
	}
	if limit > max {
		limit = max
	}
	return limit, nil
}

func (n *Node) OperatorReport(auditLimit, receiptLimit int) (observability.OperatorReport, error) {
	readiness := n.ReadinessReport()
	recentCommits, err := n.store.RecentCommits(receiptLimit)
	if err != nil {
		n.recordStorageError()
		return observability.OperatorReport{}, err
	}
	recentEvidence, err := n.store.RecentEvidence(receiptLimit)
	if err != nil {
		n.recordStorageError()
		return observability.OperatorReport{}, err
	}
	audit, err := n.store.ListAudit(auditLimit)
	if err != nil {
		n.recordStorageError()
		return observability.OperatorReport{}, err
	}
	commitSummaries, participation := n.commitSummaries(recentCommits)
	receiptSummaries, spotChecks := n.receiptSummaries(recentEvidence)
	return observability.OperatorReport{
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		Node:                 n.Snapshot(),
		Readiness:            readiness,
		PeerHealth:           n.PeerHealth(),
		Metrics:              n.obs.Metrics.Snapshot(),
		RecentCommits:        commitSummaries,
		RecentReceipts:       receiptSummaries,
		RecentAudit:          audit,
		QuorumParticipation:  participation,
		Signer: observability.SignerStatus{
			Provider:  n.signerProvider,
			Algorithm: n.signer.Algorithm(),
			KeyID:     n.selfID.KeyID,
			Status:    "configured",
		},
		VerificationChecks: spotChecks,
	}, nil
}

func (n *Node) commitSummaries(records []storage.CommitRecord) ([]observability.CommitSummary, []observability.QuorumParticipation) {
	summaries := make([]observability.CommitSummary, 0, len(records))
	counts := map[string]int{}
	for _, record := range records {
		var commit protocol.CommitRequest
		if err := json.Unmarshal(record.CommitJSON, &commit); err != nil {
			continue
		}
		for _, vote := range commit.Certificate.Votes {
			counts[vote.VoterID]++
		}
		summaries = append(summaries, observability.CommitSummary{
			Height:             record.Height,
			Round:              record.Round,
			BlockHash:          record.BlockHash,
			StateDigest:        record.StateDigest,
			ProposerID:         commit.Block.ProposerID,
			SignerCount:        len(commit.Certificate.Votes),
			CreatedAtUnixMilli: record.CreatedAtUnixMilli,
		})
	}
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	participation := make([]observability.QuorumParticipation, 0, len(ids))
	for _, id := range ids {
		participation = append(participation, observability.QuorumParticipation{ValidatorID: id, Votes: counts[id]})
	}
	return summaries, participation
}

func (n *Node) receiptSummaries(records []storage.EvidenceRecord) ([]observability.ReceiptSummary, []observability.VerificationSpotCheck) {
	summaries := make([]observability.ReceiptSummary, 0, len(records))
	checks := make([]observability.VerificationSpotCheck, 0, len(records))
	for _, record := range records {
		receipt, err := evidencepkg.UnmarshalReceipt(record.ReceiptJSON)
		if err != nil {
			continue
		}
		summaries = append(summaries, observability.ReceiptSummary{
			EvidenceID:         receipt.EvidenceID,
			ReceiptID:          receipt.ReceiptID,
			EventHash:          receipt.EventHash,
			QCHash:             receipt.QCHash,
			CommitHeight:       receipt.CommitHeight,
			SubmittingOrg:      receipt.Submission.SubmittingOrganization,
			SignerCount:        receipt.SignerCount,
			AnchorStatus:       string(receipt.AnchorStatus),
			CreatedAtUnixMilli: receipt.CreatedAtUnixMilli,
		})
		result := n.verifyEvidenceReceipt(receipt, false)
		checks = append(checks, observability.VerificationSpotCheck{
			ReceiptID:    receipt.ReceiptID,
			EvidenceID:   receipt.EvidenceID,
			Valid:        result.Valid,
			Reason:       result.Reason,
			QuorumStatus: result.QuorumStatus,
			AnchorStatus: string(result.AnchorStatus),
		})
	}
	return summaries, checks
}

func (n *Node) withExternalAPI(role string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		route := observability.RoutePattern(r.URL.Path)
		ctx, span := n.obs.Start(r.Context(), "api.request",
			attribute.String("http.method", r.Method),
			attribute.String("http.route", route),
			attribute.String("api.required_role", role),
		)
		r = r.WithContext(ctx)
		recorder := &auditResponseWriter{ResponseWriter: w}
		principal, rateKey, denialStatus, denialReason := n.authorizeExternalAPI(role, r, start)
		if !n.limiter.Allow(rateKey) {
			denialStatus = http.StatusTooManyRequests
			denialReason = "rate limit exceeded"
		}
		if denialStatus != 0 {
			writeError(recorder, denialStatus, errors.New(denialReason))
			n.saveAuditRecord(r, principal, recorder.statusCode(), start, denialReason)
			n.obs.Metrics.RecordAPI(r.Method, r.URL.Path, recorder.statusCode(), time.Since(start))
			span.SetAttributes(attribute.Int("http.status_code", recorder.statusCode()), attribute.String("api.principal_id", principal.ID))
			n.obs.EndSpan(span, errors.New(denialReason))
			return
		}
		handler(recorder, r)
		n.saveAuditRecord(r, principal, recorder.statusCode(), start, "")
		n.obs.Metrics.RecordAPI(r.Method, r.URL.Path, recorder.statusCode(), time.Since(start))
		span.SetAttributes(attribute.Int("http.status_code", recorder.statusCode()), attribute.String("api.principal_id", principal.ID))
		if recorder.statusCode() >= 500 {
			n.obs.EndSpan(span, fmt.Errorf("request returned %d", recorder.statusCode()))
		} else {
			n.obs.EndSpan(span, nil)
		}
	}
}

func (n *Node) authorizeExternalAPI(role string, r *http.Request, now time.Time) (apiauth.Principal, string, int, string) {
	token := bearerToken(r)
	if n.authn != nil {
		principal, err := n.authn.Authenticate(token, now)
		if err != nil {
			return apiauth.Principal{}, "anonymous", http.StatusUnauthorized, "missing or invalid API key"
		}
		if !apiauth.HasRole(principal, role) {
			return principal, "api-key:" + principal.ID, http.StatusForbidden, "API key lacks required role"
		}
		return principal, "api-key:" + principal.ID, 0, ""
	}
	legacy := strings.TrimSpace(n.cfg.APIBearerToken)
	if legacy != "" {
		if token == "" || subtleCompare(token, legacy) != 1 {
			return apiauth.Principal{}, "anonymous", http.StatusUnauthorized, "missing or invalid API bearer token"
		}
		return apiauth.Principal{
			ID:           "legacy-bearer",
			Name:         "legacy bearer token",
			Organization: "local",
			Roles: []string{
				apiauth.RoleEvidenceSubmit,
				apiauth.RoleEvidenceRead,
				apiauth.RoleEvidenceVerify,
				apiauth.RoleAnchorRead,
				apiauth.RoleAdminRead,
			},
			Legacy: true,
		}, "legacy-bearer", 0, ""
	}
	return apiauth.Principal{ID: "anonymous-local", Organization: "local-dev"}, "anonymous-local", 0, ""
}

func (n *Node) saveAuditRecord(r *http.Request, principal apiauth.Principal, status int, start time.Time, denialReason string) {
	if status == 0 {
		status = http.StatusOK
	}
	record := storage.AuditRecord{
		RequestID:          requestID(r, n.cfg.ID, start),
		TimestampUnixMilli: start.UnixMilli(),
		PrincipalID:        principal.ID,
		Organization:       principal.Organization,
		Method:             r.Method,
		Path:               r.URL.Path,
		StatusCode:         status,
		DurationMillis:     time.Since(start).Milliseconds(),
		ClientAddr:         r.RemoteAddr,
		DeniedReason:       denialReason,
	}
	if err := n.store.SaveAudit(record); err != nil {
		n.recordStorageError()
		n.logger.Error("audit persistence failed", "error", err, "request_id", record.RequestID, "path", record.Path)
		n.appendEvent("audit persistence failed: " + err.Error())
	}
}

type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *auditResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func requestID(r *http.Request, nodeID string, start time.Time) string {
	if value := strings.TrimSpace(r.Header.Get("X-Request-ID")); value != "" {
		if len(value) > 128 {
			return value[:128]
		}
		return value
	}
	return fmt.Sprintf("%s-%d", nodeID, start.UnixNano())
}

func subtleCompare(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	result := 0
	for i := 0; i < len(a); i++ {
		result |= int(a[i] ^ b[i])
	}
	if result == 0 {
		return 1
	}
	return 0
}

func (n *Node) handleProposal(w http.ResponseWriter, r *http.Request) {
	var proposal protocol.Proposal
	if err := json.NewDecoder(r.Body).Decode(&proposal); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := protocol.VerifyProposal(proposal, n.identities, n.verifier); err != nil {
		n.obs.Metrics.RecordInvalidSignature()
		writeError(w, http.StatusBadRequest, err)
		return
	}
	vote, err := n.voteForProposal(proposal, protocol.StagePrevote, nil)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, vote)
}

func (n *Node) handlePrecommit(w http.ResponseWriter, r *http.Request) {
	var req protocol.PrecommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := protocol.VerifyProposal(req.Proposal, n.identities, n.verifier); err != nil {
		n.obs.Metrics.RecordInvalidSignature()
		writeError(w, http.StatusBadRequest, err)
		return
	}
	vote, err := n.voteForProposal(req.Proposal, protocol.StagePrecommit, &req.PrevoteQC)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, vote)
}

func (n *Node) voteForProposal(proposal protocol.Proposal, stage string, prevoteQC *protocol.QuorumCertificate) (protocol.Vote, error) {
	block := proposal.Block
	blockHash, err := block.Hash()
	if err != nil {
		return protocol.Vote{}, err
	}
	if stage == protocol.StagePrecommit {
		if prevoteQC == nil {
			return protocol.Vote{}, errors.New("precommit requires a prevote quorum certificate")
		}
		if prevoteQC.Height != block.Height || prevoteQC.Round != block.Round || prevoteQC.Stage != protocol.StagePrevote || prevoteQC.BlockHash != blockHash {
			return protocol.Vote{}, errors.New("prevote quorum certificate does not match proposal")
		}
		if err := n.validateCurrentMembership(prevoteQC.MembershipVersion, prevoteQC.ValidatorSetHash); err != nil {
			return protocol.Vote{}, err
		}
		if err := protocol.VerifyQuorumCertificate(*prevoteQC, n.identities, n.verifier); err != nil {
			return protocol.Vote{}, err
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.validateProposalLocked(proposal, blockHash); err != nil {
		return protocol.Vote{}, err
	}
	if err := n.checkLockLocked(block.Height, block.Round, blockHash); err != nil {
		return protocol.Vote{}, err
	}
	key := voteKey{height: block.Height, round: block.Round, stage: stage}
	if existing, ok := n.votes[key]; ok {
		if existing.BlockHash == blockHash {
			return existing, nil
		}
		return protocol.Vote{}, fmt.Errorf("already voted for a different block at height %d round %d stage %s", block.Height, block.Round, stage)
	}
	vote, err := protocol.SignStageVote(block.Height, block.Round, stage, blockHash, n.cfg.ID, n.signer)
	if err != nil {
		n.obs.Metrics.RecordSignerError()
		return protocol.Vote{}, err
	}
	n.votes[key] = vote
	if stage == protocol.StagePrecommit && prevoteQC != nil {
		n.lock = protocol.LockState{
			Height:             block.Height,
			Round:              block.Round,
			BlockHash:          blockHash,
			SourceQCBlockHash:  prevoteQC.BlockHash,
			SourceQCRound:      prevoteQC.Round,
			SourceQCStage:      prevoteQC.Stage,
			SourceVoteCount:    len(prevoteQC.Votes),
			UpdatedAtUnixMilli: time.Now().UnixMilli(),
		}
	}
	n.appendEventLocked(fmt.Sprintf("%s vote for height %d round %d proposed by %s", stage, block.Height, block.Round, block.ProposerID))
	return vote, nil
}

func (n *Node) validateProposalLocked(proposal protocol.Proposal, blockHash string) error {
	block := proposal.Block
	expectedHeight := uint64(len(n.commits) + 1)
	if block.Height < expectedHeight {
		return fmt.Errorf("stale proposal height %d; expected %d", block.Height, expectedHeight)
	}
	if block.Height > expectedHeight {
		return fmt.Errorf("future proposal height %d; expected %d", block.Height, expectedHeight)
	}
	if block.Round < n.currentRound[block.Height] {
		return fmt.Errorf("stale proposal round %d for height %d; local round is %d", block.Round, block.Height, n.currentRound[block.Height])
	}
	expectedProposer, err := n.proposerFor(block.Height, block.Round)
	if err != nil {
		return err
	}
	if block.ProposerID != expectedProposer {
		return fmt.Errorf("proposal proposer %s is not scheduled for height %d round %d; expected %s", block.ProposerID, block.Height, block.Round, expectedProposer)
	}
	if block.PreviousHash != n.lastHash {
		return fmt.Errorf("expected previous hash %s, got %s", n.lastHash, block.PreviousHash)
	}
	if err := n.validateCurrentMembership(block.MembershipVersion, block.ValidatorSetHash); err != nil {
		return err
	}
	nextState := n.state.Clone()
	stateDigest, _, err := nextState.Apply(block.Transactions)
	if err != nil {
		return err
	}
	if block.StateDigest != stateDigest {
		return fmt.Errorf("proposal state digest mismatch: block=%s computed=%s", block.StateDigest, stateDigest)
	}
	key := proposalKey{height: block.Height, round: block.Round, proposerID: block.ProposerID}
	if existing, ok := n.proposals[key]; ok && existing != blockHash {
		return fmt.Errorf("conflicting proposal from %s at height %d round %d", block.ProposerID, block.Height, block.Round)
	}
	n.proposals[key] = blockHash
	if block.Round > n.currentRound[block.Height] {
		n.currentRound[block.Height] = block.Round
	}
	return nil
}

func (n *Node) checkLockLocked(height, round uint64, blockHash string) error {
	if n.lock.Height == 0 || n.lock.Height != height {
		return nil
	}
	if n.lock.BlockHash == blockHash {
		return nil
	}
	if round <= n.lock.Round {
		return fmt.Errorf("locked on block %s at height %d round %d", n.lock.BlockHash, n.lock.Height, n.lock.Round)
	}
	return fmt.Errorf("conservative lock prevents voting for conflicting block %s at height %d round %d", blockHash, height, round)
}

func (n *Node) handleCommit(w http.ResponseWriter, r *http.Request) {
	var commit protocol.CommitRequest
	if err := json.NewDecoder(r.Body).Decode(&commit); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := n.ApplyCommit(commit); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, n.Snapshot())
}

func (n *Node) requestPrevote(ctx context.Context, peerID, peerURL string, proposal protocol.Proposal) (protocol.Vote, error) {
	ctx, span := n.obs.Start(ctx, "peer.request_prevote", attribute.String("peer.id", peerID))
	defer func() { span.End() }()
	body, err := json.Marshal(proposal)
	if err != nil {
		return protocol.Vote{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, n.cfg.VoteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, peerURL+"/consensus/proposal", bytes.NewReader(body))
	if err != nil {
		return protocol.Vote{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	n.obs.Inject(req)
	resp, err := n.peerHTTPClient().Do(req)
	if err != nil {
		return protocol.Vote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return protocol.Vote{}, fmt.Errorf("peer %s returned %s: %s", peerID, resp.Status, strings.TrimSpace(string(msg)))
	}
	var vote protocol.Vote
	if err := json.NewDecoder(resp.Body).Decode(&vote); err != nil {
		return protocol.Vote{}, err
	}
	n.recordPeerHealthy(peerID, 0, "")
	return vote, nil
}

func (n *Node) requestPrecommit(ctx context.Context, peerID, peerURL string, reqBody protocol.PrecommitRequest) (protocol.Vote, error) {
	ctx, span := n.obs.Start(ctx, "peer.request_precommit", attribute.String("peer.id", peerID))
	defer func() { span.End() }()
	body, err := json.Marshal(reqBody)
	if err != nil {
		return protocol.Vote{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, n.cfg.VoteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, peerURL+"/consensus/precommit", bytes.NewReader(body))
	if err != nil {
		return protocol.Vote{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	n.obs.Inject(req)
	resp, err := n.peerHTTPClient().Do(req)
	if err != nil {
		return protocol.Vote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return protocol.Vote{}, fmt.Errorf("peer %s returned %s: %s", peerID, resp.Status, strings.TrimSpace(string(msg)))
	}
	var vote protocol.Vote
	if err := json.NewDecoder(resp.Body).Decode(&vote); err != nil {
		return protocol.Vote{}, err
	}
	n.recordPeerHealthy(peerID, 0, "")
	return vote, nil
}

func (n *Node) requestProposalFromProposer(ctx context.Context, proposerID, payload string) (protocol.CommitRequest, error) {
	ctx, span := n.obs.Start(ctx, "peer.request_proposal", attribute.String("peer.id", proposerID))
	defer func() { span.End() }()
	peerURL, ok := n.livePeerURLs()[proposerID]
	if !ok || strings.TrimSpace(peerURL) == "" {
		return protocol.CommitRequest{}, fmt.Errorf("no URL for proposer %s", proposerID)
	}
	body, err := json.Marshal(struct {
		Payload string `json:"payload"`
	}{Payload: payload})
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, n.cfg.ProposalTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, peerURL+"/propose", bytes.NewReader(body))
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	n.obs.Inject(req)
	resp, err := n.peerHTTPClient().Do(req)
	if err != nil {
		return protocol.CommitRequest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return protocol.CommitRequest{}, fmt.Errorf("proposer %s returned %s: %s", proposerID, resp.Status, strings.TrimSpace(string(msg)))
	}
	var commit protocol.CommitRequest
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return protocol.CommitRequest{}, err
	}
	return commit, nil
}

func (n *Node) broadcastCommit(ctx context.Context, commit protocol.CommitRequest) {
	body, err := json.Marshal(commit)
	if err != nil {
		n.appendEvent("failed to marshal commit for broadcast: " + err.Error())
		return
	}
	for peerID, peerURL := range n.livePeerURLs() {
		if peerID == n.cfg.ID {
			continue
		}
		reqCtx, cancel := context.WithTimeout(ctx, n.cfg.RequestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, peerURL+"/consensus/commit", bytes.NewReader(body))
		if err != nil {
			cancel()
			n.recordPeerError(peerID, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		n.obs.Inject(req)
		resp, err := n.peerHTTPClient().Do(req)
		cancel()
		if err != nil {
			n.recordPeerError(peerID, err)
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			n.recordPeerError(peerID, fmt.Errorf("commit broadcast returned %s", resp.Status))
			continue
		}
		n.recordPeerHealthy(peerID, commit.Block.Height, commit.Certificate.BlockHash)
	}
}

func (n *Node) fetchCommits(ctx context.Context, peerURL string) ([]protocol.CommitRequest, error) {
	ctx, span := n.obs.Start(ctx, "peer.fetch_commits")
	defer func() { span.End() }()
	reqCtx, cancel := context.WithTimeout(ctx, n.cfg.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, peerURL+"/commits", nil)
	if err != nil {
		return nil, err
	}
	n.obs.Inject(req)
	resp, err := n.peerHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch commits returned %s", resp.Status)
	}
	var commits []protocol.CommitRequest
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return nil, err
	}
	return commits, nil
}

func (n *Node) livePeerURLs() map[string]string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make(map[string]string)
	for id, ident := range n.identities {
		if ident.PublicURL != "" {
			out[id] = strings.TrimRight(ident.PublicURL, "/")
		}
	}
	for id, url := range n.cfg.PeerURLs {
		if url != "" {
			out[id] = strings.TrimRight(url, "/")
		}
	}
	return out
}

func (n *Node) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(600 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.probePeers(ctx)
		}
	}
}

func (n *Node) probePeers(ctx context.Context) {
	for peerID, peerURL := range n.livePeerURLs() {
		if peerID == n.cfg.ID {
			continue
		}
		reqCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, peerURL+"/health", nil)
		if err != nil {
			cancel()
			n.recordPeerError(peerID, err)
			continue
		}
		n.obs.Inject(req)
		resp, err := n.peerHTTPClient().Do(req)
		cancel()
		if err != nil {
			n.recordPeerError(peerID, err)
			continue
		}
		var snapshot protocol.StateSnapshot
		decodeErr := json.NewDecoder(resp.Body).Decode(&snapshot)
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			n.recordPeerError(peerID, fmt.Errorf("health probe returned %s", resp.Status))
			continue
		}
		if decodeErr != nil {
			n.recordPeerError(peerID, decodeErr)
			continue
		}
		n.recordPeerHealthy(peerID, snapshot.Height, snapshot.LastHash)
	}
}

func (n *Node) recordPeerHealthy(peerID string, height uint64, hash string) {
	n.obs.Metrics.RecordPeerProbe(true)
	n.mu.Lock()
	defer n.mu.Unlock()
	health := n.peerHealth[peerID]
	health.PeerID = peerID
	health.Healthy = true
	health.LastError = ""
	health.LastChecked = time.Now().UnixMilli()
	health.DetectedDown = 0
	if height != 0 {
		health.LastHeight = height
	}
	if hash != "" {
		health.LastHash = hash
	}
	n.peerHealth[peerID] = health
}

func (n *Node) recordPeerError(peerID string, err error) {
	n.obs.Metrics.RecordPeerProbe(false)
	n.mu.Lock()
	defer n.mu.Unlock()
	health := n.peerHealth[peerID]
	health.PeerID = peerID
	health.Healthy = false
	health.LastError = err.Error()
	health.LastChecked = time.Now().UnixMilli()
	if health.DetectedDown == 0 {
		health.DetectedDown = health.LastChecked
		n.appendEventLocked("detected peer down: " + peerID)
	}
	n.peerHealth[peerID] = health
}

func (n *Node) attachMembershipToQC(cert *protocol.QuorumCertificate) {
	if cert == nil {
		return
	}
	cert.MembershipVersion = n.membershipVersion
	cert.ValidatorSetHash = n.manifestHash
}

func (n *Node) validateCurrentMembership(version uint64, hash string) error {
	if n.manifest == nil {
		return nil
	}
	if version != n.membershipVersion || hash != n.manifestHash {
		return fmt.Errorf("message membership version/hash %d/%s does not match current validator set %d/%s", version, hash, n.membershipVersion, n.manifestHash)
	}
	return nil
}

func (n *Node) membershipSetForCommit(commit protocol.CommitRequest) (membershipIdentitySet, error) {
	if commit.Block.MembershipVersion != commit.Certificate.MembershipVersion || commit.Block.ValidatorSetHash != commit.Certificate.ValidatorSetHash {
		return membershipIdentitySet{}, errors.New("commit block and certificate membership metadata differ")
	}
	return n.membershipSet(commit.Block.MembershipVersion, commit.Block.ValidatorSetHash)
}

func (n *Node) membershipSetForReceipt(receipt evidencepkg.EvidenceReceipt) (membershipIdentitySet, error) {
	version := receipt.ValidatorSetHash
	if version == "" {
		version = receipt.Commit.Block.ValidatorSetHash
	}
	membershipVersion := receipt.MembershipVersion
	if membershipVersion == 0 {
		membershipVersion = receipt.Commit.Block.MembershipVersion
	}
	return n.membershipSet(membershipVersion, version)
}

func (n *Node) membershipSet(version uint64, hash string) (membershipIdentitySet, error) {
	if n.manifest == nil {
		return membershipIdentitySet{
			identities:   n.identities,
			validatorIDs: n.validatorIDs,
			threshold:    n.cfg.Threshold,
		}, nil
	}
	if version == 0 || strings.TrimSpace(hash) == "" {
		return membershipIdentitySet{}, errors.New("membership version and validator set hash are required")
	}
	memberSet, ok := n.identitySets[hash]
	if !ok {
		return membershipIdentitySet{}, fmt.Errorf("unknown validator set hash %s", hash)
	}
	if memberSet.manifest.MembershipVersion != version {
		return membershipIdentitySet{}, fmt.Errorf("validator set hash %s belongs to version %d, not %d", hash, memberSet.manifest.MembershipVersion, version)
	}
	return memberSet, nil
}

func (n *Node) keyIDKnownForSelf(keyID string) bool {
	if keyID == "" {
		return true
	}
	if n.selfID.KeyID == keyID {
		return true
	}
	for _, memberSet := range n.identitySets {
		if v, ok := memberSet.identities[n.cfg.ID]; ok && v.KeyID == keyID {
			return true
		}
	}
	return false
}

func (n *Node) peerHTTPClient() *http.Client {
	if n.peerClient != nil {
		return n.peerClient
	}
	return http.DefaultClient
}

func (n *Node) recordStorageError() {
	if n.obs != nil && n.obs.Metrics != nil {
		n.obs.Metrics.RecordStorageError()
	}
}

func (n *Node) appendEvent(message string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.appendEventLocked(message)
}

func (n *Node) appendEventLocked(message string) {
	stamp := time.Now().Format(time.RFC3339Nano)
	n.events = append(n.events, stamp+" "+message)
	if len(n.events) > 100 {
		n.events = n.events[len(n.events)-100:]
	}
}

func shortDigest(digest string) string {
	if len(digest) <= 12 {
		return digest
	}
	return digest[:12]
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func buildMembershipIdentitySets(history consortium.History) (map[string]membershipIdentitySet, error) {
	out := map[string]membershipIdentitySet{}
	for _, manifest := range history.Manifests {
		hash, err := manifest.Hash()
		if err != nil {
			return nil, err
		}
		identities, err := manifest.ActiveIdentities()
		if err != nil {
			return nil, err
		}
		out[hash] = membershipIdentitySet{
			manifest:     manifest,
			identities:   identities,
			validatorIDs: manifest.ActiveValidatorIDs(),
			threshold:    manifest.QuorumThreshold,
			hash:         hash,
		}
	}
	return out, nil
}

func validateLocalKEMIdentity(nodeID string, expected identity.ValidatorIdentity, selected cryptosuite.CryptoSuite) error {
	kemPrivate, err := selected.NewKEMPrivate(nodeID)
	if err != nil {
		return err
	}
	if !bytes.Equal(kemPrivate.PublicKey(), expected.KEMPublicKey) {
		return fmt.Errorf("local KEM public key does not match current manifest identity for %s", nodeID)
	}
	return nil
}

func validateProductionPeerURLs(publicURLs map[string]string, selfID string) error {
	for id, raw := range publicURLs {
		if id == selfID || strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid peer URL for %s: %w", id, err)
		}
		if parsed.Scheme != "https" {
			return fmt.Errorf("production mode requires HTTPS peer URL for %s", id)
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
