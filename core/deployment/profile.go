package deployment

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/keithwegner/pq-fabric/core/consortium"
	"github.com/keithwegner/pq-fabric/core/identity"
)

const (
	ProfileLocal           = "local"
	ProfileStaging         = "staging"
	ProfileProductionPilot = "production-pilot"
)

type ValidationInput struct {
	Profile           string            `json:"profile"`
	Values            map[string]string `json:"values,omitempty"`
	AllowPlaceholders bool              `json:"allow_placeholders,omitempty"`
}

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type Report struct {
	GeneratedAtUnixMilli int64   `json:"generated_at_unix_milli"`
	Profile              string  `json:"profile"`
	Status               string  `json:"status"`
	Checks               []Check `json:"checks"`
}

func ValidateProfile(input ValidationInput) Report {
	profile := strings.ToLower(strings.TrimSpace(input.Profile))
	if profile == "" {
		profile = ProfileLocal
	}
	values := normalizeValues(input.Values)
	checks := []Check{{Name: "profile_known", OK: isKnownProfile(profile), Message: profile}}
	switch profile {
	case ProfileLocal:
		checks = append(checks, validateLocal(values)...)
	case ProfileStaging:
		checks = append(checks, validateStaging(values, input.AllowPlaceholders)...)
	case ProfileProductionPilot:
		checks = append(checks, validateProductionPilot(values, input.AllowPlaceholders)...)
	}
	status := "pass"
	for _, check := range checks {
		if !check.OK {
			status = "fail"
			break
		}
	}
	return Report{
		GeneratedAtUnixMilli: time.Now().UnixMilli(),
		Profile:              profile,
		Status:               status,
		Checks:               checks,
	}
}

func (r Report) OK() bool {
	return r.Status == "pass"
}

func (r Report) MarshalText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric pilot deployment profile check\n")
	fmt.Fprintf(&b, "profile=%s status=%s generated_at_unix_milli=%d\n", r.Profile, r.Status, r.GeneratedAtUnixMilli)
	for _, check := range r.Checks {
		status := "pass"
		if !check.OK {
			status = "fail"
		}
		fmt.Fprintf(&b, "%s=%s %s\n", check.Name, status, check.Message)
	}
	return b.String()
}

func (r Report) MarshalJSONIndented(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r)
}

func ParseEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env line %q", line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid env line %q", line)
		}
		values[key] = trimEnvValue(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func MergeValues(base map[string]string, overlays ...map[string]string) map[string]string {
	out := normalizeValues(base)
	for _, overlay := range overlays {
		for key, value := range overlay {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

func validateLocal(values map[string]string) []Check {
	return []Check{
		checkBoolFalse("production_mode_local", first(values, "PQ_FABRIC_PRODUCTION_MODE", "PQFABRIC_PRODUCTION_MODE"), true),
		checkOneOf("storage_local", first(values, "STORAGE", "PQFABRIC_STORAGE"), []string{"", "memory", "durable", "sqlite"}),
		checkIntEquals("quorum_threshold", first(values, "THRESHOLD", "QUORUM_THRESHOLD"), 5),
		checkBoolFalse("public_exits_disabled", first(values, "PUBLIC_EXIT_ROUTING", "ENABLE_PUBLIC_EXITS"), true),
	}
}

func validateStaging(values map[string]string, allowPlaceholders bool) []Check {
	checks := validateProductionLike(values, allowPlaceholders)
	checks = append(checks,
		checkOneOf("anchor_backend_staging", first(values, "ANCHOR_BACKEND"), []string{"mock", "polygon-testnet", ""}),
		checkBoolFalse("mainnet_anchoring_disabled", first(values, "POLYGON_MAINNET_ENABLED", "MAINNET_ANCHORING_ENABLED"), true),
	)
	return checks
}

func validateProductionPilot(values map[string]string, allowPlaceholders bool) []Check {
	checks := validateProductionLike(values, allowPlaceholders)
	checks = append(checks,
		checkOneOf("anchor_backend_production_pilot", first(values, "ANCHOR_BACKEND"), []string{"mock", "disabled", ""}),
		checkBoolFalse("mainnet_anchoring_disabled", first(values, "POLYGON_MAINNET_ENABLED", "MAINNET_ANCHORING_ENABLED"), true),
	)
	return checks
}

func validateProductionLike(values map[string]string, allowPlaceholders bool) []Check {
	checks := []Check{
		checkBoolTrue("production_mode", first(values, "PQ_FABRIC_PRODUCTION_MODE", "PQFABRIC_PRODUCTION_MODE")),
		checkEquals("crypto_suite_pq", first(values, "PQ_FABRIC_CRYPTO_SUITE"), "pq"),
		checkEquals("storage_sqlite", first(values, "STORAGE", "PQFABRIC_STORAGE"), "sqlite"),
		checkRequired("database_url", first(values, "PQFABRIC_DATABASE_URL")),
		checkRequired("scoped_api_keys_file", first(values, "PQFABRIC_API_KEYS_FILE")),
		checkRequired("manifest_current", first(values, "PQFABRIC_CONSORTIUM_MANIFEST")),
		checkRequired("manifest_history", first(values, "PQFABRIC_CONSORTIUM_MANIFEST_HISTORY")),
		checkEquals("signer_provider_cloud_kms", first(values, "PQFABRIC_SIGNER_PROVIDER"), "cloud-kms"),
		checkRequired("kms_endpoint", first(values, "PQFABRIC_KMS_ENDPOINT")),
		checkRequired("kms_key_id_or_ref", first(values, "PQFABRIC_KMS_KEY_ID", "PQFABRIC_KMS_KEY_REF")),
		checkRequired("peer_tls_cert_file", first(values, "PQFABRIC_PEER_TLS_CERT_FILE")),
		checkRequired("peer_tls_key_file", first(values, "PQFABRIC_PEER_TLS_KEY_FILE")),
		checkRequired("peer_tls_ca_file", first(values, "PQFABRIC_PEER_TLS_CA_FILE")),
		checkEquals("log_format_json", first(values, "PQFABRIC_LOG_FORMAT"), "json"),
		checkIntEquals("quorum_threshold", first(values, "THRESHOLD", "QUORUM_THRESHOLD"), 5),
		checkBoolFalse("public_exits_disabled", first(values, "PUBLIC_EXIT_ROUTING", "ENABLE_PUBLIC_EXITS"), false),
		checkOTEL(values),
		checkManifestActiveValidators(values, allowPlaceholders),
	}
	return checks
}

func checkManifestActiveValidators(values map[string]string, allowPlaceholders bool) Check {
	path := strings.TrimSpace(first(values, "PQFABRIC_CONSORTIUM_MANIFEST"))
	if path != "" {
		if manifest, err := consortium.LoadManifest(path); err == nil {
			got := len(manifest.ActiveValidatorIDs())
			want := len(identity.DefaultValidatorIDs())
			return Check{Name: "active_validators", OK: got == want, Message: fmt.Sprintf("manifest active=%d expected=%d", got, want)}
		} else if !errors.Is(err, os.ErrNotExist) && !allowPlaceholders {
			return Check{Name: "active_validators", OK: false, Message: err.Error()}
		}
	}
	if raw := first(values, "PQFABRIC_ACTIVE_VALIDATORS", "VALIDATOR_COUNT"); raw != "" {
		got, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return Check{Name: "active_validators", OK: false, Message: "active validator count is not an integer"}
		}
		want := len(identity.DefaultValidatorIDs())
		return Check{Name: "active_validators", OK: got == want, Message: fmt.Sprintf("declared active=%d expected=%d", got, want)}
	}
	if allowPlaceholders {
		return Check{Name: "active_validators", OK: false, Message: "production-pilot placeholder config must declare PQFABRIC_ACTIVE_VALIDATORS=7 or provide a readable manifest"}
	}
	return Check{Name: "active_validators", OK: false, Message: "readable consortium manifest is required"}
}

func checkOTEL(values map[string]string) Check {
	enabled := parseBool(first(values, "PQFABRIC_OTEL_ENABLED"))
	endpoint := strings.TrimSpace(first(values, "PQFABRIC_OTEL_EXPORTER_OTLP_ENDPOINT"))
	if enabled && endpoint == "" {
		return Check{Name: "otel_endpoint_when_enabled", OK: false, Message: "OTEL is enabled but endpoint is empty"}
	}
	return Check{Name: "otel_endpoint_when_enabled", OK: true, Message: "OTEL disabled or endpoint configured"}
}

func checkRequired(name, value string) Check {
	ok := strings.TrimSpace(value) != ""
	return Check{Name: name, OK: ok, Message: boolMessage(ok, "configured", "missing")}
}

func checkEquals(name, value, want string) Check {
	got := strings.ToLower(strings.TrimSpace(value))
	ok := got == strings.ToLower(strings.TrimSpace(want))
	return Check{Name: name, OK: ok, Message: fmt.Sprintf("got=%q want=%q", got, want)}
}

func checkOneOf(name, value string, allowed []string) Check {
	got := strings.ToLower(strings.TrimSpace(value))
	for _, item := range allowed {
		if got == strings.ToLower(strings.TrimSpace(item)) {
			return Check{Name: name, OK: true, Message: "got=" + strconv.Quote(got)}
		}
	}
	return Check{Name: name, OK: false, Message: "got=" + strconv.Quote(got)}
}

func checkIntEquals(name, value string, want int) Check {
	got, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return Check{Name: name, OK: false, Message: "missing or not an integer"}
	}
	return Check{Name: name, OK: got == want, Message: fmt.Sprintf("got=%d want=%d", got, want)}
}

func checkBoolTrue(name, value string) Check {
	ok := parseBool(value)
	return Check{Name: name, OK: ok, Message: boolMessage(ok, "true", "must be true")}
}

func checkBoolFalse(name, value string, allowEmpty bool) Check {
	trimmed := strings.TrimSpace(value)
	ok := (!parseBool(trimmed) && trimmed != "") || (allowEmpty && trimmed == "")
	return Check{Name: name, OK: ok, Message: boolMessage(ok, "false", "must be false")}
}

func isKnownProfile(profile string) bool {
	switch profile {
	case ProfileLocal, ProfileStaging, ProfileProductionPilot:
		return true
	default:
		return false
	}
}

func normalizeValues(values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func first(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return ""
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func boolMessage(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func trimEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
}
