package signing

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	ProviderLocal    = "local"
	ProviderCloudKMS = "cloud-kms"
	ProviderHSM      = "hsm"
)

var ErrUnsupportedProvider = errors.New("unsupported signer provider")

type Config struct {
	Provider          string
	NodeID            string
	Suite             cryptosuite.CryptoSuite
	ExpectedAlgorithm string
	ExpectedPublicKey []byte
	KMS               KMSConfig
}

type KMSConfig struct {
	Endpoint      string
	KeyID         string
	AuthToken     string
	CAFile        string
	AllowInsecure bool
	Timeout       time.Duration
}

type Result struct {
	Provider string
	Signer   pqcrypto.Signer
}

func NewSigner(cfg Config) (Result, error) {
	provider := NormalizeProvider(cfg.Provider)
	switch provider {
	case ProviderLocal:
		signer, err := cfg.Suite.NewSigner(cfg.NodeID)
		if err != nil {
			return Result{}, err
		}
		if err := validateExpectedSigner(cfg, signer.PublicKey(), signer.Algorithm()); err != nil {
			return Result{}, err
		}
		return Result{Provider: provider, Signer: signer}, nil
	case ProviderCloudKMS:
		signer, err := NewCloudKMSSigner(cfg)
		if err != nil {
			return Result{}, err
		}
		return Result{Provider: provider, Signer: signer}, nil
	case ProviderHSM:
		return Result{}, fmt.Errorf("%w: %s provider is a configured extension point and is not implemented in this build", ErrUnsupportedProvider, provider)
	default:
		return Result{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

func NormalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return ProviderLocal
	}
	return provider
}

type CloudKMSSigner struct {
	nodeID    string
	algorithm string
	publicKey []byte
	endpoint  string
	keyID     string
	authToken string
	client    *http.Client
}

type kmsKeyResponse struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
	Disabled  bool   `json:"disabled,omitempty"`
}

type kmsSignRequest struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Message   string `json:"message"`
}

type kmsSignResponse struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

func NewCloudKMSSigner(cfg Config) (*CloudKMSSigner, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("cloud-kms signer requires node id")
	}
	kms := cfg.KMS
	endpoint := strings.TrimRight(strings.TrimSpace(kms.Endpoint), "/")
	if endpoint == "" {
		return nil, errors.New("cloud-kms signer requires endpoint")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid cloud-kms endpoint: %w", err)
	}
	if parsed.Scheme != "https" && !kms.AllowInsecure {
		return nil, errors.New("cloud-kms endpoint must use https unless insecure mode is explicitly allowed")
	}
	keyID := strings.TrimSpace(kms.KeyID)
	if keyID == "" {
		return nil, errors.New("cloud-kms signer requires key id")
	}
	algorithm := strings.TrimSpace(cfg.ExpectedAlgorithm)
	if algorithm == "" {
		algorithm = cfg.Suite.SignatureAlgorithm
	}
	if algorithm == "" {
		return nil, errors.New("cloud-kms signer requires expected algorithm")
	}
	if len(cfg.ExpectedPublicKey) == 0 {
		return nil, errors.New("cloud-kms signer requires expected public key")
	}
	client, err := kmsHTTPClient(kms)
	if err != nil {
		return nil, err
	}
	signer := &CloudKMSSigner{
		nodeID:    cfg.NodeID,
		algorithm: algorithm,
		publicKey: append([]byte(nil), cfg.ExpectedPublicKey...),
		endpoint:  endpoint,
		keyID:     keyID,
		authToken: strings.TrimSpace(kms.AuthToken),
		client:    client,
	}
	if err := signer.validateKey(context.Background()); err != nil {
		return nil, err
	}
	return signer, nil
}

func (s *CloudKMSSigner) NodeID() string    { return s.nodeID }
func (s *CloudKMSSigner) Algorithm() string { return s.algorithm }
func (s *CloudKMSSigner) PublicKey() []byte { return append([]byte(nil), s.publicKey...) }

func (s *CloudKMSSigner) Sign(message []byte) ([]byte, error) {
	ctx, span := otel.Tracer("github.com/keithwegner/pq-fabric/signing").Start(context.Background(), "kms.sign",
		trace.WithAttributes(attribute.String("kms.key_id", s.keyID), attribute.String("signature.algorithm", s.algorithm)),
	)
	defer span.End()
	body := kmsSignRequest{
		KeyID:     s.keyID,
		Algorithm: s.algorithm,
		Message:   base64.StdEncoding.EncodeToString(message),
	}
	var response kmsSignResponse
	if err := s.doJSON(ctx, http.MethodPost, "/v1/sign", body, &response); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if response.KeyID != s.keyID {
		return nil, fmt.Errorf("cloud-kms sign response key id %q does not match requested key %q", response.KeyID, s.keyID)
	}
	if response.Algorithm != s.algorithm {
		return nil, fmt.Errorf("cloud-kms sign response algorithm %q does not match expected %q", response.Algorithm, s.algorithm)
	}
	signature, err := base64.StdEncoding.DecodeString(response.Signature)
	if err != nil {
		return nil, fmt.Errorf("cloud-kms sign response signature is not base64: %w", err)
	}
	if len(signature) == 0 {
		return nil, errors.New("cloud-kms sign response returned empty signature")
	}
	return signature, nil
}

func (s *CloudKMSSigner) validateKey(ctx context.Context) error {
	ctx, span := otel.Tracer("github.com/keithwegner/pq-fabric/signing").Start(ctx, "kms.validate_key",
		trace.WithAttributes(attribute.String("kms.key_id", s.keyID), attribute.String("signature.algorithm", s.algorithm)),
	)
	defer span.End()
	var response kmsKeyResponse
	if err := s.doJSON(ctx, http.MethodGet, "/v1/keys/"+url.PathEscape(s.keyID), nil, &response); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if response.Disabled {
		return fmt.Errorf("cloud-kms key %s is disabled", s.keyID)
	}
	if response.KeyID != s.keyID {
		return fmt.Errorf("cloud-kms key response id %q does not match configured key %q", response.KeyID, s.keyID)
	}
	if response.Algorithm != s.algorithm {
		return fmt.Errorf("cloud-kms key algorithm %q does not match expected %q", response.Algorithm, s.algorithm)
	}
	publicKey, err := base64.StdEncoding.DecodeString(response.PublicKey)
	if err != nil {
		return fmt.Errorf("cloud-kms public key is not base64: %w", err)
	}
	if !bytes.Equal(publicKey, s.publicKey) {
		return fmt.Errorf("cloud-kms public key for %s does not match consortium manifest", s.keyID)
	}
	return nil
}

func (s *CloudKMSSigner) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.endpoint+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("cloud-kms %s %s returned %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func validateExpectedSigner(cfg Config, publicKey []byte, algorithm string) error {
	expectedAlgorithm := strings.TrimSpace(cfg.ExpectedAlgorithm)
	if expectedAlgorithm == "" {
		expectedAlgorithm = cfg.Suite.SignatureAlgorithm
	}
	if expectedAlgorithm != "" && algorithm != expectedAlgorithm {
		return fmt.Errorf("signer algorithm %q does not match expected %q", algorithm, expectedAlgorithm)
	}
	if len(cfg.ExpectedPublicKey) > 0 && !bytes.Equal(publicKey, cfg.ExpectedPublicKey) {
		return errors.New("signer public key does not match expected public key")
	}
	return nil
}

func kmsHTTPClient(cfg KMSConfig) (*http.Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(cfg.CAFile) != "" {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read cloud-kms CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("cloud-kms CA file contains no PEM certificates")
		}
		if transport.TLSClientConfig != nil {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		} else {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}
