package consortium

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
)

func TestManifestValidatesAgainstLocalIdentities(t *testing.T) {
	manifest := testManifest(t, cryptosuite.Dev)
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	identities, err := identity.ValidatorIdentitiesForSuite(manifest.PublicURLs(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidateAgainstIdentities(identities, 5); err != nil {
		t.Fatal(err)
	}
	hash1, err := manifest.Hash()
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := manifest.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hash1 == "" || hash1 != hash2 {
		t.Fatalf("expected stable manifest hash, got %q and %q", hash1, hash2)
	}
}

func TestManifestRejectsIdentityMismatch(t *testing.T) {
	manifest := testManifest(t, cryptosuite.Dev)
	manifest.Validators[0].SignatureKeyFingerprint = "wrong"
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	identities, err := identity.ValidatorIdentitiesForSuite(manifest.PublicURLs(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidateAgainstIdentities(identities, 5); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected invalid manifest error, got %v", err)
	}
}

func TestLoadManifestRejectsMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"wrong"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected invalid manifest error, got %v", err)
	}
}

func testManifest(t *testing.T, suite cryptosuite.Name) Manifest {
	t.Helper()
	selected := cryptosuite.MustLookup(string(suite))
	urls := map[string]string{}
	for _, id := range identity.DefaultValidatorIDs() {
		urls[id] = "http://" + id + ":8080"
	}
	identities, err := identity.ValidatorIdentitiesForSuite(urls, selected)
	if err != nil {
		t.Fatal(err)
	}
	manifest := ManifestFromIdentities("test-consortium", 1, 5, identities, identity.DefaultValidatorIDs())
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Manifest
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	return roundTrip
}
