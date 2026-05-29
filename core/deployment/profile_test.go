package deployment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keithwegner/pq-fabric/core/consortium"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
)

func TestValidateLocalProfileAllowsDevDurableStorage(t *testing.T) {
	report := ValidateProfile(ValidationInput{Profile: ProfileLocal, Values: map[string]string{
		"PQ_FABRIC_PRODUCTION_MODE": "0",
		"PQ_FABRIC_CRYPTO_SUITE":    "dev",
		"STORAGE":                   "durable",
		"QUORUM_THRESHOLD":          "5",
		"PUBLIC_EXIT_ROUTING":       "false",
	}})
	if !report.OK() {
		t.Fatalf("expected local profile to pass: %+v", report)
	}
}

func TestValidateProductionPilotRejectsUnsafeRuntimeChoices(t *testing.T) {
	values := productionPilotValues(t)
	for name, mutate := range map[string]func(map[string]string){
		"dev_crypto":       func(v map[string]string) { v["PQ_FABRIC_CRYPTO_SUITE"] = "dev" },
		"durable_storage":  func(v map[string]string) { v["STORAGE"] = "durable" },
		"missing_db":       func(v map[string]string) { v["PQFABRIC_DATABASE_URL"] = "" },
		"missing_history":  func(v map[string]string) { v["PQFABRIC_CONSORTIUM_MANIFEST_HISTORY"] = "" },
		"local_signer":     func(v map[string]string) { v["PQFABRIC_SIGNER_PROVIDER"] = "local" },
		"bad_threshold":    func(v map[string]string) { v["QUORUM_THRESHOLD"] = "4" },
		"public_exits":     func(v map[string]string) { v["PUBLIC_EXIT_ROUTING"] = "true" },
		"mainnet_anchor":   func(v map[string]string) { v["POLYGON_MAINNET_ENABLED"] = "true" },
		"bad_log_format":   func(v map[string]string) { v["PQFABRIC_LOG_FORMAT"] = "text" },
		"otel_no_endpoint": func(v map[string]string) { v["PQFABRIC_OTEL_ENABLED"] = "true" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := copyValues(values)
			mutate(candidate)
			report := ValidateProfile(ValidationInput{Profile: ProfileProductionPilot, Values: candidate})
			if report.OK() {
				t.Fatalf("expected production-pilot profile to fail for %s: %+v", name, report)
			}
		})
	}
}

func TestValidateProductionPilotRequiresSevenActiveValidators(t *testing.T) {
	values := productionPilotValues(t)
	values["PQFABRIC_ACTIVE_VALIDATORS"] = ""
	values["PQFABRIC_CONSORTIUM_MANIFEST"] = manifestFile(t, identity.DefaultValidatorIDs()[:6])
	report := ValidateProfile(ValidationInput{Profile: ProfileProductionPilot, Values: values})
	if report.OK() {
		t.Fatalf("expected six active validators to fail: %+v", report)
	}
	if !strings.Contains(report.MarshalText(), "active_validators=fail") {
		t.Fatalf("expected active validator failure in report:\n%s", report.MarshalText())
	}
	values["PQFABRIC_CONSORTIUM_MANIFEST"] = manifestFile(t, identity.DefaultValidatorIDs())
	report = ValidateProfile(ValidationInput{Profile: ProfileProductionPilot, Values: values})
	if !report.OK() {
		t.Fatalf("expected seven active validators to pass: %+v", report)
	}
}

func TestParseEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.env")
	if err := os.WriteFile(path, []byte("A=one\nB=\"two\"\n# ignored\nC='three'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	values, err := ParseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "one" || values["B"] != "two" || values["C"] != "three" {
		t.Fatalf("unexpected env values: %+v", values)
	}
}

func productionPilotValues(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"PQ_FABRIC_PRODUCTION_MODE":               "true",
		"PQ_FABRIC_CRYPTO_SUITE":                  "pq",
		"STORAGE":                                 "sqlite",
		"PQFABRIC_DATABASE_URL":                   "file:/data/validator.db",
		"PQFABRIC_API_KEYS_FILE":                  "/etc/pq-fabric/secrets/api-keys.json",
		"PQFABRIC_CONSORTIUM_MANIFEST":            manifestFile(t, identity.DefaultValidatorIDs()),
		"PQFABRIC_CONSORTIUM_MANIFEST_HISTORY":    "/etc/pq-fabric/manifest/history.json",
		"PQFABRIC_SIGNER_PROVIDER":                "cloud-kms",
		"PQFABRIC_KMS_ENDPOINT":                   "https://kms.example.invalid",
		"PQFABRIC_KMS_KEY_ID":                     "kms://example/validator-1",
		"PQFABRIC_PEER_TLS_CERT_FILE":             "/etc/pq-fabric/tls/tls.crt",
		"PQFABRIC_PEER_TLS_KEY_FILE":              "/etc/pq-fabric/tls/tls.key",
		"PQFABRIC_PEER_TLS_CA_FILE":               "/etc/pq-fabric/tls/ca.crt",
		"PQFABRIC_LOG_FORMAT":                     "json",
		"PQFABRIC_OTEL_ENABLED":                   "false",
		"QUORUM_THRESHOLD":                        "5",
		"PUBLIC_EXIT_ROUTING":                     "false",
		"ANCHOR_BACKEND":                          "mock",
		"POLYGON_MAINNET_ENABLED":                 "false",
		"PQFABRIC_ACTIVE_VALIDATORS":              "7",
		"PQFABRIC_OTEL_EXPORTER_OTLP_ENDPOINT":    "",
		"PQFABRIC_OTEL_EXPORTER_OTLP_INSECURE":    "false",
		"PQFABRIC_OTEL_EXPORTER_OTLP_HEADERS":     "",
		"PQFABRIC_OTEL_SERVICE_NAME":              "pq-fabric-validator",
		"PQFABRIC_RATE_LIMIT_PER_MINUTE":          "60",
		"PQFABRIC_RATE_LIMIT_BURST":               "20",
		"PQFABRIC_PEER_TLS_EXPECTED_URI_SAN_ROOT": "spiffe://example-consortium/validator/",
	}
}

func copyValues(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func manifestFile(t *testing.T, activeIDs []string) string {
	t.Helper()
	selected := cryptosuite.MustLookup(string(cryptosuite.Dev))
	urls := map[string]string{}
	for _, id := range identity.DefaultValidatorIDs() {
		urls[id] = "https://" + id + ".example.invalid"
	}
	identities, err := identity.ValidatorIdentitiesForSuite(urls, selected)
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]struct{}{}
	for _, id := range activeIDs {
		active[id] = struct{}{}
	}
	manifest := consortium.ManifestFromIdentities("example-consortium", 1, 5, identities, identity.DefaultValidatorIDs())
	for i := range manifest.Validators {
		_, manifest.Validators[i].Active = active[manifest.Validators[i].ID]
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
