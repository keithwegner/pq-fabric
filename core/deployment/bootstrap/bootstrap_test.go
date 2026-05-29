package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keithwegner/pq-fabric/core/deployment"
)

func TestValidateExternalSecretContractPassesWithUnresolvedContent(t *testing.T) {
	spec := BootstrapSpec{
		SchemaVersion:       SchemaVersion,
		Profile:             deployment.ProfileProductionPilot,
		ConsortiumID:        "test-consortium",
		ValidatorCount:      7,
		QuorumThreshold:     5,
		SecretSource:        SecretSourceSpec{Mode: SecretModeExternalSecretContract, Path: filepath.Join("..", "..", "..", "deployments", "secrets", "external-secret-contract.example.yaml")},
		SecretReferences:    pilotSecretReferences(),
		DatabaseURLTemplate: "/data/${NODE_ID}/validator.db",
		KMS: KMSBootstrapSpec{
			Provider:      "cloud-kms",
			Endpoint:      "https://kms.example.invalid",
			KeyIDTemplate: "manifest-signing-key-ref",
		},
	}
	report := Validate(context.Background(), spec)
	if !report.OK() {
		data, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("expected unresolved external secret contract to pass, got:\n%s", data)
	}
	if report.Status != "pass_with_unresolved" {
		t.Fatalf("expected pass_with_unresolved, got %s", report.Status)
	}
	if report.SecretSource.UnresolvedKeys == 0 {
		t.Fatalf("expected unresolved external secret keys: %+v", report.SecretSource)
	}
	if len(report.SecretSource.SecretStoreRefs) != 1 || report.SecretSource.SecretStoreRefs[0] != "ClusterSecretStore/pq-fabric-production-pilot-secret-store" {
		t.Fatalf("expected external secret store ref evidence: %+v", report.SecretSource.SecretStoreRefs)
	}
	if len(report.SecretEvidence) == 0 {
		t.Fatalf("expected redacted secret evidence entries")
	}
	for _, evidence := range report.SecretEvidence {
		if evidence.Redaction == "" || evidence.Status == "" {
			t.Fatalf("secret evidence must be redacted and statused: %+v", evidence)
		}
		if strings.Contains(evidence.Redaction, "bootstrap-smoke-admin-token") {
			t.Fatalf("secret evidence leaked a token: %+v", evidence)
		}
	}
}

func TestExternalSecretContractRejectsMissingStoreRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "external-secret.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: pq-fabric-api-keys
spec:
  target:
    name: pq-fabric-api-keys
  data:
    - secretKey: api-keys.json
      remoteRef:
        key: pq-fabric/production-pilot/api
        property: api-keys.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, checks := resolveExternalSecrets(SecretSourceSpec{Mode: SecretModeExternalSecretContract, Path: path})
	if !hasCheckStatus(checks, "external_secret_store_ref_pq_fabric_api_keys", StatusFail) {
		t.Fatalf("expected missing store ref failure: %+v", checks)
	}
	if !hasCheckStatus(checks, "external_secret_store_kind_pq_fabric_api_keys", StatusFail) {
		t.Fatalf("expected missing store kind failure: %+v", checks)
	}
}

func TestValidateRejectsProductionUnsafeBootstrapSpec(t *testing.T) {
	spec := BootstrapSpec{
		SchemaVersion:       SchemaVersion,
		Profile:             deployment.ProfileProductionPilot,
		ConsortiumID:        "test-consortium",
		ValidatorCount:      7,
		QuorumThreshold:     5,
		SecretSource:        SecretSourceSpec{Mode: SecretModeExternalSecretContract, Path: filepath.Join("..", "..", "..", "deployments", "secrets", "external-secret-contract.example.yaml")},
		SecretReferences:    pilotSecretReferences(),
		DatabaseURLTemplate: "/data/${NODE_ID}/validator.db",
		KMS: KMSBootstrapSpec{
			Provider:      "local",
			Endpoint:      "http://kms.example.invalid",
			KeyIDTemplate: "manifest-signing-key-ref",
			AllowInsecure: true,
		},
		PublicExitRouting: true,
		MainnetAnchoring:  true,
	}
	report := Validate(context.Background(), spec)
	if report.OK() {
		t.Fatalf("expected unsafe bootstrap spec to fail")
	}
	for _, want := range []string{"public_exits_disabled", "mainnet_anchoring_disabled", "kms_provider", "kms_insecure_disabled"} {
		if !hasFailedCheck(report, want) {
			t.Fatalf("expected failed check %s in %+v", want, report.Checks)
		}
	}
}

func TestSmokeRunsSevenValidatorProductionFlowAndRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	spec := BootstrapSpec{
		SchemaVersion:       SchemaVersion,
		Profile:             deployment.ProfileProductionPilot,
		ConsortiumID:        "test-consortium",
		ValidatorCount:      7,
		QuorumThreshold:     5,
		SecretSource:        SecretSourceSpec{Mode: SecretModeExternalSecretContract, Path: filepath.Join("..", "..", "..", "deployments", "secrets", "external-secret-contract.example.yaml")},
		SecretReferences:    pilotSecretReferences(),
		DatabaseURLTemplate: "/data/${NODE_ID}/validator.db",
		KMS: KMSBootstrapSpec{
			Provider:      "cloud-kms",
			Endpoint:      "https://kms.example.invalid",
			KeyIDTemplate: "manifest-signing-key-ref",
		},
	}
	report, err := Smoke(ctx, spec)
	if err != nil {
		data, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("smoke failed: %v\n%s", err, data)
	}
	if !report.OK() {
		data, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("expected smoke report to pass, got:\n%s", data)
	}
	if report.Smoke.VerificationStatus != "valid" || report.Smoke.SignerCount < 5 || report.Smoke.RestoredDatabase == "" {
		t.Fatalf("unexpected smoke evidence: %+v", report.Smoke)
	}
	if _, err := os.Stat(report.Smoke.RestoredDatabase); err != nil {
		t.Fatalf("expected restored database evidence to exist: %v", err)
	}
	for _, want := range []string{"smoke_submit_receipt", "smoke_verify_receipt", "smoke_restore_verify"} {
		if !hasPassingCheck(report, want) {
			t.Fatalf("expected passing check %s in %+v", want, report.Checks)
		}
	}
}

func hasFailedCheck(report BootstrapReport, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name && !check.OK {
			return true
		}
	}
	return false
}

func hasPassingCheck(report BootstrapReport, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.OK {
			return true
		}
	}
	return false
}

func hasCheckStatus(checks []Check, name, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
