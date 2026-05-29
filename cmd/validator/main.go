package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/validator"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func main() {
	storageMode := flag.String("storage", getenv("STORAGE", storage.ModeMemory), "validator storage backend: memory, durable, or sqlite")
	dataDir := flag.String("data-dir", getenv("DATA_DIR", ""), "validator durable storage data directory")
	databaseURL := flag.String("database-url", getenv("PQFABRIC_DATABASE_URL", ""), "SQLite database file or DSN when --storage sqlite")
	proposalTimeoutMS := flag.Int("proposal-timeout-ms", getenvInt("PROPOSAL_TIMEOUT_MS", 700), "proposal timeout in milliseconds")
	voteTimeoutMS := flag.Int("vote-timeout-ms", getenvInt("VOTE_TIMEOUT_MS", 700), "vote timeout in milliseconds")
	maxRounds := flag.Uint64("max-rounds", uint64(getenvInt("MAX_ROUNDS", len(identity.DefaultValidatorIDs()))), "maximum consensus rounds per height")
	productionMode := flag.Bool("production-mode", getenvBool("PQ_FABRIC_PRODUCTION_MODE", false), "enable production guardrails")
	apiBearerToken := flag.String("api-bearer-token", getenv("API_BEARER_TOKEN", ""), "bearer token required for external /v1 API calls")
	apiKeysFile := flag.String("api-keys-file", getenv("PQFABRIC_API_KEYS_FILE", ""), "JSON file containing hashed scoped API keys")
	rateLimitPerMinute := flag.Int("rate-limit-per-minute", getenvInt("PQFABRIC_RATE_LIMIT_PER_MINUTE", 60), "per-API-key /v1 request limit per minute")
	rateLimitBurst := flag.Int("rate-limit-burst", getenvInt("PQFABRIC_RATE_LIMIT_BURST", 20), "per-API-key /v1 request burst limit")
	consortiumManifest := flag.String("consortium-manifest", getenv("PQFABRIC_CONSORTIUM_MANIFEST", ""), "JSON consortium manifest for validator membership and key fingerprints")
	manifestHistory := flag.String("manifest-history", getenv("PQFABRIC_CONSORTIUM_MANIFEST_HISTORY", ""), "comma-separated historical consortium manifest files for old receipt verification")
	signerProvider := flag.String("signer-provider", getenv("PQFABRIC_SIGNER_PROVIDER", "local"), "signer provider: local, cloud-kms, or hsm")
	allowLocalSigner := flag.Bool("allow-local-signer", getenvBool("PQFABRIC_ALLOW_LOCAL_SIGNER", false), "allow local signer in production mode for controlled pilot/local profiles")
	kmsKeyID := flag.String("kms-key-id", getenv("PQFABRIC_KMS_KEY_ID", ""), "cloud-kms key resource id; defaults to manifest signing_key_ref")
	kmsEndpoint := flag.String("kms-endpoint", getenv("PQFABRIC_KMS_ENDPOINT", ""), "cloud-kms signing endpoint")
	kmsToken := flag.String("kms-token", getenv("PQFABRIC_KMS_TOKEN", ""), "optional cloud-kms bearer token")
	kmsCAFile := flag.String("kms-ca-file", getenv("PQFABRIC_KMS_CA_FILE", ""), "optional CA bundle for cloud-kms HTTPS endpoint")
	kmsAllowInsecure := flag.Bool("kms-allow-insecure", getenvBool("PQFABRIC_KMS_ALLOW_INSECURE", false), "allow non-HTTPS cloud-kms endpoint for local tests only")
	peerTLSCertFile := flag.String("peer-tls-cert-file", getenv("PQFABRIC_PEER_TLS_CERT_FILE", ""), "validator peer mTLS certificate file")
	peerTLSKeyFile := flag.String("peer-tls-key-file", getenv("PQFABRIC_PEER_TLS_KEY_FILE", ""), "validator peer mTLS private key file")
	peerTLSCAFile := flag.String("peer-tls-ca-file", getenv("PQFABRIC_PEER_TLS_CA_FILE", ""), "validator peer mTLS CA bundle file")
	opsListenAddr := flag.String("ops-listen-addr", getenv("PQFABRIC_OPS_LISTEN_ADDR", ""), "optional unauthenticated Kubernetes probe address exposing /livez and /readyz only")
	logFormat := flag.String("log-format", getenv("PQFABRIC_LOG_FORMAT", ""), "log format: text or json; defaults to json in production")
	otelEnabled := flag.Bool("otel-enabled", getenvBool("PQFABRIC_OTEL_ENABLED", false), "enable OpenTelemetry tracing")
	otelServiceName := flag.String("otel-service-name", getenv("PQFABRIC_OTEL_SERVICE_NAME", "pq-fabric-validator"), "OpenTelemetry service name")
	otelEndpoint := flag.String("otel-exporter-otlp-endpoint", getenv("PQFABRIC_OTEL_EXPORTER_OTLP_ENDPOINT", ""), "OTLP/HTTP traces endpoint URL")
	otelHeaders := flag.String("otel-exporter-otlp-headers", getenv("PQFABRIC_OTEL_EXPORTER_OTLP_HEADERS", ""), "comma-separated OTLP headers as key=value pairs")
	otelInsecure := flag.Bool("otel-exporter-otlp-insecure", getenvBool("PQFABRIC_OTEL_EXPORTER_OTLP_INSECURE", false), "allow insecure OTLP/HTTP transport")
	flag.Parse()

	nodeID := getenv("NODE_ID", "validator-1")
	region := getenv("REGION", identity.DefaultRegionFor(nodeID))
	port := getenv("PORT", "8080")
	listenAddr := getenv("LISTEN_ADDR", ":"+port)
	publicURL := getenv("PUBLIC_URL", fmt.Sprintf("http://%s:%s", nodeID, port))
	peerURLs := parsePeers(os.Getenv("PEERS"))
	if len(peerURLs) == 0 {
		for _, id := range identity.DefaultValidatorIDs() {
			peerURLs[id] = fmt.Sprintf("http://%s:%s", id, port)
		}
	}
	threshold, _ := strconv.Atoi(getenv("THRESHOLD", getenv("QUORUM_THRESHOLD", "5")))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	node, err := validator.NewNode(validator.Config{
		ID:                 nodeID,
		Region:             region,
		ListenAddr:         listenAddr,
		OpsListenAddr:      *opsListenAddr,
		PublicURL:          publicURL,
		PeerURLs:           peerURLs,
		Threshold:          threshold,
		RequestTimeout:     700 * time.Millisecond,
		ProposalTimeout:    time.Duration(*proposalTimeoutMS) * time.Millisecond,
		VoteTimeout:        time.Duration(*voteTimeoutMS) * time.Millisecond,
		MaxRounds:          *maxRounds,
		EnableHealthProbe:  true,
		StorageMode:        *storageMode,
		DataDir:            *dataDir,
		DatabaseURL:        *databaseURL,
		ProductionMode:     *productionMode,
		APIBearerToken:     *apiBearerToken,
		APIKeysFile:        *apiKeysFile,
		RateLimitPerMin:    *rateLimitPerMinute,
		RateLimitBurst:     *rateLimitBurst,
		ConsortiumManifest: *consortiumManifest,
		ManifestHistory:    *manifestHistory,
		SignerProvider:     *signerProvider,
		AllowLocalSigner:   *allowLocalSigner,
		KMSKeyID:           *kmsKeyID,
		KMSEndpoint:        *kmsEndpoint,
		KMSToken:           *kmsToken,
		KMSCAFile:          *kmsCAFile,
		KMSAllowInsecure:   *kmsAllowInsecure,
		PeerTLSCertFile:    *peerTLSCertFile,
		PeerTLSKeyFile:     *peerTLSKeyFile,
		PeerTLSCAFile:      *peerTLSCAFile,
		LogFormat:          *logFormat,
		OTELEnabled:        *otelEnabled,
		OTELServiceName:    *otelServiceName,
		OTELExporterURL:    *otelEndpoint,
		OTELHeaders:        *otelHeaders,
		OTELAllowInsecure:  *otelInsecure,
	})
	if err != nil {
		slog.Error("validator startup failed", "error", err)
		os.Exit(1)
	}
	if err := node.Start(ctx); err != nil {
		slog.Error("validator start failed", "error", err)
		os.Exit(1)
	}
	slog.Info("validator started", "node_id", nodeID, "region", region, "listen", listenAddr, "public_url", publicURL, "threshold", threshold)

	if os.Getenv("AUTO_PROPOSE") == "1" {
		interval, _ := strconv.Atoi(getenv("PROPOSE_INTERVAL_SECONDS", "10"))
		go autoPropose(ctx, node, time.Duration(interval)*time.Second)
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = node.Stop(shutdownCtx)
	_ = node.Close()
	slog.Info("validator stopped", "node_id", nodeID)
}

func autoPropose(ctx context.Context, node *validator.Node, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	time.Sleep(3 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, err := node.Propose(ctx, fmt.Sprintf("auto proposal from %s at %s", node.ID(), time.Now().Format(time.RFC3339)))
			if err != nil {
				slog.Error("auto proposal failed", "error", err, "node_id", node.ID())
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func parsePeers(raw string) map[string]string {
	out := make(map[string]string)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimRight(strings.TrimSpace(parts[1]), "/")
	}
	return out
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getenvBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}
