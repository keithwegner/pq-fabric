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

func readWorkspaceFile(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
