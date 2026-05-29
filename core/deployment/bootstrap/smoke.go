package bootstrap

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
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keithwegner/pq-fabric/consensus/validator"
	apiauth "github.com/keithwegner/pq-fabric/core/auth"
	"github.com/keithwegner/pq-fabric/core/consortium"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	evidencepkg "github.com/keithwegner/pq-fabric/core/evidence"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
	"github.com/keithwegner/pq-fabric/core/storage"
)

const generatedSmokeToken = "bootstrap-smoke-admin-token"

func Smoke(ctx context.Context, spec BootstrapSpec) (BootstrapReport, error) {
	report := Validate(ctx, spec)
	if !report.OK() {
		report.Smoke.Message = "bootstrap validation failed; smoke not run"
		return report, nil
	}
	evidence, checks, err := runGeneratedSmoke(ctx, spec)
	report.Checks = append(report.Checks, checks...)
	report.Smoke = evidence
	report.Status = reportStatus(report.Checks)
	if err != nil {
		return report, err
	}
	return report, nil
}

func runGeneratedSmoke(ctx context.Context, spec BootstrapSpec) (SmokeEvidence, []Check, error) {
	workDir, err := os.MkdirTemp("", "pq-fabric-bootstrap-smoke-*")
	if err != nil {
		return SmokeEvidence{}, nil, err
	}
	evidence := SmokeEvidence{Message: "generated temporary production-pilot material under " + workDir}
	checks := []Check{}
	add := func(name string, ok bool, msg string) {
		status := StatusPass
		if !ok {
			status = StatusFail
		}
		checks = append(checks, Check{Name: name, Status: status, OK: ok, Message: msg})
	}
	fail := func(name string, err error) (SmokeEvidence, []Check, error) {
		add(name, false, err.Error())
		evidence.Message = err.Error()
		return evidence, checks, err
	}

	previousSuite := os.Getenv(cryptosuite.EnvVar)
	if err := os.Setenv(cryptosuite.EnvVar, string(cryptosuite.PQ)); err != nil {
		return fail("smoke_crypto_suite", err)
	}
	defer func() {
		if previousSuite == "" {
			_ = os.Unsetenv(cryptosuite.EnvVar)
		} else {
			_ = os.Setenv(cryptosuite.EnvVar, previousSuite)
		}
	}()

	consortiumID := strings.TrimSpace(spec.ConsortiumID)
	if consortiumID == "" {
		consortiumID = "bootstrap-smoke-consortium"
	}
	publicURLs, listenAddrs, err := smokePeerAddresses()
	if err != nil {
		return fail("smoke_addresses", err)
	}
	selected := cryptosuite.MustLookup(string(cryptosuite.PQ))
	identities, err := identity.ValidatorIdentitiesForSuite(publicURLs, selected)
	if err != nil {
		return fail("smoke_identities", err)
	}
	manifest := consortium.ManifestFromIdentities(consortiumID, 1, 5, identities, identity.DefaultValidatorIDs())
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fail("smoke_manifest_marshal", err)
	}

	fileRoot := filepath.Join(workDir, "file-tree")
	apiKeysPath := mountPath(fileRoot, "/etc/pq-fabric/secrets/api-keys.json")
	manifestPath := mountPath(fileRoot, "/etc/pq-fabric/manifest/current.json")
	historyPath := mountPath(fileRoot, "/etc/pq-fabric/manifest/history/v1.json")
	if err := writeMounted(apiKeysPath, smokeAPIKeyFile()); err != nil {
		return fail("smoke_write_api_keys", err)
	}
	if err := writeMounted(manifestPath, manifestBytes); err != nil {
		return fail("smoke_write_manifest", err)
	}
	if err := writeMounted(historyPath, manifestBytes); err != nil {
		return fail("smoke_write_manifest_history", err)
	}

	tlsAuthority, err := newSmokeTLSAuthority(fileRoot)
	if err != nil {
		return fail("smoke_tls_ca", err)
	}
	for _, id := range identity.DefaultValidatorIDs() {
		if err := tlsAuthority.writePeerCert(consortiumID, id); err != nil {
			return fail("smoke_peer_tls_"+safeName(id), err)
		}
	}
	kmsServer, kmsCAFile, err := newSmokeKMSServer(workDir, selected, identity.DefaultValidatorIDs())
	if err != nil {
		return fail("smoke_kms_server", err)
	}
	defer kmsServer.Close()
	if err := writeMounted(mountPath(fileRoot, "/etc/pq-fabric/kms/ca.crt"), mustRead(kmsCAFile)); err != nil {
		return fail("smoke_write_kms_ca", err)
	}
	if err := writeMounted(mountPath(fileRoot, "/etc/pq-fabric/kms/token"), []byte("generated-smoke-token")); err != nil {
		return fail("smoke_write_kms_token", err)
	}
	if err := writeMounted(mountPath(fileRoot, "/etc/pq-fabric/kms/key-id"), []byte("manifest-signing-key-ref")); err != nil {
		return fail("smoke_write_kms_key", err)
	}

	generatedSpec := generatedSmokeSpec(spec, fileRoot, kmsServer.URL)
	validation := Validate(ctx, generatedSpec)
	add("smoke_generated_secret_validation", validation.OK(), "status="+validation.Status)
	if !validation.OK() {
		return fail("smoke_generated_secret_validation", errors.New("generated secret material failed bootstrap validation"))
	}

	nodes := make([]*validator.Node, 0, len(identity.DefaultValidatorIDs()))
	nodeCtx, cancelNodes := context.WithCancel(ctx)
	defer cancelNodes()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, node := range nodes {
			_ = node.Stop(stopCtx)
			_ = node.Close()
		}
	}()

	for _, id := range identity.DefaultValidatorIDs() {
		cfg := validator.Config{
			ID:                 id,
			ListenAddr:         listenAddrs[id],
			PublicURL:          publicURLs[id],
			PeerURLs:           publicURLs,
			Threshold:          5,
			RequestTimeout:     2 * time.Second,
			ProposalTimeout:    2 * time.Second,
			VoteTimeout:        2 * time.Second,
			MaxRounds:          7,
			StorageMode:        storage.ModeSQLite,
			DatabaseURL:        filepath.Join(workDir, "db", id, "validator.db"),
			ProductionMode:     true,
			APIKeysFile:        apiKeysPath,
			ConsortiumManifest: manifestPath,
			ManifestHistory:    historyPath,
			SignerProvider:     "cloud-kms",
			KMSEndpoint:        kmsServer.URL,
			KMSCAFile:          kmsCAFile,
			PeerTLSCertFile:    mountPath(fileRoot, "/etc/pq-fabric/tls/"+id+".crt"),
			PeerTLSKeyFile:     mountPath(fileRoot, "/etc/pq-fabric/tls/"+id+".key"),
			PeerTLSCAFile:      mountPath(fileRoot, "/etc/pq-fabric/tls/ca.crt"),
			LogFormat:          "json",
		}
		node, err := validator.NewNode(cfg)
		if err != nil {
			return fail("smoke_new_node_"+safeName(id), err)
		}
		nodes = append(nodes, node)
		if err := node.Start(nodeCtx); err != nil {
			return fail("smoke_start_node_"+safeName(id), err)
		}
	}
	add("smoke_start_seven_validators", true, "started 7 production-mode validators with peer mTLS, cloud-kms signer, and sqlite storage")

	client, err := smokeAPIClient(mountPath(fileRoot, "/etc/pq-fabric/tls/ca.crt"))
	if err != nil {
		return fail("smoke_api_client", err)
	}
	receipt, err := submitSmokeEvidence(ctx, client, publicURLs["validator-1"])
	if err != nil {
		return fail("smoke_submit_receipt", err)
	}
	evidence.ReceiptID = receipt.ReceiptID
	evidence.EvidenceID = receipt.EvidenceID
	evidence.SignerCount = receipt.SignerCount
	evidence.CommitHeight = receipt.CommitHeight
	add("smoke_submit_receipt", receipt.SignerCount >= 5, fmt.Sprintf("receipt=%s signers=%d", short(receipt.ReceiptID), receipt.SignerCount))

	result, err := verifySmokeReceipt(ctx, client, publicURLs["validator-1"], receipt.ReceiptID)
	if err != nil {
		return fail("smoke_verify_receipt", err)
	}
	evidence.VerificationStatus = result.Status
	add("smoke_verify_receipt", result.Valid, "status="+result.Status+" quorum="+result.QuorumStatus)
	if !result.Valid {
		return fail("smoke_verify_receipt", errors.New(result.Reason))
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	for _, node := range nodes {
		_ = node.Stop(stopCtx)
		_ = node.Close()
	}
	stopCancel()
	nodes = nil
	cancelNodes()

	sourceDB := filepath.Join(workDir, "db", "validator-1", "validator.db")
	restoredDB := filepath.Join(workDir, "restore", "validator-1.db")
	if err := copySQLiteDatabase(sourceDB, restoredDB); err != nil {
		return fail("smoke_restore_copy", err)
	}
	evidence.RestoredDatabase = restoredDB
	restored, err := validator.NewNode(validator.Config{
		ID:                 "validator-1",
		ListenAddr:         listenAddrs["validator-1"],
		PublicURL:          publicURLs["validator-1"],
		PeerURLs:           publicURLs,
		Threshold:          5,
		StorageMode:        storage.ModeSQLite,
		DatabaseURL:        restoredDB,
		ProductionMode:     true,
		APIKeysFile:        apiKeysPath,
		ConsortiumManifest: manifestPath,
		ManifestHistory:    historyPath,
		SignerProvider:     "cloud-kms",
		KMSEndpoint:        kmsServer.URL,
		KMSCAFile:          kmsCAFile,
		PeerTLSCertFile:    mountPath(fileRoot, "/etc/pq-fabric/tls/validator-1.crt"),
		PeerTLSKeyFile:     mountPath(fileRoot, "/etc/pq-fabric/tls/validator-1.key"),
		PeerTLSCAFile:      mountPath(fileRoot, "/etc/pq-fabric/tls/ca.crt"),
		LogFormat:          "json",
	})
	if err != nil {
		return fail("smoke_restore_reopen", err)
	}
	defer restored.Close()
	restoredReceipt, ok, err := restored.EvidenceReceiptByReceiptID(receipt.ReceiptID)
	if err != nil {
		return fail("smoke_restore_lookup", err)
	}
	add("smoke_restore_lookup", ok, "receipt found after sqlite restore")
	if !ok {
		return fail("smoke_restore_lookup", errors.New("receipt missing after sqlite restore"))
	}
	restoredResult := restored.VerifyEvidenceReceipt(restoredReceipt)
	add("smoke_restore_verify", restoredResult.Valid, "status="+restoredResult.Status+" quorum="+restoredResult.QuorumStatus)
	if !restoredResult.Valid {
		return fail("smoke_restore_verify", errors.New(restoredResult.Reason))
	}
	evidence.Message = "temporary seven-validator production-pilot smoke passed"
	return evidence, checks, nil
}

func generatedSmokeSpec(base BootstrapSpec, fileRoot, kmsEndpoint string) BootstrapSpec {
	consortiumID := strings.TrimSpace(base.ConsortiumID)
	if consortiumID == "" {
		consortiumID = "bootstrap-smoke-consortium"
	}
	spec := BootstrapSpec{
		SchemaVersion:       SchemaVersion,
		Profile:             firstNonEmpty(base.Profile, "production-pilot"),
		ConsortiumID:        consortiumID,
		ValidatorCount:      7,
		QuorumThreshold:     5,
		SecretSource:        SecretSourceSpec{Mode: SecretModeFileTree, FileTreeRoot: fileRoot},
		DatabaseURLTemplate: "sqlite:///data/${NODE_ID}/validator.db",
		KMS: KMSBootstrapSpec{
			Provider:      "cloud-kms",
			Endpoint:      kmsEndpoint,
			KeyIDTemplate: "manifest-signing-key-ref",
			CAFile:        "/etc/pq-fabric/kms/ca.crt",
		},
	}
	spec.SecretReferences = pilotSecretReferences()
	return spec
}

func pilotSecretReferences() []SecretReference {
	refs := []SecretReference{
		{Name: "pq-fabric-api-keys", Key: "api-keys.json", MountPath: "/etc/pq-fabric/secrets/api-keys.json", Sensitivity: SensitivityAPIKeys, Required: true},
		{Name: "pq-fabric-consortium-manifest", Key: "current.json", MountPath: "/etc/pq-fabric/manifest/current.json", Sensitivity: SensitivityManifestCurrent, Required: true},
		{Name: "pq-fabric-consortium-manifest", Key: "v1.json", MountPath: "/etc/pq-fabric/manifest/history/v1.json", Sensitivity: SensitivityManifestHistory, Required: true},
		{Name: "pq-fabric-peer-tls", Key: "ca.crt", MountPath: "/etc/pq-fabric/tls/ca.crt", Sensitivity: SensitivityPeerTLSCA, Required: true},
		{Name: "pq-fabric-kms", Key: "token", MountPath: "/etc/pq-fabric/kms/token", Sensitivity: SensitivityKMSToken, Required: true},
		{Name: "pq-fabric-kms-ca", Key: "ca.crt", MountPath: "/etc/pq-fabric/kms/ca.crt", Sensitivity: SensitivityKMSCA, Required: true},
	}
	for _, id := range identity.DefaultValidatorIDs() {
		refs = append(refs,
			SecretReference{Name: "pq-fabric-peer-tls", Key: id + ".crt", MountPath: "/etc/pq-fabric/tls/" + id + ".crt", Sensitivity: SensitivityPeerTLSCert, Required: true, ValidatorID: id},
			SecretReference{Name: "pq-fabric-peer-tls", Key: id + ".key", MountPath: "/etc/pq-fabric/tls/" + id + ".key", Sensitivity: SensitivityPeerTLSKey, Required: true, ValidatorID: id},
		)
	}
	return refs
}

func smokeAPIKeyFile() []byte {
	file := apiauth.APIKeyFile{Keys: []apiauth.APIKeyRecord{{
		ID:           "bootstrap-admin",
		Name:         "Bootstrap Smoke Admin",
		Organization: "bootstrap-smoke",
		TokenHash:    apiauth.HashToken(generatedSmokeToken),
		Roles: []string{
			apiauth.RoleAdminRead,
			apiauth.RoleAnchorRead,
			apiauth.RoleEvidenceRead,
			apiauth.RoleEvidenceSubmit,
			apiauth.RoleEvidenceVerify,
		},
		CreatedAtUnixMilli: time.Now().UnixMilli(),
	}}}
	data, _ := json.MarshalIndent(file, "", "  ")
	return data
}

func smokePeerAddresses() (map[string]string, map[string]string, error) {
	publicURLs := map[string]string{}
	listenAddrs := map[string]string{}
	for _, id := range identity.DefaultValidatorIDs() {
		addr, err := freeLocalAddr()
		if err != nil {
			return nil, nil, err
		}
		listenAddrs[id] = addr
		publicURLs[id] = "https://" + addr
	}
	return publicURLs, listenAddrs, nil
}

func freeLocalAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

type smokeTLSAuthority struct {
	root  string
	ca    *x509.Certificate
	caKey *rsa.PrivateKey
}

func newSmokeTLSAuthority(fileRoot string) (smokeTLSAuthority, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return smokeTLSAuthority{}, err
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "pq-fabric bootstrap smoke CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return smokeTLSAuthority{}, err
	}
	if err := writeMounted(mountPath(fileRoot, "/etc/pq-fabric/tls/ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return smokeTLSAuthority{}, err
	}
	return smokeTLSAuthority{root: fileRoot, ca: template, caKey: key}, nil
}

func (a smokeTLSAuthority) writePeerCert(consortiumID, validatorID string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	uri, err := url.Parse(consortium.ExpectedTLSURISAN(consortiumID, validatorID))
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: validatorID},
		DNSNames:     []string{"localhost", "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		URIs:         []*url.URL{uri},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.ca, &key.PublicKey, a.caKey)
	if err != nil {
		return err
	}
	if err := writeMounted(mountPath(a.root, "/etc/pq-fabric/tls/"+validatorID+".crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return err
	}
	return writeMounted(mountPath(a.root, "/etc/pq-fabric/tls/"+validatorID+".key"), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func newSmokeKMSServer(workDir string, selected cryptosuite.CryptoSuite, validatorIDs []string) (*httptest.Server, string, error) {
	signers := map[string]struct {
		algorithm string
		publicKey []byte
		signer    interface {
			Sign([]byte) ([]byte, error)
		}
	}{}
	for _, id := range validatorIDs {
		signer, err := selected.NewSigner(id)
		if err != nil {
			return nil, "", err
		}
		kemPrivate, err := selected.NewKEMPrivate(id)
		if err != nil {
			return nil, "", err
		}
		keyID := identity.ValidatorKeyID(id, signer.Algorithm(), signer.PublicKey(), selected.KEMAlgorithm, kemPrivate.PublicKey())
		signers[keyID] = struct {
			algorithm string
			publicKey []byte
			signer    interface {
				Sign([]byte) ([]byte, error)
			}
		}{algorithm: signer.Algorithm(), publicKey: signer.PublicKey(), signer: signer}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/keys/", func(w http.ResponseWriter, r *http.Request) {
		keyID, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/v1/keys/"))
		entry, ok := signers[keyID]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"key_id":     keyID,
			"algorithm":  entry.algorithm,
			"public_key": base64.StdEncoding.EncodeToString(entry.publicKey),
		})
	})
	mux.HandleFunc("POST /v1/sign", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			KeyID     string `json:"key_id"`
			Algorithm string `json:"algorithm"`
			Message   string `json:"message"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		entry, ok := signers[req.KeyID]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if req.Algorithm != entry.algorithm {
			http.Error(w, "wrong algorithm", http.StatusBadRequest)
			return
		}
		message, err := base64.StdEncoding.DecodeString(req.Message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		signature, err := entry.signer.Sign(message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"key_id":    req.KeyID,
			"algorithm": entry.algorithm,
			"signature": base64.StdEncoding.EncodeToString(signature),
		})
	})
	server := httptest.NewTLSServer(mux)
	caFile := filepath.Join(workDir, "kms-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		server.Close()
		return nil, "", err
	}
	return server, caFile, nil
}

func smokeAPIClient(caFile string) (*http.Client, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("peer CA file contains no PEM certificates")
	}
	return &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}, nil
}

func submitSmokeEvidence(ctx context.Context, client *http.Client, baseURL string) (evidencepkg.EvidenceReceipt, error) {
	submission := evidencepkg.EvidenceSubmission{
		SchemaVersion:          evidencepkg.SchemaVersion,
		EvidenceCategory:       "bootstrap-smoke",
		ArtifactHash:           messages.HashBytes([]byte("bootstrap smoke artifact")),
		MetadataHash:           messages.HashBytes([]byte("bootstrap smoke metadata")),
		SubmittingOrganization: "bootstrap-smoke",
		IdempotencyKey:         "bootstrap-smoke-" + fmt.Sprint(time.Now().UnixNano()),
	}
	var receipt evidencepkg.EvidenceReceipt
	err := doSmokeJSON(ctx, client, http.MethodPost, baseURL+"/v1/evidence", submission, &receipt)
	return receipt, err
}

func verifySmokeReceipt(ctx context.Context, client *http.Client, baseURL, receiptID string) (evidencepkg.VerificationResult, error) {
	var result evidencepkg.VerificationResult
	req := evidencepkg.VerificationRequest{ReceiptID: receiptID}
	err := doSmokeJSON(ctx, client, http.MethodPost, baseURL+"/v1/verify", req, &result)
	return result, err
}

func doSmokeJSON(ctx context.Context, client *http.Client, method, rawURL string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var lastErr error
	deadline := time.Now().Add(12 * time.Second)
	for {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+generatedSmokeToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode < 300 {
				return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
			}
			response, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			lastErr = fmt.Errorf("%s %s returned %s: %s", method, rawURL, resp.Status, strings.TrimSpace(string(response)))
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return lastErr
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func copySQLiteDatabase(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(source + suffix)
		if err != nil {
			if suffix != "" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := os.WriteFile(destination+suffix, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func mountPath(root, mounted string) string {
	return filepath.Join(root, strings.TrimPrefix(filepath.Clean(mounted), string(filepath.Separator)))
}

func writeMounted(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func mustRead(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
