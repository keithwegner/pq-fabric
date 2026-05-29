package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	apiauth "github.com/keithwegner/pq-fabric/core/auth"
	"github.com/keithwegner/pq-fabric/core/consortium"
	"github.com/keithwegner/pq-fabric/core/deployment"
	"github.com/keithwegner/pq-fabric/core/identity"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "pq-fabric.bootstrap.v1"

	SecretModeFileTree               = "file-tree"
	SecretModeKubernetesSecret       = "kubernetes-secret-manifest"
	SecretModeExternalSecretContract = "external-secret-contract"

	StatusPass       = "pass"
	StatusFail       = "fail"
	StatusUnresolved = "unresolved"

	SensitivityAPIKeys         = "api-key-config"
	SensitivityManifestCurrent = "manifest-current"
	SensitivityManifestHistory = "manifest-history"
	SensitivityPeerTLSCert     = "peer-tls-cert"
	SensitivityPeerTLSKey      = "peer-tls-key"
	SensitivityPeerTLSCA       = "peer-tls-ca"
	SensitivityKMSToken        = "kms-token"
	SensitivityKMSKeyID        = "kms-key-id"
	SensitivityKMSCA           = "kms-ca"
	SensitivityOTELHeaders     = "otel-headers"
)

type BootstrapSpec struct {
	SchemaVersion       string            `json:"schema_version" yaml:"schema_version"`
	Profile             string            `json:"profile" yaml:"profile"`
	ConsortiumID        string            `json:"consortium_id" yaml:"consortium_id"`
	ValidatorCount      int               `json:"validator_count" yaml:"validator_count"`
	QuorumThreshold     int               `json:"quorum_threshold" yaml:"quorum_threshold"`
	SecretSource        SecretSourceSpec  `json:"secret_source" yaml:"secret_source"`
	SecretReferences    []SecretReference `json:"secret_references" yaml:"secret_references"`
	DatabaseURLTemplate string            `json:"database_url_template" yaml:"database_url_template"`
	KMS                 KMSBootstrapSpec  `json:"kms" yaml:"kms"`
	OTEL                OTELBootstrapSpec `json:"otel" yaml:"otel"`
	PublicExitRouting   bool              `json:"public_exit_routing" yaml:"public_exit_routing"`
	MainnetAnchoring    bool              `json:"mainnet_anchoring" yaml:"mainnet_anchoring"`
}

type SecretSourceSpec struct {
	Mode         string `json:"mode" yaml:"mode"`
	Provider     string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Path         string `json:"path,omitempty" yaml:"path,omitempty"`
	FileTreeRoot string `json:"file_tree_root,omitempty" yaml:"file_tree_root,omitempty"`
}

type SecretReference struct {
	Name        string `json:"name" yaml:"name"`
	Key         string `json:"key" yaml:"key"`
	MountPath   string `json:"mount_path" yaml:"mount_path"`
	Sensitivity string `json:"sensitivity" yaml:"sensitivity"`
	Required    bool   `json:"required" yaml:"required"`
	ValidatorID string `json:"validator_id,omitempty" yaml:"validator_id,omitempty"`
	RemoteRef   string `json:"remote_ref,omitempty" yaml:"remote_ref,omitempty"`
}

type KMSBootstrapSpec struct {
	Provider      string `json:"provider" yaml:"provider"`
	Endpoint      string `json:"endpoint" yaml:"endpoint"`
	KeyIDTemplate string `json:"key_id_template" yaml:"key_id_template"`
	TokenRef      string `json:"token_ref,omitempty" yaml:"token_ref,omitempty"`
	CAFile        string `json:"ca_file,omitempty" yaml:"ca_file,omitempty"`
	AllowInsecure bool   `json:"allow_insecure" yaml:"allow_insecure"`
}

type OTELBootstrapSpec struct {
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	Endpoint   string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	HeadersRef string `json:"headers_ref,omitempty" yaml:"headers_ref,omitempty"`
	Insecure   bool   `json:"insecure,omitempty" yaml:"insecure,omitempty"`
}

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type ManifestSummary struct {
	ConsortiumID      string   `json:"consortium_id,omitempty"`
	MembershipVersion uint64   `json:"membership_version,omitempty"`
	ValidatorSetHash  string   `json:"validator_set_hash,omitempty"`
	QuorumThreshold   int      `json:"quorum_threshold,omitempty"`
	ActiveValidators  []string `json:"active_validators,omitempty"`
	HistoryVersions   []uint64 `json:"history_versions,omitempty"`
}

type SecretSourceSummary struct {
	Mode              string   `json:"mode"`
	Provider          string   `json:"provider,omitempty"`
	Path              string   `json:"path,omitempty"`
	FileTreeRoot      string   `json:"file_tree_root,omitempty"`
	ResolvedSecrets   int      `json:"resolved_secrets"`
	ResolvedKeys      int      `json:"resolved_keys"`
	UnresolvedKeys    int      `json:"unresolved_keys"`
	SecretNames       []string `json:"secret_names,omitempty"`
	SecretStoreRefs   []string `json:"secret_store_refs,omitempty"`
	RawValuesRedacted bool     `json:"raw_values_redacted"`
}

type SecretMaterialEvidence struct {
	Name            string `json:"name"`
	Key             string `json:"key"`
	MountPath       string `json:"mount_path,omitempty"`
	Sensitivity     string `json:"sensitivity"`
	ValidatorID     string `json:"validator_id,omitempty"`
	Required        bool   `json:"required"`
	Source          string `json:"source"`
	StoreRef        string `json:"store_ref,omitempty"`
	RemoteRef       string `json:"remote_ref,omitempty"`
	Resolved        bool   `json:"resolved"`
	Status          string `json:"status"`
	SizeBytes       int    `json:"size_bytes,omitempty"`
	SafeFingerprint string `json:"safe_fingerprint,omitempty"`
	ContentStatus   string `json:"content_status,omitempty"`
	Redaction       string `json:"redaction"`
}

type SmokeEvidence struct {
	ReceiptID          string `json:"receipt_id,omitempty"`
	EvidenceID         string `json:"evidence_id,omitempty"`
	VerificationStatus string `json:"verification_status,omitempty"`
	SignerCount        int    `json:"signer_count,omitempty"`
	CommitHeight       uint64 `json:"commit_height,omitempty"`
	RestoredDatabase   string `json:"restored_database,omitempty"`
	Message            string `json:"message,omitempty"`
}

type BootstrapReport struct {
	GeneratedAtUnixMilli int64                    `json:"generated_at_unix_milli"`
	Profile              string                   `json:"profile"`
	Status               string                   `json:"status"`
	Checks               []Check                  `json:"checks"`
	Manifest             ManifestSummary          `json:"manifest,omitempty"`
	SecretSource         SecretSourceSummary      `json:"secret_source"`
	SecretEvidence       []SecretMaterialEvidence `json:"secret_evidence,omitempty"`
	Smoke                SmokeEvidence            `json:"smoke,omitempty"`
	Limitations          string                   `json:"limitations"`
}

func LoadSpec(path string) (BootstrapSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BootstrapSpec{}, err
	}
	var spec BootstrapSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return BootstrapSpec{}, fmt.Errorf("parse bootstrap spec %s: %w", path, err)
	}
	if spec.SecretSource.Path != "" && !filepath.IsAbs(spec.SecretSource.Path) {
		spec.SecretSource.Path = filepath.Clean(filepath.Join(filepath.Dir(path), spec.SecretSource.Path))
	}
	if spec.SecretSource.FileTreeRoot != "" && !filepath.IsAbs(spec.SecretSource.FileTreeRoot) {
		spec.SecretSource.FileTreeRoot = filepath.Clean(filepath.Join(filepath.Dir(path), spec.SecretSource.FileTreeRoot))
	}
	return spec, nil
}

func Validate(ctx context.Context, spec BootstrapSpec) BootstrapReport {
	_ = ctx
	checks := []Check{}
	add := func(name string, ok bool, msg string) {
		status := StatusPass
		if !ok {
			status = StatusFail
		}
		checks = append(checks, Check{Name: name, Status: status, OK: ok, Message: msg})
	}
	unresolved := func(name, msg string) {
		checks = append(checks, Check{Name: name, Status: StatusUnresolved, OK: true, Message: msg})
	}

	if strings.TrimSpace(spec.SchemaVersion) == "" {
		spec.SchemaVersion = SchemaVersion
	}
	if spec.Profile == "" {
		spec.Profile = deployment.ProfileProductionPilot
	}
	if spec.ValidatorCount == 0 {
		spec.ValidatorCount = len(identity.DefaultValidatorIDs())
	}
	if spec.QuorumThreshold == 0 {
		spec.QuorumThreshold = 5
	}
	add("schema_version", spec.SchemaVersion == SchemaVersion, fmt.Sprintf("got=%q want=%q", spec.SchemaVersion, SchemaVersion))
	add("profile", spec.Profile == deployment.ProfileProductionPilot || spec.Profile == deployment.ProfileStaging, "bootstrap is intended for staging or production-pilot")
	add("validator_count", spec.ValidatorCount == len(identity.DefaultValidatorIDs()), fmt.Sprintf("got=%d want=%d", spec.ValidatorCount, len(identity.DefaultValidatorIDs())))
	add("quorum_threshold", spec.QuorumThreshold == 5, fmt.Sprintf("got=%d want=5", spec.QuorumThreshold))
	add("consortium_id", strings.TrimSpace(spec.ConsortiumID) != "", "consortium id configured")
	add("public_exits_disabled", !spec.PublicExitRouting, "public exits must remain disabled")
	add("mainnet_anchoring_disabled", !spec.MainnetAnchoring, "mainnet anchoring must remain disabled")
	add("database_url_template", strings.TrimSpace(spec.DatabaseURLTemplate) != "", "SQLite database URL template configured")
	add("kms_provider", strings.TrimSpace(spec.KMS.Provider) == "cloud-kms", "cloud-kms provider required for production-pilot bootstrap")
	add("kms_endpoint", strings.TrimSpace(spec.KMS.Endpoint) != "", "KMS endpoint configured")
	add("kms_key_template", strings.TrimSpace(spec.KMS.KeyIDTemplate) != "", "KMS key id template configured")
	add("kms_insecure_disabled", !spec.KMS.AllowInsecure, "KMS insecure mode must be false in bootstrap contracts")
	if spec.OTEL.Enabled {
		add("otel_endpoint", strings.TrimSpace(spec.OTEL.Endpoint) != "", "OTEL endpoint required when enabled")
	}

	source, sourceChecks := resolveSecretSource(spec)
	checks = append(checks, sourceChecks...)
	bySensitivity := map[string][]SecretReference{}
	refKeys := map[string]struct{}{}
	for _, ref := range spec.SecretReferences {
		ref = normalizeSecretReference(ref)
		key := ref.Name + "/" + ref.Key
		refKeys[key] = struct{}{}
		bySensitivity[ref.Sensitivity] = append(bySensitivity[ref.Sensitivity], ref)
		if ref.Required {
			material, ok := source.Material(ref.Name, ref.Key)
			if !ok {
				add("secret_ref_"+safeName(ref.Name+"_"+ref.Key), false, "required secret reference missing")
				continue
			}
			if !material.Resolved {
				unresolved("secret_ref_"+safeName(ref.Name+"_"+ref.Key), "external secret reference present; content validation deferred")
			} else if len(material.Value) == 0 {
				add("secret_ref_"+safeName(ref.Name+"_"+ref.Key), false, "resolved secret value is empty")
			} else {
				add("secret_ref_"+safeName(ref.Name+"_"+ref.Key), true, "resolved")
			}
		}
	}
	if len(refKeys) == 0 {
		add("secret_references", false, "at least one secret reference is required")
	}
	manifestSummary := validateManifestReferences(source, bySensitivity, spec, add, unresolved)
	validateAPIKeyReferences(source, bySensitivity, add, unresolved)
	validateTLSReferences(source, bySensitivity, spec, add, unresolved)
	validateKMSReferences(source, bySensitivity, add, unresolved)
	validateOverlayCompatibleSecretNames(spec, add)

	report := BootstrapReport{
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		Profile:              spec.Profile,
		Checks:               checks,
		Manifest:             manifestSummary,
		SecretSource:         source.Summary(spec.SecretSource),
		SecretEvidence:       buildSecretEvidence(source, spec.SecretReferences),
		Limitations:          "Provider-neutral bootstrap validation only; no cloud resources, Kubernetes apply, Terraform apply, remote secret fetch, image signing, Polygon mainnet, public exits, payload custody, or certification claims.",
	}
	report.Status = reportStatus(checks)
	return report
}

func (r BootstrapReport) OK() bool {
	for _, check := range r.Checks {
		if check.Status == StatusFail || !check.OK {
			return false
		}
	}
	return true
}

func (r BootstrapReport) MarshalJSONIndented(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r)
}

func (r BootstrapReport) MarshalText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric pilot bootstrap report\n")
	fmt.Fprintf(&b, "profile=%s status=%s generated_at_unix_milli=%d\n", r.Profile, r.Status, r.GeneratedAtUnixMilli)
	fmt.Fprintf(&b, "secret_source=%s provider=%s resolved_keys=%d unresolved_keys=%d redacted=%t\n", r.SecretSource.Mode, r.SecretSource.Provider, r.SecretSource.ResolvedKeys, r.SecretSource.UnresolvedKeys, r.SecretSource.RawValuesRedacted)
	if len(r.SecretSource.SecretStoreRefs) > 0 {
		fmt.Fprintf(&b, "secret_store_refs=%s\n", strings.Join(r.SecretSource.SecretStoreRefs, ","))
	}
	if len(r.SecretEvidence) > 0 {
		fmt.Fprintf(&b, "secret_evidence_entries=%d\n", len(r.SecretEvidence))
	}
	if r.Manifest.ConsortiumID != "" {
		fmt.Fprintf(&b, "manifest consortium=%s version=%d threshold=%d active=%d set=%s\n", r.Manifest.ConsortiumID, r.Manifest.MembershipVersion, r.Manifest.QuorumThreshold, len(r.Manifest.ActiveValidators), short(r.Manifest.ValidatorSetHash))
	}
	if r.Smoke.ReceiptID != "" {
		fmt.Fprintf(&b, "smoke receipt=%s evidence=%s verification=%s signers=%d restored_db=%s\n", short(r.Smoke.ReceiptID), short(r.Smoke.EvidenceID), r.Smoke.VerificationStatus, r.Smoke.SignerCount, r.Smoke.RestoredDatabase)
	}
	for _, check := range r.Checks {
		fmt.Fprintf(&b, "%s=%s %s\n", check.Name, check.Status, check.Message)
	}
	fmt.Fprintf(&b, "limitations: %s\n", r.Limitations)
	return b.String()
}

func validateManifestReferences(source secretMap, bySensitivity map[string][]SecretReference, spec BootstrapSpec, add func(string, bool, string), unresolved func(string, string)) ManifestSummary {
	currentRefs := bySensitivity[SensitivityManifestCurrent]
	if len(currentRefs) == 0 {
		add("manifest_current_ref", false, "current manifest secret reference required")
		return ManifestSummary{}
	}
	currentMaterial, ok := source.Material(currentRefs[0].Name, currentRefs[0].Key)
	if !ok || !currentMaterial.Resolved {
		unresolved("manifest_current_content", "current manifest content deferred to external secret provider")
		return ManifestSummary{}
	}
	var manifest consortium.Manifest
	if err := json.Unmarshal(currentMaterial.Value, &manifest); err != nil {
		add("manifest_current_parse", false, err.Error())
		return ManifestSummary{}
	}
	if err := manifest.Validate(); err != nil {
		add("manifest_current_validate", false, err.Error())
		return ManifestSummary{}
	}
	hash, err := manifest.Hash()
	if err != nil {
		add("manifest_hash", false, err.Error())
	} else {
		add("manifest_hash", true, "validator set hash computed")
	}
	add("manifest_consortium", manifest.ConsortiumID == spec.ConsortiumID, fmt.Sprintf("got=%q want=%q", manifest.ConsortiumID, spec.ConsortiumID))
	add("manifest_active_validators", len(manifest.ActiveValidatorIDs()) == spec.ValidatorCount, fmt.Sprintf("got=%d want=%d", len(manifest.ActiveValidatorIDs()), spec.ValidatorCount))
	add("manifest_threshold", manifest.QuorumThreshold == spec.QuorumThreshold, fmt.Sprintf("got=%d want=%d", manifest.QuorumThreshold, spec.QuorumThreshold))

	historyManifests := []consortium.Manifest{manifest}
	seenHistoryHashes := map[string]bool{}
	if currentHash, err := manifest.Hash(); err == nil {
		seenHistoryHashes[currentHash] = true
	}
	for _, ref := range bySensitivity[SensitivityManifestHistory] {
		material, ok := source.Material(ref.Name, ref.Key)
		if !ok || !material.Resolved {
			unresolved("manifest_history_"+safeName(ref.Key), "history content deferred to external secret provider")
			continue
		}
		var hist consortium.Manifest
		if err := json.Unmarshal(material.Value, &hist); err != nil {
			add("manifest_history_"+safeName(ref.Key), false, err.Error())
			continue
		}
		histHash, err := hist.Hash()
		if err != nil {
			add("manifest_history_"+safeName(ref.Key), false, err.Error())
			continue
		}
		if seenHistoryHashes[histHash] {
			continue
		}
		seenHistoryHashes[histHash] = true
		historyManifests = append(historyManifests, hist)
	}
	history, err := consortium.NewHistory(historyManifests)
	if err != nil {
		add("manifest_history_validate", false, err.Error())
	} else {
		add("manifest_history_validate", true, fmt.Sprintf("versions=%v", historyVersions(history)))
	}
	return ManifestSummary{
		ConsortiumID:      manifest.ConsortiumID,
		MembershipVersion: manifest.MembershipVersion,
		ValidatorSetHash:  hash,
		QuorumThreshold:   manifest.QuorumThreshold,
		ActiveValidators:  manifest.ActiveValidatorIDs(),
		HistoryVersions:   historyVersions(history),
	}
}

func validateAPIKeyReferences(source secretMap, bySensitivity map[string][]SecretReference, add func(string, bool, string), unresolved func(string, string)) {
	refs := bySensitivity[SensitivityAPIKeys]
	if len(refs) == 0 {
		add("api_keys_ref", false, "API key config secret reference required")
		return
	}
	material, ok := source.Material(refs[0].Name, refs[0].Key)
	if !ok || !material.Resolved {
		unresolved("api_keys_content", "API key content deferred to external secret provider")
		return
	}
	var file apiauth.APIKeyFile
	if err := json.Unmarshal(material.Value, &file); err != nil {
		add("api_keys_parse", false, err.Error())
		return
	}
	if _, err := apiauth.NewAuthenticator(file.Keys); err != nil {
		add("api_keys_validate", false, err.Error())
		return
	}
	have := map[string]bool{}
	for _, key := range file.Keys {
		if key.Disabled {
			continue
		}
		for _, role := range key.Roles {
			have[role] = true
		}
	}
	required := []string{apiauth.RoleAdminRead, apiauth.RoleEvidenceSubmit, apiauth.RoleEvidenceRead, apiauth.RoleEvidenceVerify}
	missing := []string{}
	for _, role := range required {
		if !have[role] {
			missing = append(missing, role)
		}
	}
	add("api_keys_roles", len(missing) == 0, "missing_roles="+strings.Join(missing, ","))
}

func validateTLSReferences(source secretMap, bySensitivity map[string][]SecretReference, spec BootstrapSpec, add func(string, bool, string), unresolved func(string, string)) {
	certRefs := bySensitivity[SensitivityPeerTLSCert]
	if len(certRefs) == 0 {
		add("peer_tls_cert_refs", false, "peer TLS certificate references required")
		return
	}
	seen := map[string]bool{}
	now := time.Now()
	for _, ref := range certRefs {
		if ref.ValidatorID != "" {
			seen[ref.ValidatorID] = true
		}
		material, ok := source.Material(ref.Name, ref.Key)
		checkName := "peer_tls_cert_" + safeName(firstNonEmpty(ref.ValidatorID, ref.Key))
		if !ok || !material.Resolved {
			unresolved(checkName, "certificate content deferred to external secret provider")
			continue
		}
		cert, err := parseCertificate(material.Value)
		if err != nil {
			add(checkName, false, err.Error())
			continue
		}
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			add(checkName, false, "certificate is not currently valid")
			continue
		}
		if ref.ValidatorID != "" {
			expected := consortium.ExpectedTLSURISAN(spec.ConsortiumID, ref.ValidatorID)
			found := false
			for _, uri := range cert.URIs {
				if uri.String() == expected {
					found = true
					break
				}
			}
			add(checkName, found, "expected_uri_san="+expected)
		} else {
			_, err := consortium.ValidatorIDFromTLSURIs(cert.URIs, spec.ConsortiumID)
			add(checkName, err == nil, "validator URI SAN present")
		}
	}
	for _, id := range identity.DefaultValidatorIDs() {
		if !seen[id] && source.HasResolvedContent() {
			add("peer_tls_cert_"+safeName(id), false, "missing validator-specific peer TLS cert reference")
		}
	}
	if len(bySensitivity[SensitivityPeerTLSKey]) == 0 {
		add("peer_tls_key_refs", false, "peer TLS private key references required")
	}
	for _, ref := range bySensitivity[SensitivityPeerTLSKey] {
		material, ok := source.Material(ref.Name, ref.Key)
		checkName := "peer_tls_key_" + safeName(firstNonEmpty(ref.ValidatorID, ref.Key))
		if !ok || !material.Resolved {
			unresolved(checkName, "private key content deferred to external secret provider")
			continue
		}
		if err := parsePrivateKeyPEM(material.Value); err != nil {
			add(checkName, false, err.Error())
			continue
		}
		add(checkName, true, "private key PEM present")
	}
	if len(bySensitivity[SensitivityPeerTLSCA]) == 0 {
		add("peer_tls_ca_ref", false, "peer TLS CA reference required")
	}
	for _, ref := range bySensitivity[SensitivityPeerTLSCA] {
		material, ok := source.Material(ref.Name, ref.Key)
		checkName := "peer_tls_ca_" + safeName(ref.Key)
		if !ok || !material.Resolved {
			unresolved(checkName, "CA content deferred to external secret provider")
			continue
		}
		cert, err := parseCertificate(material.Value)
		if err != nil {
			add(checkName, false, err.Error())
			continue
		}
		add(checkName, cert.IsCA, "CA certificate PEM present")
	}
}

func validateKMSReferences(source secretMap, bySensitivity map[string][]SecretReference, add func(string, bool, string), unresolved func(string, string)) {
	tokenRefs := bySensitivity[SensitivityKMSToken]
	if len(tokenRefs) == 0 {
		add("kms_token_ref", false, "KMS token reference required")
	} else {
		material, ok := source.Material(tokenRefs[0].Name, tokenRefs[0].Key)
		switch {
		case !ok:
			add("kms_token_content", false, "KMS token secret reference missing")
		case !material.Resolved:
			unresolved("kms_token_content", "KMS token content deferred to external secret provider")
		default:
			add("kms_token_content", len(bytes.TrimSpace(material.Value)) > 0, "KMS token is non-empty and redacted")
		}
	}
	caRefs := bySensitivity[SensitivityKMSCA]
	if len(caRefs) == 0 {
		add("kms_ca_ref", false, "KMS CA reference required")
		return
	}
	material, ok := source.Material(caRefs[0].Name, caRefs[0].Key)
	switch {
	case !ok:
		add("kms_ca_content", false, "KMS CA secret reference missing")
	case !material.Resolved:
		unresolved("kms_ca_content", "KMS CA content deferred to external secret provider")
	default:
		cert, err := parseCertificate(material.Value)
		if err != nil {
			add("kms_ca_content", false, err.Error())
		} else {
			add("kms_ca_content", true, "KMS CA certificate PEM present; is_ca="+strconv.FormatBool(cert.IsCA))
		}
	}
}

func validateOverlayCompatibleSecretNames(spec BootstrapSpec, add func(string, bool, string)) {
	requiredNames := map[string]bool{
		"pq-fabric-api-keys":            false,
		"pq-fabric-consortium-manifest": false,
		"pq-fabric-peer-tls":            false,
		"pq-fabric-kms":                 false,
		"pq-fabric-kms-ca":              false,
	}
	for _, ref := range spec.SecretReferences {
		if _, ok := requiredNames[ref.Name]; ok {
			requiredNames[ref.Name] = true
		}
	}
	missing := []string{}
	for name, ok := range requiredNames {
		if !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	add("k8s_overlay_secret_names", len(missing) == 0, "missing="+strings.Join(missing, ","))
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("certificate PEM block not found")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parsePrivateKeyPEM(data []byte) error {
	block, _ := pem.Decode(data)
	if block == nil {
		return errors.New("private key PEM block not found")
	}
	if !strings.Contains(block.Type, "PRIVATE KEY") {
		return fmt.Errorf("PEM block %q is not a private key", block.Type)
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		_, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		return err
	case "EC PRIVATE KEY":
		_, err := x509.ParseECPrivateKey(block.Bytes)
		return err
	default:
		if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
			return err
		}
		return nil
	}
}

func normalizeSecretReference(ref SecretReference) SecretReference {
	ref.Name = strings.TrimSpace(ref.Name)
	ref.Key = strings.TrimSpace(ref.Key)
	ref.MountPath = strings.TrimSpace(ref.MountPath)
	ref.Sensitivity = strings.TrimSpace(ref.Sensitivity)
	ref.ValidatorID = strings.TrimSpace(ref.ValidatorID)
	ref.RemoteRef = strings.TrimSpace(ref.RemoteRef)
	return ref
}

func reportStatus(checks []Check) string {
	unresolved := false
	for _, check := range checks {
		if check.Status == StatusFail || !check.OK {
			return StatusFail
		}
		if check.Status == StatusUnresolved {
			unresolved = true
		}
	}
	if unresolved {
		return "pass_with_unresolved"
	}
	return StatusPass
}

func historyVersions(history consortium.History) []uint64 {
	out := make([]uint64, 0, len(history.Manifests))
	for _, manifest := range history.Manifests {
		out = append(out, manifest.MembershipVersion)
	}
	return out
}

func safeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}

func short(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

type secretMaterial struct {
	Value     []byte
	Resolved  bool
	RemoteRef string
	StoreRef  string
	Source    string
}

type secretMap map[string]map[string]secretMaterial

func (m secretMap) Material(name, key string) (secretMaterial, bool) {
	keys, ok := m[name]
	if !ok {
		return secretMaterial{}, false
	}
	material, ok := keys[key]
	return material, ok
}

func (m secretMap) HasResolvedContent() bool {
	for _, keys := range m {
		for _, material := range keys {
			if material.Resolved {
				return true
			}
		}
	}
	return false
}

func (m secretMap) Summary(source SecretSourceSpec) SecretSourceSummary {
	mode := strings.TrimSpace(source.Mode)
	if mode == "" {
		mode = SecretModeExternalSecretContract
	}
	secretNames := make([]string, 0, len(m))
	storeRefs := map[string]struct{}{}
	resolvedKeys := 0
	unresolvedKeys := 0
	for name, keys := range m {
		secretNames = append(secretNames, name)
		for _, material := range keys {
			if material.Resolved {
				resolvedKeys++
			} else {
				unresolvedKeys++
			}
			if material.StoreRef != "" {
				storeRefs[material.StoreRef] = struct{}{}
			}
		}
	}
	sort.Strings(secretNames)
	secretStoreRefs := make([]string, 0, len(storeRefs))
	for ref := range storeRefs {
		secretStoreRefs = append(secretStoreRefs, ref)
	}
	sort.Strings(secretStoreRefs)
	return SecretSourceSummary{
		Mode:              mode,
		Provider:          secretProvider(source),
		Path:              redactPath(source.Path),
		FileTreeRoot:      redactPath(source.FileTreeRoot),
		ResolvedSecrets:   len(m),
		ResolvedKeys:      resolvedKeys,
		UnresolvedKeys:    unresolvedKeys,
		SecretNames:       secretNames,
		SecretStoreRefs:   secretStoreRefs,
		RawValuesRedacted: true,
	}
}

func secretProvider(source SecretSourceSpec) string {
	if strings.TrimSpace(source.Provider) != "" {
		return strings.TrimSpace(source.Provider)
	}
	switch strings.TrimSpace(source.Mode) {
	case SecretModeFileTree:
		return "mounted-file-tree"
	case SecretModeKubernetesSecret:
		return "kubernetes-secret"
	case SecretModeExternalSecretContract:
		return "external-secrets-operator"
	default:
		return ""
	}
}

func buildSecretEvidence(source secretMap, refs []SecretReference) []SecretMaterialEvidence {
	out := make([]SecretMaterialEvidence, 0, len(refs))
	for _, ref := range refs {
		ref = normalizeSecretReference(ref)
		evidence := SecretMaterialEvidence{
			Name:        ref.Name,
			Key:         ref.Key,
			MountPath:   ref.MountPath,
			Sensitivity: ref.Sensitivity,
			ValidatorID: ref.ValidatorID,
			Required:    ref.Required,
			Status:      "missing",
			Redaction:   "raw_value_redacted",
		}
		material, ok := source.Material(ref.Name, ref.Key)
		if !ok {
			if !ref.Required {
				evidence.Status = "optional_missing"
			}
			out = append(out, evidence)
			continue
		}
		evidence.Source = firstNonEmpty(material.Source, "unknown")
		evidence.StoreRef = material.StoreRef
		evidence.RemoteRef = material.RemoteRef
		evidence.Resolved = material.Resolved
		if !material.Resolved {
			evidence.Status = StatusUnresolved
			evidence.Redaction = "external_reference_only"
			out = append(out, evidence)
			continue
		}
		evidence.SizeBytes = len(material.Value)
		evidence.ContentStatus = contentStatus(ref.Sensitivity, material.Value)
		if len(material.Value) == 0 {
			evidence.Status = StatusFail
			out = append(out, evidence)
			continue
		}
		evidence.Status = StatusPass
		evidence.SafeFingerprint = safeSecretFingerprint(ref.Sensitivity, material.Value)
		if evidence.SafeFingerprint == "" {
			evidence.Redaction = "secret_material_not_fingerprinted"
		}
		out = append(out, evidence)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Key < out[j].Key
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func safeSecretFingerprint(sensitivity string, value []byte) string {
	switch sensitivity {
	case SensitivityManifestCurrent, SensitivityManifestHistory, SensitivityPeerTLSCert, SensitivityPeerTLSCA, SensitivityKMSCA:
		sum := sha256.Sum256(value)
		return "sha256:" + hex.EncodeToString(sum[:])
	default:
		return ""
	}
}

func contentStatus(sensitivity string, value []byte) string {
	switch sensitivity {
	case SensitivityManifestCurrent, SensitivityManifestHistory, SensitivityAPIKeys:
		if json.Valid(value) {
			return "json"
		}
		return "invalid-json"
	case SensitivityPeerTLSCert, SensitivityPeerTLSCA, SensitivityKMSCA:
		if _, err := parseCertificate(value); err != nil {
			return "invalid-certificate-pem"
		}
		return "certificate-pem"
	case SensitivityPeerTLSKey:
		if err := parsePrivateKeyPEM(value); err != nil {
			return "invalid-private-key-pem"
		}
		return "private-key-pem"
	case SensitivityKMSToken:
		if len(bytes.TrimSpace(value)) == 0 {
			return "empty-token"
		}
		return "non-empty-token"
	default:
		if len(value) == 0 {
			return "empty"
		}
		return "present"
	}
}

func resolveSecretSource(spec BootstrapSpec) (secretMap, []Check) {
	source := spec.SecretSource
	source.Mode = strings.TrimSpace(source.Mode)
	if source.Mode == "" {
		source.Mode = SecretModeExternalSecretContract
	}
	switch source.Mode {
	case SecretModeFileTree:
		return resolveFileTree(source, spec.SecretReferences)
	case SecretModeKubernetesSecret:
		return resolveKubernetesSecrets(source)
	case SecretModeExternalSecretContract:
		return resolveExternalSecrets(source)
	default:
		return secretMap{}, []Check{{Name: "secret_source_mode", Status: StatusFail, OK: false, Message: "unsupported mode " + strconv.Quote(source.Mode)}}
	}
}

func resolveFileTree(source SecretSourceSpec, refs []SecretReference) (secretMap, []Check) {
	out := secretMap{}
	checks := []Check{{Name: "secret_source_mode", Status: StatusPass, OK: true, Message: SecretModeFileTree}}
	if strings.TrimSpace(source.FileTreeRoot) == "" {
		checks = append(checks, Check{Name: "file_tree_root", Status: StatusFail, OK: false, Message: "file_tree_root is required"})
		return out, checks
	}
	for _, ref := range refs {
		ref = normalizeSecretReference(ref)
		if ref.Name == "" || ref.Key == "" || ref.MountPath == "" {
			continue
		}
		path := filepath.Join(source.FileTreeRoot, strings.TrimPrefix(filepath.Clean(ref.MountPath), string(filepath.Separator)))
		data, err := os.ReadFile(path)
		if err != nil {
			if ref.Required {
				checks = append(checks, Check{Name: "file_tree_" + safeName(ref.Name+"_"+ref.Key), Status: StatusFail, OK: false, Message: err.Error()})
			}
			continue
		}
		putSecret(out, ref.Name, ref.Key, secretMaterial{Value: data, Resolved: true, Source: SecretModeFileTree})
	}
	return out, checks
}

func resolveKubernetesSecrets(source SecretSourceSpec) (secretMap, []Check) {
	out := secretMap{}
	checks := []Check{{Name: "secret_source_mode", Status: StatusPass, OK: true, Message: SecretModeKubernetesSecret}}
	if source.Path == "" {
		checks = append(checks, Check{Name: "kubernetes_secret_manifest", Status: StatusFail, OK: false, Message: "path is required"})
		return out, checks
	}
	data, err := os.ReadFile(source.Path)
	if err != nil {
		checks = append(checks, Check{Name: "kubernetes_secret_manifest", Status: StatusFail, OK: false, Message: err.Error()})
		return out, checks
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc kubeSecretDoc
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			checks = append(checks, Check{Name: "kubernetes_secret_parse", Status: StatusFail, OK: false, Message: err.Error()})
			break
		}
		if doc.Kind != "Secret" || doc.Metadata.Name == "" {
			continue
		}
		for key, value := range doc.StringData {
			putSecret(out, doc.Metadata.Name, key, secretMaterial{Value: []byte(value), Resolved: true, Source: SecretModeKubernetesSecret})
		}
		for key, value := range doc.Data {
			decoded, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				checks = append(checks, Check{Name: "kubernetes_secret_data_" + safeName(doc.Metadata.Name+"_"+key), Status: StatusFail, OK: false, Message: "data value is not base64"})
				continue
			}
			putSecret(out, doc.Metadata.Name, key, secretMaterial{Value: decoded, Resolved: true, Source: SecretModeKubernetesSecret})
		}
	}
	return out, checks
}

func resolveExternalSecrets(source SecretSourceSpec) (secretMap, []Check) {
	out := secretMap{}
	checks := []Check{{Name: "secret_source_mode", Status: StatusPass, OK: true, Message: SecretModeExternalSecretContract}}
	if source.Path == "" {
		checks = append(checks, Check{Name: "external_secret_contract", Status: StatusFail, OK: false, Message: "path is required"})
		return out, checks
	}
	data, err := os.ReadFile(source.Path)
	if err != nil {
		checks = append(checks, Check{Name: "external_secret_contract", Status: StatusFail, OK: false, Message: err.Error()})
		return out, checks
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc externalSecretDoc
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			checks = append(checks, Check{Name: "external_secret_parse", Status: StatusFail, OK: false, Message: err.Error()})
			break
		}
		if doc.Kind == "" || doc.Metadata.Name == "" {
			continue
		}
		storeRef := ""
		if doc.Spec.SecretStoreRef.Kind != "" && doc.Spec.SecretStoreRef.Name != "" {
			storeRef = strings.TrimSpace(doc.Spec.SecretStoreRef.Kind + "/" + doc.Spec.SecretStoreRef.Name)
		}
		if doc.Spec.SecretStoreRef.Name == "" {
			checks = append(checks, Check{Name: "external_secret_store_ref_" + safeName(doc.Metadata.Name), Status: StatusFail, OK: false, Message: "secretStoreRef.name is required"})
		}
		if doc.Spec.SecretStoreRef.Kind == "" {
			checks = append(checks, Check{Name: "external_secret_store_kind_" + safeName(doc.Metadata.Name), Status: StatusFail, OK: false, Message: "secretStoreRef.kind is required"})
		}
		targetName := doc.Spec.Target.Name
		if targetName == "" {
			targetName = doc.Metadata.Name
		}
		for _, item := range doc.Spec.Data {
			if item.SecretKey == "" {
				continue
			}
			if item.RemoteRef.Key == "" {
				checks = append(checks, Check{Name: "external_secret_remote_ref_" + safeName(targetName+"_"+item.SecretKey), Status: StatusFail, OK: false, Message: "remoteRef.key is required"})
				continue
			}
			remote := item.RemoteRef.Key
			if item.RemoteRef.Property != "" {
				remote += "#" + item.RemoteRef.Property
			}
			putSecret(out, targetName, item.SecretKey, secretMaterial{Resolved: false, RemoteRef: remote, StoreRef: storeRef, Source: SecretModeExternalSecretContract})
		}
	}
	return out, checks
}

func putSecret(out secretMap, name, key string, material secretMaterial) {
	if out[name] == nil {
		out[name] = map[string]secretMaterial{}
	}
	out[name][key] = material
}

func redactPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(path), "secret") || strings.Contains(strings.ToLower(path), "token") {
		return "[redacted-path]"
	}
	return path
}

type kubeSecretDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Data       map[string]string `yaml:"data"`
	StringData map[string]string `yaml:"stringData"`
}

type externalSecretDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		SecretStoreRef struct {
			Name string `yaml:"name"`
			Kind string `yaml:"kind"`
		} `yaml:"secretStoreRef"`
		Target struct {
			Name string `yaml:"name"`
		} `yaml:"target"`
		Data []struct {
			SecretKey string `yaml:"secretKey"`
			RemoteRef struct {
				Key      string `yaml:"key"`
				Property string `yaml:"property"`
			} `yaml:"remoteRef"`
		} `yaml:"data"`
	} `yaml:"spec"`
}
