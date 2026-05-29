package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashTokenIsStableAndDomainSeparated(t *testing.T) {
	first := HashToken("secret")
	second := HashToken(" secret ")
	if first != second {
		t.Fatalf("expected trimmed token hash to be stable: %s != %s", first, second)
	}
	if first == "sha256:2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b" {
		t.Fatal("expected domain-separated hash, got raw sha256(secret)")
	}
}

func TestAuthenticatorAcceptsValidKeyAndRejectsWrongRoleDisabledAndExpired(t *testing.T) {
	now := time.UnixMilli(2_000)
	authn, err := NewAuthenticator([]APIKeyRecord{
		{
			ID:                 "submitter",
			Organization:       "bestgate",
			TokenHash:          HashToken("submit-token"),
			Roles:              []string{RoleEvidenceSubmit},
			CreatedAtUnixMilli: 1_000,
		},
		{
			ID:           "disabled",
			Organization: "bestgate",
			TokenHash:    HashToken("disabled-token"),
			Roles:        []string{RoleEvidenceRead},
			Disabled:     true,
		},
		{
			ID:                 "expired",
			Organization:       "bestgate",
			TokenHash:          HashToken("expired-token"),
			Roles:              []string{RoleEvidenceRead},
			ExpiresAtUnixMilli: now.UnixMilli(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authn.Authenticate("submit-token", now)
	if err != nil {
		t.Fatal(err)
	}
	if principal.ID != "submitter" || !HasRole(principal, RoleEvidenceSubmit) || HasRole(principal, RoleEvidenceRead) {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if _, err := authn.Authenticate("disabled-token", now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected disabled token to be unauthorized, got %v", err)
	}
	if _, err := authn.Authenticate("expired-token", now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected expired token to be unauthorized, got %v", err)
	}
	if _, err := authn.Authenticate("wrong", now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected wrong token to be unauthorized, got %v", err)
	}
}

func TestLoadAPIKeyFileValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api-keys.json")
	data := `{"keys":[{"id":"admin","organization":"bestgate","token_hash":"` + HashToken("admin-token") + `","roles":["admin:read","evidence:read"]}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	authn, err := LoadAPIKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authn.Authenticate("admin-token", time.Now()); err != nil {
		t.Fatal(err)
	}

	badPath := filepath.Join(t.TempDir(), "bad-api-keys.json")
	if err := os.WriteFile(badPath, []byte(`{"keys":[{"id":"bad","organization":"bestgate","token_hash":"sha256:nothex","roles":["evidence:read"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAPIKeyFile(badPath); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestAuthenticatorRejectsAllDisabledConfig(t *testing.T) {
	_, err := NewAuthenticator([]APIKeyRecord{
		{ID: "disabled", Organization: "bestgate", TokenHash: HashToken("disabled-token"), Roles: []string{RoleEvidenceRead}, Disabled: true},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected all-disabled config to be invalid, got %v", err)
	}
}
