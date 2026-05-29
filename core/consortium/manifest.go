package consortium

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
)

const SchemaVersion = "pq-fabric.consortium.v1"

var ErrInvalidManifest = errors.New("invalid consortium manifest")

type Manifest struct {
	SchemaVersion     string            `json:"schema_version"`
	ConsortiumID      string            `json:"consortium_id"`
	MembershipVersion uint64            `json:"membership_version"`
	QuorumThreshold   int               `json:"quorum_threshold"`
	Validators        []ValidatorRecord `json:"validators"`
}

type ValidatorRecord struct {
	ID                      string `json:"id"`
	Operator                string `json:"operator"`
	Region                  string `json:"region"`
	PublicURL               string `json:"public_url"`
	SignatureAlgorithm      string `json:"signature_algorithm"`
	SignaturePublicKey      string `json:"signature_public_key"`
	SignatureKeyFingerprint string `json:"signature_key_fingerprint"`
	KEMAlgorithm            string `json:"kem_algorithm"`
	KEMPublicKey            string `json:"kem_public_key"`
	KEMKeyFingerprint       string `json:"kem_key_fingerprint"`
	KeyID                   string `json:"key_id"`
	SigningKeyRef           string `json:"signing_key_ref,omitempty"`
	TLSURISAN               string `json:"tls_uri_san"`
	Active                  bool   `json:"active"`
}

type History struct {
	Manifests []Manifest
	byHash    map[string]Manifest
	byVersion map[uint64]Manifest
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: parse %s: %v", ErrInvalidManifest, path, err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func LoadManifestHistory(raw string) (History, error) {
	paths := splitPathList(raw)
	manifests := make([]Manifest, 0, len(paths))
	for _, path := range paths {
		manifest, err := LoadManifest(path)
		if err != nil {
			return History{}, fmt.Errorf("load manifest history %s: %w", path, err)
		}
		manifests = append(manifests, manifest)
	}
	return NewHistory(manifests)
}

func NewHistory(manifests []Manifest) (History, error) {
	history := History{
		Manifests: append([]Manifest(nil), manifests...),
		byHash:    map[string]Manifest{},
		byVersion: map[uint64]Manifest{},
	}
	if len(history.Manifests) == 0 {
		return history, nil
	}
	consortiumID := strings.TrimSpace(history.Manifests[0].ConsortiumID)
	for _, manifest := range history.Manifests {
		if err := manifest.Validate(); err != nil {
			return History{}, err
		}
		if manifest.ConsortiumID != consortiumID {
			return History{}, fmt.Errorf("%w: manifest history mixes consortium ids %q and %q", ErrInvalidManifest, consortiumID, manifest.ConsortiumID)
		}
		hash, err := manifest.Hash()
		if err != nil {
			return History{}, err
		}
		if _, exists := history.byHash[hash]; exists {
			return History{}, fmt.Errorf("%w: duplicate manifest hash %s", ErrInvalidManifest, hash)
		}
		if _, exists := history.byVersion[manifest.MembershipVersion]; exists {
			return History{}, fmt.Errorf("%w: duplicate membership version %d", ErrInvalidManifest, manifest.MembershipVersion)
		}
		history.byHash[hash] = manifest
		history.byVersion[manifest.MembershipVersion] = manifest
	}
	sort.Slice(history.Manifests, func(i, j int) bool {
		return history.Manifests[i].MembershipVersion < history.Manifests[j].MembershipVersion
	})
	return history, nil
}

func (h History) WithManifest(manifest Manifest) (History, error) {
	manifests := append([]Manifest(nil), h.Manifests...)
	hash, err := manifest.Hash()
	if err != nil {
		return History{}, err
	}
	for _, existing := range manifests {
		existingHash, err := existing.Hash()
		if err != nil {
			return History{}, err
		}
		if existingHash == hash {
			return NewHistory(manifests)
		}
		if existing.MembershipVersion == manifest.MembershipVersion {
			return History{}, fmt.Errorf("%w: history has version %d with a different hash", ErrInvalidManifest, manifest.MembershipVersion)
		}
	}
	manifests = append(manifests, manifest)
	return NewHistory(manifests)
}

func (h History) ManifestByHash(hash string) (Manifest, bool) {
	manifest, ok := h.byHash[strings.TrimSpace(hash)]
	return manifest, ok
}

func (h History) ManifestByVersion(version uint64) (Manifest, bool) {
	manifest, ok := h.byVersion[version]
	return manifest, ok
}

func (h History) Contains(manifest Manifest) (bool, error) {
	hash, err := manifest.Hash()
	if err != nil {
		return false, err
	}
	_, ok := h.ManifestByHash(hash)
	return ok, nil
}

func ManifestFromIdentities(consortiumID string, membershipVersion uint64, threshold int, identities map[string]identity.ValidatorIdentity, order []string) Manifest {
	if len(order) == 0 {
		order = identity.DefaultValidatorIDs()
	}
	records := make([]ValidatorRecord, 0, len(order))
	for _, id := range order {
		v, ok := identities[id]
		if !ok {
			continue
		}
		record := ValidatorRecordFromIdentity(v, "operator-"+id, true)
		record.TLSURISAN = ExpectedTLSURISAN(consortiumID, id)
		records = append(records, record)
	}
	return Manifest{
		SchemaVersion:     SchemaVersion,
		ConsortiumID:      consortiumID,
		MembershipVersion: membershipVersion,
		QuorumThreshold:   threshold,
		Validators:        records,
	}
}

func ValidatorRecordFromIdentity(v identity.ValidatorIdentity, operator string, active bool) ValidatorRecord {
	signatureAlgorithm := v.SignatureAlgorithmName()
	return ValidatorRecord{
		ID:                      v.ID,
		Operator:                strings.TrimSpace(operator),
		Region:                  v.Region,
		PublicURL:               strings.TrimRight(strings.TrimSpace(v.PublicURL), "/"),
		SignatureAlgorithm:      signatureAlgorithm,
		SignaturePublicKey:      base64.StdEncoding.EncodeToString(v.SignaturePublicKeyBytes()),
		SignatureKeyFingerprint: identity.PublicKeyFingerprint(signatureAlgorithm, v.SignaturePublicKeyBytes()),
		KEMAlgorithm:            v.KEMAlgorithm,
		KEMPublicKey:            base64.StdEncoding.EncodeToString(v.KEMPublicKey),
		KEMKeyFingerprint:       identity.PublicKeyFingerprint(v.KEMAlgorithm, v.KEMPublicKey),
		KeyID:                   v.KeyID,
		SigningKeyRef:           v.KeyID,
		Active:                  active,
	}
}

func (m Manifest) Validate() error {
	if strings.TrimSpace(m.SchemaVersion) != SchemaVersion {
		return fmt.Errorf("%w: unsupported schema version %q", ErrInvalidManifest, m.SchemaVersion)
	}
	if strings.TrimSpace(m.ConsortiumID) == "" {
		return fmt.Errorf("%w: consortium id required", ErrInvalidManifest)
	}
	if m.MembershipVersion == 0 {
		return fmt.Errorf("%w: membership version must be positive", ErrInvalidManifest)
	}
	if m.QuorumThreshold <= 0 {
		return fmt.Errorf("%w: quorum threshold must be positive", ErrInvalidManifest)
	}
	if len(m.Validators) == 0 {
		return fmt.Errorf("%w: validators required", ErrInvalidManifest)
	}
	seen := map[string]struct{}{}
	activeCount := 0
	for _, record := range m.Validators {
		id := strings.TrimSpace(record.ID)
		if id == "" {
			return fmt.Errorf("%w: validator id required", ErrInvalidManifest)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate validator id %s", ErrInvalidManifest, id)
		}
		seen[id] = struct{}{}
		if !record.Active {
			continue
		}
		activeCount++
		if strings.TrimSpace(record.Operator) == "" {
			return fmt.Errorf("%w: active validator %s operator required", ErrInvalidManifest, id)
		}
		if strings.TrimSpace(record.Region) == "" {
			return fmt.Errorf("%w: active validator %s region required", ErrInvalidManifest, id)
		}
		if strings.TrimSpace(record.SignatureAlgorithm) == "" || strings.TrimSpace(record.KEMAlgorithm) == "" {
			return fmt.Errorf("%w: validator %s algorithms required", ErrInvalidManifest, id)
		}
		if strings.TrimSpace(record.SignaturePublicKey) == "" || strings.TrimSpace(record.KEMPublicKey) == "" {
			return fmt.Errorf("%w: validator %s public keys required", ErrInvalidManifest, id)
		}
		signaturePublic, err := base64.StdEncoding.DecodeString(record.SignaturePublicKey)
		if err != nil {
			return fmt.Errorf("%w: validator %s signature public key must be base64", ErrInvalidManifest, id)
		}
		kemPublic, err := base64.StdEncoding.DecodeString(record.KEMPublicKey)
		if err != nil {
			return fmt.Errorf("%w: validator %s KEM public key must be base64", ErrInvalidManifest, id)
		}
		if strings.TrimSpace(record.SignatureKeyFingerprint) == "" || strings.TrimSpace(record.KEMKeyFingerprint) == "" || strings.TrimSpace(record.KeyID) == "" {
			return fmt.Errorf("%w: validator %s key fingerprints required", ErrInvalidManifest, id)
		}
		if record.SignatureKeyFingerprint != identity.PublicKeyFingerprint(record.SignatureAlgorithm, signaturePublic) {
			return fmt.Errorf("%w: validator %s signature fingerprint mismatch", ErrInvalidManifest, id)
		}
		if record.KEMKeyFingerprint != identity.PublicKeyFingerprint(record.KEMAlgorithm, kemPublic) {
			return fmt.Errorf("%w: validator %s KEM fingerprint mismatch", ErrInvalidManifest, id)
		}
		expectedKeyID := identity.ValidatorKeyID(id, record.SignatureAlgorithm, signaturePublic, record.KEMAlgorithm, kemPublic)
		if record.KeyID != expectedKeyID {
			return fmt.Errorf("%w: validator %s key id mismatch", ErrInvalidManifest, id)
		}
		if record.Active && strings.TrimSpace(record.TLSURISAN) != ExpectedTLSURISAN(m.ConsortiumID, id) {
			return fmt.Errorf("%w: active validator %s TLS URI SAN must be %s", ErrInvalidManifest, id, ExpectedTLSURISAN(m.ConsortiumID, id))
		}
	}
	if activeCount < m.QuorumThreshold {
		return fmt.Errorf("%w: active validator count %d below threshold %d", ErrInvalidManifest, activeCount, m.QuorumThreshold)
	}
	return nil
}

func (m Manifest) ValidateAgainstIdentities(identities map[string]identity.ValidatorIdentity, threshold int) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if threshold > 0 && m.QuorumThreshold != threshold {
		return fmt.Errorf("%w: manifest threshold %d does not match configured threshold %d", ErrInvalidManifest, m.QuorumThreshold, threshold)
	}
	for _, record := range m.ActiveValidators() {
		v, ok := identities[record.ID]
		if !ok {
			return fmt.Errorf("%w: active validator %s missing local identity", ErrInvalidManifest, record.ID)
		}
		expected := ValidatorRecordFromIdentity(v, record.Operator, true)
		if record.Region != expected.Region {
			return fmt.Errorf("%w: validator %s region mismatch manifest=%s local=%s", ErrInvalidManifest, record.ID, record.Region, expected.Region)
		}
		if record.SignatureAlgorithm != expected.SignatureAlgorithm || record.SignatureKeyFingerprint != expected.SignatureKeyFingerprint {
			return fmt.Errorf("%w: validator %s signature identity mismatch", ErrInvalidManifest, record.ID)
		}
		if record.KEMAlgorithm != expected.KEMAlgorithm || record.KEMKeyFingerprint != expected.KEMKeyFingerprint {
			return fmt.Errorf("%w: validator %s KEM identity mismatch", ErrInvalidManifest, record.ID)
		}
		if record.KeyID != expected.KeyID {
			return fmt.Errorf("%w: validator %s key id mismatch", ErrInvalidManifest, record.ID)
		}
	}
	return nil
}

func (m Manifest) ActiveIdentities() (map[string]identity.ValidatorIdentity, error) {
	return m.identities(true)
}

func (m Manifest) AllIdentities() (map[string]identity.ValidatorIdentity, error) {
	return m.identities(false)
}

func (m Manifest) Hash() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	return messages.HashCanonical(m)
}

func (m Manifest) ActiveValidators() []ValidatorRecord {
	out := make([]ValidatorRecord, 0, len(m.Validators))
	for _, record := range m.Validators {
		if record.Active {
			record.ID = strings.TrimSpace(record.ID)
			record.PublicURL = strings.TrimRight(strings.TrimSpace(record.PublicURL), "/")
			out = append(out, record)
		}
	}
	return out
}

func ExpectedTLSURISAN(consortiumID, validatorID string) string {
	return "spiffe://" + strings.TrimSpace(consortiumID) + "/validator/" + strings.TrimSpace(validatorID)
}

func ValidatorIDFromTLSURIs(uris []*url.URL, consortiumID string) (string, error) {
	prefix := "spiffe://" + strings.TrimSpace(consortiumID) + "/validator/"
	for _, uri := range uris {
		if uri == nil {
			continue
		}
		value := uri.String()
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if id == "" || strings.Contains(id, "/") {
			return "", fmt.Errorf("%w: malformed validator TLS URI SAN %q", ErrInvalidManifest, value)
		}
		return id, nil
	}
	return "", fmt.Errorf("%w: validator TLS URI SAN with prefix %s not found", ErrInvalidManifest, prefix)
}

func (m Manifest) identities(activeOnly bool) (map[string]identity.ValidatorIdentity, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	out := make(map[string]identity.ValidatorIdentity)
	for _, record := range m.Validators {
		if activeOnly && !record.Active {
			continue
		}
		signaturePublic, err := base64.StdEncoding.DecodeString(record.SignaturePublicKey)
		if err != nil {
			return nil, err
		}
		kemPublic, err := base64.StdEncoding.DecodeString(record.KEMPublicKey)
		if err != nil {
			return nil, err
		}
		out[record.ID] = identity.ValidatorIdentity{
			ID:                 record.ID,
			Region:             record.Region,
			PublicURL:          strings.TrimRight(strings.TrimSpace(record.PublicURL), "/"),
			Algorithm:          record.SignatureAlgorithm,
			PublicKey:          signaturePublic,
			SignatureAlgorithm: record.SignatureAlgorithm,
			SignaturePublicKey: signaturePublic,
			KEMAlgorithm:       record.KEMAlgorithm,
			KEMPublicKey:       kemPublic,
			KeyID:              record.KeyID,
		}
	}
	return out, nil
}

func splitPathList(raw string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, err := strconv.Unquote(item); err == nil {
			item, _ = strconv.Unquote(item)
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (m Manifest) ActiveValidatorIDs() []string {
	records := m.ActiveValidators()
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.ID)
	}
	return out
}

func (m Manifest) ValidatorByID(id string) (ValidatorRecord, bool) {
	id = strings.TrimSpace(id)
	for _, record := range m.Validators {
		if strings.TrimSpace(record.ID) == id {
			record.ID = strings.TrimSpace(record.ID)
			record.PublicURL = strings.TrimRight(strings.TrimSpace(record.PublicURL), "/")
			return record, true
		}
	}
	return ValidatorRecord{}, false
}

func (m Manifest) PublicURLs() map[string]string {
	out := map[string]string{}
	for _, record := range m.ActiveValidators() {
		if record.PublicURL != "" {
			out[record.ID] = record.PublicURL
		}
	}
	return out
}

func (m Manifest) SortedOperators() []string {
	seen := map[string]struct{}{}
	for _, record := range m.ActiveValidators() {
		if record.Operator != "" {
			seen[record.Operator] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for operator := range seen {
		out = append(out, operator)
	}
	sort.Strings(out)
	return out
}
