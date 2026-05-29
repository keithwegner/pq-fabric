package crypto

import "testing"

func TestDevSignerRoundTrip(t *testing.T) {
	signer := NewDeterministicDevSigner("validator-1")
	message := []byte("hello quorum")
	sig, err := signer.Sign(message)
	if err != nil {
		t.Fatal(err)
	}
	if !(DevSignatureVerifier{}).Verify(signer.PublicKey(), message, sig) {
		t.Fatal("expected valid signature")
	}
}

func TestDevKEMRoundTrip(t *testing.T) {
	priv, err := NewDeterministicDevKEMPrivate("relay-1")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := NewDevKEMPublic(priv.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, sharedA, err := pub.Encapsulate()
	if err != nil {
		t.Fatal(err)
	}
	sharedB, err := priv.Decapsulate(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(sharedA) != string(sharedB) {
		t.Fatal("shared secrets did not match")
	}
}

func TestDevVerifierRejectsMalformedInputs(t *testing.T) {
	signer := NewDeterministicDevSigner("validator-1")
	sig, err := signer.Sign([]byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	if (DevSignatureVerifier{}).Verify([]byte("short"), []byte("message"), sig) {
		t.Fatal("short public key should not verify")
	}
	if (DevSignatureVerifier{}).Verify(signer.PublicKey(), []byte("message"), []byte("short")) {
		t.Fatal("short signature should not verify")
	}
	if signer.NodeID() != "validator-1" || signer.Algorithm() != DevSignatureAlgorithm {
		t.Fatal("unexpected signer metadata")
	}
	if (DevSignatureVerifier{}).Algorithm() != DevSignatureAlgorithm {
		t.Fatal("unexpected verifier algorithm")
	}
}

func TestDevKEMValidationAndMetadata(t *testing.T) {
	randomPriv, err := NewRandomDevKEMPrivate()
	if err != nil {
		t.Fatal(err)
	}
	if randomPriv.Algorithm() != DevKEMAlgorithm {
		t.Fatal("unexpected private KEM algorithm")
	}
	if _, err := randomPriv.Decapsulate(nil); err == nil {
		t.Fatal("expected empty ciphertext error")
	}
	if _, err := NewDevKEMPublic(nil); err == nil {
		t.Fatal("expected empty public key error")
	}
	priv, err := NewDeterministicDevKEMPrivate("relay-1")
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.Public()
	if pub.Algorithm() != DevKEMAlgorithm || len(pub.Bytes()) == 0 {
		t.Fatal("unexpected public KEM metadata")
	}
}
