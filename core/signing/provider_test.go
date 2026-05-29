package signing

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pqcrypto "github.com/keithwegner/pq-fabric/core/crypto"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
)

func TestLocalProviderCreatesSuiteSigner(t *testing.T) {
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	result, err := NewSigner(Config{Provider: ProviderLocal, NodeID: "validator-1", Suite: selected})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != ProviderLocal || result.Signer.NodeID() != "validator-1" || result.Signer.Algorithm() != selected.SignatureAlgorithm {
		t.Fatalf("unexpected signer result: provider=%s node=%s algorithm=%s", result.Provider, result.Signer.NodeID(), result.Signer.Algorithm())
	}
}

func TestCloudKMSProviderSignsThroughRemoteEndpoint(t *testing.T) {
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	signer, err := selected.NewSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	server := fakeKMSServer(t, "kms-key-1", signer, false)
	defer server.Close()
	result, err := NewSigner(Config{
		Provider:          ProviderCloudKMS,
		NodeID:            "validator-1",
		Suite:             selected,
		ExpectedAlgorithm: selected.SignatureAlgorithm,
		ExpectedPublicKey: signer.PublicKey(),
		KMS: KMSConfig{
			Endpoint:      server.URL,
			KeyID:         "kms-key-1",
			AllowInsecure: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	signature, err := result.Signer.Sign([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !selected.NewVerifier().Verify(signer.PublicKey(), []byte("hello"), signature) {
		t.Fatal("expected cloud-kms signature to verify against configured public key")
	}
}

func TestCloudKMSProviderRejectsBadStartupConfig(t *testing.T) {
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	signer, err := selected.NewSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSigner(Config{Provider: ProviderCloudKMS, NodeID: "validator-1", Suite: selected}); err == nil {
		t.Fatal("expected missing cloud-kms config to fail")
	}
	if _, err := NewSigner(Config{
		Provider:          ProviderCloudKMS,
		NodeID:            "validator-1",
		Suite:             selected,
		ExpectedAlgorithm: selected.SignatureAlgorithm,
		ExpectedPublicKey: signer.PublicKey(),
		KMS: KMSConfig{
			Endpoint: "http://kms.example.invalid",
			KeyID:    "kms-key-1",
		},
	}); err == nil {
		t.Fatal("expected insecure cloud-kms endpoint to fail by default")
	}
	disabled := fakeKMSServer(t, "kms-key-1", signer, true)
	defer disabled.Close()
	if _, err := NewSigner(Config{
		Provider:          ProviderCloudKMS,
		NodeID:            "validator-1",
		Suite:             selected,
		ExpectedAlgorithm: selected.SignatureAlgorithm,
		ExpectedPublicKey: signer.PublicKey(),
		KMS: KMSConfig{
			Endpoint:      disabled.URL,
			KeyID:         "kms-key-1",
			AllowInsecure: true,
		},
	}); err == nil {
		t.Fatal("expected disabled cloud-kms key to fail")
	}
	other, err := selected.NewSigner("validator-2")
	if err != nil {
		t.Fatal(err)
	}
	wrong := fakeKMSServer(t, "kms-key-1", other, false)
	defer wrong.Close()
	if _, err := NewSigner(Config{
		Provider:          ProviderCloudKMS,
		NodeID:            "validator-1",
		Suite:             selected,
		ExpectedAlgorithm: selected.SignatureAlgorithm,
		ExpectedPublicKey: signer.PublicKey(),
		KMS: KMSConfig{
			Endpoint:      wrong.URL,
			KeyID:         "kms-key-1",
			AllowInsecure: true,
		},
	}); err == nil {
		t.Fatal("expected cloud-kms public key mismatch to fail")
	}
}

func TestUnsupportedProvidersFailClosed(t *testing.T) {
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	for _, provider := range []string{ProviderHSM, "bogus"} {
		_, err := NewSigner(Config{Provider: provider, NodeID: "validator-1", Suite: selected})
		if !errors.Is(err, ErrUnsupportedProvider) {
			t.Fatalf("expected unsupported provider for %s, got %v", provider, err)
		}
	}
}

func fakeKMSServer(t *testing.T, keyID string, signer pqcrypto.Signer, disabled bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/keys/", func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(r.URL.Path, "/v1/keys/")
		if requested != keyID {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(kmsKeyResponse{
			KeyID:     keyID,
			Algorithm: signer.Algorithm(),
			PublicKey: base64.StdEncoding.EncodeToString(signer.PublicKey()),
			Disabled:  disabled,
		})
	})
	mux.HandleFunc("POST /v1/sign", func(w http.ResponseWriter, r *http.Request) {
		var req kmsSignRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
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
		_ = json.NewEncoder(w).Encode(kmsSignResponse{
			KeyID:     keyID,
			Algorithm: signer.Algorithm(),
			Signature: base64.StdEncoding.EncodeToString(signature),
		})
	})
	return httptest.NewServer(mux)
}
