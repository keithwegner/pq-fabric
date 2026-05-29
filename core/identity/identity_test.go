package identity

import (
	"encoding/base64"
	"testing"

	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
)

func TestDefaultValidatorIdentities(t *testing.T) {
	ids := DefaultValidatorIDs()
	if len(ids) != 7 || ids[0] != "validator-1" || ids[6] != "validator-7" {
		t.Fatalf("unexpected validator ids: %+v", ids)
	}
	urls := map[string]string{"validator-1": "http://validator-1:8080"}
	identities := DefaultValidatorIdentities(urls)
	validator := identities["validator-1"]
	if validator.Region != "nyc" || validator.PublicURL != urls["validator-1"] {
		t.Fatalf("unexpected validator identity: %+v", validator)
	}
	if validator.PublicKeyB64() != base64.StdEncoding.EncodeToString(validator.PublicKey) {
		t.Fatal("public key base64 helper mismatch")
	}
	if validator.SignatureAlgorithm == "" || len(validator.SignaturePublicKey) == 0 {
		t.Fatalf("expected signature metadata: %+v", validator)
	}
	if validator.KEMAlgorithm == "" || len(validator.KEMPublicKey) == 0 {
		t.Fatalf("expected KEM metadata: %+v", validator)
	}
	if validator.KeyID == "" {
		t.Fatal("expected deterministic key id")
	}
	if DefaultRegionFor("validator-99") != "unknown" {
		t.Fatal("unknown validator should use unknown region")
	}
	if _, err := RequireIdentity(identities, "validator-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireIdentity(identities, "validator-99"); err == nil {
		t.Fatal("expected missing identity error")
	}
}

func TestValidatorIdentitiesForPQSuite(t *testing.T) {
	selected, err := cryptosuite.Lookup("pq")
	if err != nil {
		t.Fatal(err)
	}
	identities, err := ValidatorIdentitiesForSuite(map[string]string{"validator-1": "http://validator-1:8080"}, selected)
	if err != nil {
		t.Fatal(err)
	}
	validator := identities["validator-1"]
	if validator.SignatureAlgorithm != selected.SignatureAlgorithm || validator.KEMAlgorithm != selected.KEMAlgorithm {
		t.Fatalf("unexpected algorithms: %+v", validator)
	}
	if len(validator.SignaturePublicKeyBytes()) == 0 || len(validator.KEMPublicKey) == 0 {
		t.Fatal("expected PQ public keys")
	}
	if validator.KeyID != ValidatorKeyID(validator.ID, validator.SignatureAlgorithm, validator.SignaturePublicKey, validator.KEMAlgorithm, validator.KEMPublicKey) {
		t.Fatal("key id should be deterministic")
	}
	if PublicKeyFingerprint(validator.SignatureAlgorithm, validator.SignaturePublicKey) == PublicKeyFingerprint(validator.KEMAlgorithm, validator.KEMPublicKey) {
		t.Fatal("different keys should have different fingerprints")
	}
}
