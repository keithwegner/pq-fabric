package mlkem

import (
	"bytes"
	"testing"

	circlmlkem "github.com/cloudflare/circl/kem/mlkem/mlkem768"
)

func TestMLKEMSharedSecretAgreement(t *testing.T) {
	private, err := NewDeterministicPrivate("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	public, err := NewPublic(private.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, sharedA, err := public.Encapsulate()
	if err != nil {
		t.Fatal(err)
	}
	sharedB, err := private.Decapsulate(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sharedA, sharedB) {
		t.Fatal("ML-KEM shared secrets did not match")
	}
}

func TestMLKEMSharedSecretMismatchForTamperedCiphertext(t *testing.T) {
	private, err := NewDeterministicPrivate("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	public, err := NewPublic(private.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	seed := deterministicSeed("test-encapsulation-seed", circlmlkem.EncapsulationSeedSize)
	ciphertext, sharedA, err := public.EncapsulateDeterministically(seed)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[0] ^= 0x01
	sharedB, err := private.Decapsulate(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sharedA, sharedB) {
		t.Fatal("tampered ML-KEM ciphertext should not recover the original shared secret")
	}
}

func TestMLKEMRejectsMalformedMaterialSafely(t *testing.T) {
	private, err := NewDeterministicPrivate("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	if private.Algorithm() != Algorithm {
		t.Fatal("unexpected private KEM algorithm")
	}
	if _, err := NewPublic([]byte("short")); err == nil {
		t.Fatal("expected malformed public key error")
	}
	if _, err := NewPrivate([]byte("short")); err == nil {
		t.Fatal("expected malformed private key error")
	}
	if _, err := private.Decapsulate([]byte("short")); err == nil {
		t.Fatal("expected malformed ciphertext error")
	}
	public, err := NewPublic(private.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if public.Algorithm() != Algorithm || len(public.Bytes()) == 0 {
		t.Fatal("unexpected public KEM metadata")
	}
}
