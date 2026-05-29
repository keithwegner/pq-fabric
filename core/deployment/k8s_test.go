package deployment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionPilotOverlayContainsSecretRefsOpsPortAndSecurityContext(t *testing.T) {
	root := filepath.Join("..", "..")
	patch := readWorkspaceFile(t, root, "deployments/k8s/overlays/production-pilot/validator-production-patch.yaml")
	config := readWorkspaceFile(t, root, "deployments/k8s/overlays/production-pilot/config-production-patch.yaml")
	combined := patch + "\n" + config
	for _, want := range []string{
		"secretKeyRef:",
		"secretName: pq-fabric-api-keys",
		"secretName: pq-fabric-consortium-manifest",
		"secretName: pq-fabric-peer-tls",
		"PQFABRIC_PEER_TLS_CERT_FILE=\"/etc/pq-fabric/tls/${NODE_ID}.crt\"",
		"key: validator-1.crt",
		"key: validator-7.key",
		"path: history/v1.json",
		"PQFABRIC_OPS_LISTEN_ADDR",
		"containerPort: 8090",
		"path: /readyz",
		"path: /livez",
		"readOnlyRootFilesystem: true",
		"runAsNonRoot: true",
		"resources:",
		"storageClassName: pq-fabric-pilot-retain",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("production-pilot overlay missing %q:\n%s", want, combined)
		}
	}
	if strings.Contains(combined, "api-keys.example") || strings.Contains(combined, "replace-with") {
		t.Fatalf("production-pilot overlay should contain references only, not example secret payloads:\n%s", combined)
	}
}

func TestProductionPilotOverlayKeepsRelaysDisabledAndPeerURLsHTTPS(t *testing.T) {
	root := filepath.Join("..", "..")
	config := readWorkspaceFile(t, root, "deployments/k8s/overlays/production-pilot/config-production-patch.yaml")
	relay := readWorkspaceFile(t, root, "deployments/k8s/overlays/production-pilot/relay-disabled-patch.yaml")
	if !strings.Contains(relay, "replicas: 0") {
		t.Fatalf("production-pilot relay patch must disable relay replicas:\n%s", relay)
	}
	if strings.Contains(config, "validator-1=http://") {
		t.Fatalf("production-pilot peer URLs must be HTTPS:\n%s", config)
	}
	if strings.Contains(config, "PQFABRIC_KMS_KEY_ID") {
		t.Fatalf("production-pilot config should use manifest signing_key_ref instead of one shared KMS key id:\n%s", config)
	}
	for _, want := range []string{
		"PQ_FABRIC_PRODUCTION_MODE: \"true\"",
		"PQ_FABRIC_CRYPTO_SUITE: \"pq\"",
		"STORAGE: \"sqlite\"",
		"PUBLIC_EXIT_ROUTING: \"false\"",
		"ANCHOR_BACKEND: \"mock\"",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("production-pilot config missing %q:\n%s", want, config)
		}
	}
}

func TestAWSStagingOverlayContainsExternalSecretsPVCsAndOpsProbes(t *testing.T) {
	root := filepath.Join("..", "..")
	overlay := readWorkspaceFile(t, root, "deployments/k8s/overlays/aws-staging/kustomization.yaml")
	patch := readWorkspaceFile(t, root, "deployments/k8s/overlays/aws-staging/validator-aws-staging-patch.yaml")
	config := readWorkspaceFile(t, root, "deployments/k8s/overlays/aws-staging/config-aws-staging-patch.yaml")
	secrets := readWorkspaceFile(t, root, "deployments/k8s/overlays/aws-staging/external-secrets-aws.yaml")
	relay := readWorkspaceFile(t, root, "deployments/k8s/overlays/aws-staging/relay-disabled-patch.yaml")
	combined := overlay + "\n" + patch + "\n" + config + "\n" + secrets + "\n" + relay
	for _, want := range []string{
		"external-secrets-aws.yaml",
		"ghcr.io/keithwegner/pq-fabric",
		"digest: sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"PQ_FABRIC_PRODUCTION_MODE: \"true\"",
		"PQ_FABRIC_CRYPTO_SUITE: \"pq\"",
		"PQFABRIC_SIGNER_PROVIDER: \"cloud-kms\"",
		"PQFABRIC_API_KEYS_FILE: \"/etc/pq-fabric/secrets/api-keys.json\"",
		"STORAGE: \"sqlite\"",
		"PUBLIC_EXIT_ROUTING: \"false\"",
		"POLYGON_MAINNET_ENABLED: \"false\"",
		"replicas: 7",
		"replicas: 0",
		"storageClassName: pq-fabric-staging-gp3-retain",
		"path: /readyz",
		"path: /livez",
		"containerPort: 8090",
		"secretName: pq-fabric-api-keys",
		"secretName: pq-fabric-consortium-manifest",
		"secretName: pq-fabric-peer-tls",
		"secretName: pq-fabric-kms-ca",
		"kind: ExternalSecret",
		"pq-fabric-staging-aws-secret-store",
		"pq-fabric/staging/api",
		"pq-fabric/staging/manifest",
		"pq-fabric/staging/peer-tls",
		"pq-fabric/staging/kms",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("aws-staging overlay missing %q:\n%s", want, combined)
		}
	}
	for _, rejected := range []string{
		"stringData:",
		"replace-with",
		"BEGIN PRIVATE KEY",
		"PQFABRIC_SIGNER_PROVIDER: \"local\"",
		"PUBLIC_EXIT_ROUTING: \"true\"",
		"POLYGON_MAINNET_ENABLED: \"true\"",
	} {
		if strings.Contains(combined, rejected) {
			t.Fatalf("aws-staging overlay contains rejected %q:\n%s", rejected, combined)
		}
	}
}

func TestAWSStagingDeployWorkflowIsManualGatedAndVerifiesArtifacts(t *testing.T) {
	root := filepath.Join("..", "..")
	workflow := readWorkspaceFile(t, root, ".github/workflows/aws-staging-deploy.yml")
	for _, want := range []string{
		"workflow_dispatch:",
		"environment: staging-aws",
		"id-token: write",
		"packages: read",
		"aws-actions/configure-aws-credentials@v6.1.2",
		"sigstore/cosign-installer@v4.1.2",
		"cosign verify",
		"ghcr\\.io/keithwegner/pq-fabric@sha256",
		"aws eks update-kubeconfig",
		"kubectl get crd externalsecrets.external-secrets.io clustersecretstores.external-secrets.io",
		"kubectl apply --server-side --dry-run=server",
		"rollout status statefulset/validator",
		"port-forward statefulset/validator",
		"aws-staging-render.yaml",
		"aws-staging-cosign-verify.txt",
		"aws-staging-deploy-summary.json",
		"aws-staging-smoke.json",
		"aws-staging-ops-report.json",
		"PQFABRIC_STAGING_API_TOKEN",
		"PQFABRIC_STAGING_ADMIN_TOKEN",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("aws-staging workflow missing %q:\n%s", want, workflow)
		}
	}
	for _, rejected := range []string{
		"pull_request:",
		"branches:",
		"AWS_SECRET_ACCESS_KEY",
		"terraform apply",
	} {
		if strings.Contains(workflow, rejected) {
			t.Fatalf("aws-staging workflow contains rejected %q:\n%s", rejected, workflow)
		}
	}
}

func readWorkspaceFile(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
