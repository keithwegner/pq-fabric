package suite

import (
	"testing"

	"github.com/keithwegner/pq-fabric/core/crypto/mldsa"
	"github.com/keithwegner/pq-fabric/core/crypto/mlkem"
)

func TestSuiteSelectorDefaultsToDev(t *testing.T) {
	t.Setenv(EnvVar, "")
	selected, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != Dev {
		t.Fatalf("expected default dev suite, got %s", selected.Name)
	}
	signer, err := selected.NewSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	if signer.Algorithm() != selected.SignatureAlgorithm {
		t.Fatal("dev signer algorithm should match suite metadata")
	}
}

func TestSuiteSelectorSelectsPQ(t *testing.T) {
	t.Setenv(EnvVar, string(PQ))
	selected, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != PQ {
		t.Fatalf("expected pq suite, got %s", selected.Name)
	}
	if selected.SignatureAlgorithm != mldsa.Algorithm || selected.KEMAlgorithm != mlkem.Algorithm {
		t.Fatalf("unexpected PQ suite metadata: %+v", selected)
	}
	signer, err := selected.NewSigner("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := signer.Sign([]byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	if !selected.NewVerifier().Verify(signer.PublicKey(), []byte("message"), signature) {
		t.Fatal("expected PQ suite verifier to accept signer output")
	}
	kemPrivate, err := selected.NewKEMPrivate("validator-1")
	if err != nil {
		t.Fatal(err)
	}
	kemPublic, err := selected.NewKEMPublic(kemPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, sharedA, err := kemPublic.Encapsulate()
	if err != nil {
		t.Fatal(err)
	}
	sharedB, err := kemPrivate.Decapsulate(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(sharedA) != string(sharedB) {
		t.Fatal("expected PQ suite KEM agreement")
	}
}

func TestSuiteSelectorRejectsUnknown(t *testing.T) {
	if _, err := Lookup("unknown"); err == nil {
		t.Fatal("expected unknown suite to fail")
	}
}
