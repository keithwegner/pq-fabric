package mldsa

import "testing"

func TestMLDSASignerRoundTrip(t *testing.T) {
	signer, err := NewDeterministicSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("hello ML-DSA quorum")
	signature, err := signer.Sign(message)
	if err != nil {
		t.Fatal(err)
	}
	if !NewVerifier().Verify(signer.PublicKey(), message, signature) {
		t.Fatal("expected valid ML-DSA-87 signature")
	}
}

func TestMLDSAVerifierRejectsInvalidSignatureWrongKeyAndWrongDigest(t *testing.T) {
	signer, err := NewDeterministicSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewDeterministicSigner("validator-2")
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("block hash")
	signature, err := signer.Sign(message)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewVerifier()
	tamperedSignature := append([]byte(nil), signature...)
	tamperedSignature[0] ^= 0x01
	if verifier.Verify(signer.PublicKey(), message, tamperedSignature) {
		t.Fatal("tampered signature should not verify")
	}
	if verifier.Verify(other.PublicKey(), message, signature) {
		t.Fatal("signature should not verify under the wrong validator key")
	}
	if verifier.Verify(signer.PublicKey(), []byte("different block hash"), signature) {
		t.Fatal("signature should not verify over a different digest")
	}
}

func TestMLDSARejectsMalformedMaterialSafely(t *testing.T) {
	signer, err := NewDeterministicSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign([]byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewVerifier()
	if verifier.Verify([]byte("short"), []byte("message"), signature) {
		t.Fatal("short public key should not verify")
	}
	if verifier.Verify(signer.PublicKey(), []byte("message"), []byte("short")) {
		t.Fatal("short signature should not verify")
	}
	if signer.Algorithm() != Algorithm || verifier.Algorithm() != Algorithm {
		t.Fatal("unexpected ML-DSA metadata")
	}
}
