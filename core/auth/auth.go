package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	RoleEvidenceSubmit = "evidence:submit"
	RoleEvidenceRead   = "evidence:read"
	RoleEvidenceVerify = "evidence:verify"
	RoleAnchorRead     = "anchor:read"
	RoleAdminRead      = "admin:read"

	hashPrefix = "sha256:"
	hashDomain = "pq-fabric/api-key/v1"
)

var (
	ErrInvalidConfig = errors.New("invalid API key config")
	ErrUnauthorized  = errors.New("API key unauthorized")
	ErrForbidden     = errors.New("API key lacks required role")
)

type APIKeyFile struct {
	Keys []APIKeyRecord `json:"keys"`
}

type APIKeyRecord struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Organization       string   `json:"organization"`
	TokenHash          string   `json:"token_hash"`
	Roles              []string `json:"roles"`
	Disabled           bool     `json:"disabled,omitempty"`
	CreatedAtUnixMilli int64    `json:"created_at_unix_milli,omitempty"`
	ExpiresAtUnixMilli int64    `json:"expires_at_unix_milli,omitempty"`
}

type Principal struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	Legacy       bool     `json:"legacy,omitempty"`
}

type Authenticator struct {
	byHash map[string]APIKeyRecord
	byID   map[string]APIKeyRecord
}

func LoadAPIKeyFile(path string) (*Authenticator, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file APIKeyFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrInvalidConfig, path, err)
	}
	return NewAuthenticator(file.Keys)
}

func NewAuthenticator(records []APIKeyRecord) (*Authenticator, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("%w: at least one API key is required", ErrInvalidConfig)
	}
	authn := &Authenticator{
		byHash: make(map[string]APIKeyRecord, len(records)),
		byID:   make(map[string]APIKeyRecord, len(records)),
	}
	enabled := 0
	for _, record := range records {
		normalized, err := normalizeRecord(record)
		if err != nil {
			return nil, err
		}
		if _, exists := authn.byID[normalized.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate API key id %q", ErrInvalidConfig, normalized.ID)
		}
		if _, exists := authn.byHash[normalized.TokenHash]; exists {
			return nil, fmt.Errorf("%w: duplicate API key hash for id %q", ErrInvalidConfig, normalized.ID)
		}
		authn.byID[normalized.ID] = normalized
		authn.byHash[normalized.TokenHash] = normalized
		if !normalized.Disabled {
			enabled++
		}
	}
	if enabled == 0 {
		return nil, fmt.Errorf("%w: at least one enabled API key is required", ErrInvalidConfig)
	}
	return authn, nil
}

func (a *Authenticator) Authenticate(rawToken string, now time.Time) (Principal, error) {
	if a == nil {
		return Principal{}, ErrUnauthorized
	}
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return Principal{}, ErrUnauthorized
	}
	tokenHash := HashToken(token)
	for configuredHash, record := range a.byHash {
		if subtle.ConstantTimeCompare([]byte(configuredHash), []byte(tokenHash)) != 1 {
			continue
		}
		if record.Disabled {
			return Principal{}, ErrUnauthorized
		}
		if record.ExpiresAtUnixMilli > 0 && now.UnixMilli() >= record.ExpiresAtUnixMilli {
			return Principal{}, ErrUnauthorized
		}
		return Principal{
			ID:           record.ID,
			Name:         record.Name,
			Organization: record.Organization,
			Roles:        append([]string(nil), record.Roles...),
		}, nil
	}
	return Principal{}, ErrUnauthorized
}

func HasRole(principal Principal, role string) bool {
	role = strings.TrimSpace(role)
	if role == "" {
		return true
	}
	for _, candidate := range principal.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func HashToken(token string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(hashDomain))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.TrimSpace(token)))
	return hashPrefix + hex.EncodeToString(h.Sum(nil))
}

func normalizeRecord(record APIKeyRecord) (APIKeyRecord, error) {
	record.ID = strings.TrimSpace(record.ID)
	record.Name = strings.TrimSpace(record.Name)
	record.Organization = strings.TrimSpace(record.Organization)
	record.TokenHash = strings.ToLower(strings.TrimSpace(record.TokenHash))
	if record.ID == "" {
		return APIKeyRecord{}, fmt.Errorf("%w: API key id is required", ErrInvalidConfig)
	}
	if record.Organization == "" {
		return APIKeyRecord{}, fmt.Errorf("%w: API key %q organization is required", ErrInvalidConfig, record.ID)
	}
	if !validHash(record.TokenHash) {
		return APIKeyRecord{}, fmt.Errorf("%w: API key %q token_hash must be sha256:<64 hex chars>", ErrInvalidConfig, record.ID)
	}
	roles := make([]string, 0, len(record.Roles))
	seen := map[string]struct{}{}
	for _, role := range record.Roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if !knownRole(role) {
			return APIKeyRecord{}, fmt.Errorf("%w: API key %q has unknown role %q", ErrInvalidConfig, record.ID, role)
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		return APIKeyRecord{}, fmt.Errorf("%w: API key %q requires at least one role", ErrInvalidConfig, record.ID)
	}
	sort.Strings(roles)
	record.Roles = roles
	return record, nil
}

func validHash(value string) bool {
	if !strings.HasPrefix(value, hashPrefix) {
		return false
	}
	raw := strings.TrimPrefix(value, hashPrefix)
	if len(raw) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(raw)
	return err == nil
}

func knownRole(role string) bool {
	switch role {
	case RoleEvidenceSubmit, RoleEvidenceRead, RoleEvidenceVerify, RoleAnchorRead, RoleAdminRead:
		return true
	default:
		return false
	}
}
