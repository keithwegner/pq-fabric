package mlkem

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	vectors "github.com/keithwegner/pq-fabric/tests/crypto_vectors"
)

type mlkemVectorCase struct {
	CaseID                   string `json:"case_id"`
	KeyLabel                 string `json:"key_label"`
	EncapsulationSeedHex     string `json:"encapsulation_seed_hex"`
	ExpectedPublicKeySHA256  string `json:"expected_public_key_sha256"`
	ExpectedCiphertextSHA256 string `json:"expected_ciphertext_sha256"`
	ExpectedSharedSecretHex  string `json:"expected_shared_secret_hex"`
}

func TestMLKEMVectorFixtures(t *testing.T) {
	fixture := vectors.LoadFixture[mlkemVectorCase](t, "mlkem768", "encap_decap.json")
	passed := 0
	failed := 0
	for _, tc := range fixture.Cases {
		if runMLKEMVectorCase(t, tc) {
			passed++
		} else {
			failed++
		}
	}
	vectors.Report(t, fixture.Metadata, passed, failed)
}

func runMLKEMVectorCase(t *testing.T, tc mlkemVectorCase) bool {
	t.Helper()
	caseOK := true
	seed, err := vectors.DecodeHex(tc.CaseID, "encapsulation_seed_hex", tc.EncapsulationSeedHex)
	if err != nil {
		t.Error(err)
		return false
	}
	expectedShared, err := vectors.DecodeHex(tc.CaseID, "expected_shared_secret_hex", tc.ExpectedSharedSecretHex)
	if err != nil {
		t.Error(err)
		return false
	}
	private, err := NewDeterministicPrivate(tc.KeyLabel)
	if err != nil {
		t.Errorf("case %s create deterministic private key: %v", tc.CaseID, err)
		return false
	}
	if got := sha256Hex(private.PublicKey()); got != tc.ExpectedPublicKeySHA256 {
		t.Errorf("case %s public key digest mismatch: got %s want %s", tc.CaseID, got, tc.ExpectedPublicKeySHA256)
		caseOK = false
	}
	public, err := NewPublic(private.PublicKey())
	if err != nil {
		t.Errorf("case %s parse public key: %v", tc.CaseID, err)
		return false
	}
	ciphertext, sharedA, err := public.EncapsulateDeterministically(seed)
	if err != nil {
		t.Errorf("case %s deterministic encapsulate: %v", tc.CaseID, err)
		return false
	}
	if got := sha256Hex(ciphertext); got != tc.ExpectedCiphertextSHA256 {
		t.Errorf("case %s ciphertext digest mismatch: got %s want %s", tc.CaseID, got, tc.ExpectedCiphertextSHA256)
		caseOK = false
	}
	if !bytes.Equal(sharedA, expectedShared) {
		t.Errorf("case %s encapsulated shared secret mismatch: got %s want %s", tc.CaseID, hex.EncodeToString(sharedA), tc.ExpectedSharedSecretHex)
		caseOK = false
	}
	sharedB, err := private.Decapsulate(ciphertext)
	if err != nil {
		t.Errorf("case %s decapsulate: %v", tc.CaseID, err)
		return false
	}
	if !bytes.Equal(sharedA, sharedB) {
		t.Errorf("case %s decapsulated shared secret mismatch: got %s want %s", tc.CaseID, hex.EncodeToString(sharedB), hex.EncodeToString(sharedA))
		caseOK = false
	}
	tamperedCiphertext := append([]byte(nil), ciphertext...)
	tamperedCiphertext[0] ^= 0x01
	tamperedShared, err := private.Decapsulate(tamperedCiphertext)
	if err != nil {
		t.Errorf("case %s tampered ciphertext decapsulation should fail closed with fallback shared secret, got error: %v", tc.CaseID, err)
		caseOK = false
	} else if bytes.Equal(tamperedShared, sharedA) {
		t.Errorf("case %s tampered ciphertext recovered original shared secret unexpectedly", tc.CaseID)
		caseOK = false
	}
	if _, err := private.Decapsulate([]byte("malformed")); err == nil {
		t.Errorf("case %s malformed ciphertext accepted unexpectedly", tc.CaseID)
		caseOK = false
	}
	return caseOK
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
