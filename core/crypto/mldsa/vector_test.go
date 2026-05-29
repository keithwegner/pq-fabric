package mldsa

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	vectors "github.com/keithwegner/pq-fabric/tests/crypto_vectors"
)

type mldsaVectorCase struct {
	CaseID                  string `json:"case_id"`
	NodeID                  string `json:"node_id"`
	MessageHex              string `json:"message_hex"`
	ExpectedPublicKeySHA256 string `json:"expected_public_key_sha256"`
	ExpectedSignatureSHA256 string `json:"expected_signature_sha256"`
}

func TestMLDSAVectorFixtures(t *testing.T) {
	fixture := vectors.LoadFixture[mldsaVectorCase](t, "mldsa87", "sign_verify.json")
	passed := 0
	failed := 0
	for _, tc := range fixture.Cases {
		if runMLDSAVectorCase(t, tc) {
			passed++
		} else {
			failed++
		}
	}
	vectors.Report(t, fixture.Metadata, passed, failed)
}

func runMLDSAVectorCase(t *testing.T, tc mldsaVectorCase) bool {
	t.Helper()
	caseOK := true
	message, err := vectors.DecodeHex(tc.CaseID, "message_hex", tc.MessageHex)
	if err != nil {
		t.Error(err)
		return false
	}
	signer, err := NewDeterministicSigner(tc.NodeID)
	if err != nil {
		t.Errorf("case %s create signer: %v", tc.CaseID, err)
		return false
	}
	if got := sha256Hex(signer.PublicKey()); got != tc.ExpectedPublicKeySHA256 {
		t.Errorf("case %s public key digest mismatch: got %s want %s", tc.CaseID, got, tc.ExpectedPublicKeySHA256)
		caseOK = false
	}
	signature, err := signer.Sign(message)
	if err != nil {
		t.Errorf("case %s sign: %v", tc.CaseID, err)
		return false
	}
	if got := sha256Hex(signature); got != tc.ExpectedSignatureSHA256 {
		t.Errorf("case %s signature digest mismatch: got %s want %s", tc.CaseID, got, tc.ExpectedSignatureSHA256)
		caseOK = false
	}
	verifier := NewVerifier()
	if !verifier.Verify(signer.PublicKey(), message, signature) {
		t.Errorf("case %s expected valid signature to verify", tc.CaseID)
		caseOK = false
	}
	tamperedSignature := append([]byte(nil), signature...)
	tamperedSignature[0] ^= 0x01
	if verifier.Verify(signer.PublicKey(), message, tamperedSignature) {
		t.Errorf("case %s tampered signature verified unexpectedly", tc.CaseID)
		caseOK = false
	}
	wrongMessage := append([]byte(nil), message...)
	wrongMessage = append(wrongMessage, 0x00)
	if verifier.Verify(signer.PublicKey(), wrongMessage, signature) {
		t.Errorf("case %s signature verified over wrong message unexpectedly", tc.CaseID)
		caseOK = false
	}
	return caseOK
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
