#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p tmp

require_file() {
  local path="$1"
  if [[ ! -s "$path" ]]; then
    echo "aws-staging-check: missing required file: $path" >&2
    exit 1
  fi
}

require_pattern() {
  local pattern="$1"
  local path="$2"
  if ! rg -q "$pattern" "$path"; then
    echo "aws-staging-check: missing pattern '$pattern' in $path" >&2
    exit 1
  fi
}

reject_pattern() {
  local pattern="$1"
  local path="$2"
  if rg -q "$pattern" "$path"; then
    echo "aws-staging-check: rejected pattern '$pattern' in $path" >&2
    exit 1
  fi
}

overlay="deployments/k8s/overlays/aws-staging"
workflow=".github/workflows/aws-staging-deploy.yml"

require_file "$workflow"
require_file "${overlay}/kustomization.yaml"
require_file "${overlay}/config-aws-staging-patch.yaml"
require_file "${overlay}/validator-aws-staging-patch.yaml"
require_file "${overlay}/networkpolicy-aws-staging-patch.yaml"
require_file "${overlay}/relay-disabled-patch.yaml"
require_file "${overlay}/external-secrets-aws.yaml"
require_file "deployments/secrets/aws-staging-external-secret-contract.example.yaml"
require_file "config/examples/aws-staging.example.env"
require_file "docs/deployment-aws-staging.md"

require_pattern 'workflow_dispatch:' "$workflow"
reject_pattern 'pull_request:' "$workflow"
reject_pattern 'branches:' "$workflow"
require_pattern 'environment: staging-aws' "$workflow"
require_pattern 'id-token: write' "$workflow"
require_pattern 'packages: read' "$workflow"
require_pattern 'aws-actions/configure-aws-credentials@v6\.1\.2' "$workflow"
require_pattern 'sigstore/cosign-installer@v4\.1\.2' "$workflow"
require_pattern 'cosign verify' "$workflow"
require_pattern 'ghcr\.io/keithwegner/pq-fabric@sha256' "$workflow"
require_pattern 'aws eks update-kubeconfig' "$workflow"
require_pattern 'kubectl get crd externalsecrets\.external-secrets\.io clustersecretstores\.external-secrets\.io' "$workflow"
require_pattern 'kubectl apply --server-side --dry-run=server' "$workflow"
require_pattern 'rollout status statefulset/validator' "$workflow"
require_pattern 'port-forward statefulset/validator' "$workflow"
require_pattern 'go run ./cmd/pqfabric submit' "$workflow"
require_pattern 'go run ./cmd/pqfabric verify' "$workflow"
require_pattern 'go run ./cmd/pqfabric report' "$workflow"
require_pattern 'PQFABRIC_STAGING_API_TOKEN' "$workflow"
require_pattern 'PQFABRIC_STAGING_ADMIN_TOKEN' "$workflow"
require_pattern 'AWS_ROLE_ARN' "$workflow"
require_pattern 'AWS_REGION' "$workflow"
require_pattern 'EKS_CLUSTER_NAME' "$workflow"

require_pattern 'digest: sha256:[0-9a-f]{64}' "${overlay}/kustomization.yaml"
require_pattern 'ghcr\.io/keithwegner/pq-fabric' "${overlay}/kustomization.yaml"
require_pattern 'external-secrets-aws\.yaml' "${overlay}/kustomization.yaml"
require_pattern 'PQ_FABRIC_PRODUCTION_MODE: "true"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'PQ_FABRIC_CRYPTO_SUITE: "pq"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'PQFABRIC_SIGNER_PROVIDER: "cloud-kms"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'PQFABRIC_ALLOW_LOCAL_SIGNER: "false"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'PQFABRIC_API_KEYS_FILE: "/etc/pq-fabric/secrets/api-keys.json"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'STORAGE: "sqlite"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'PUBLIC_EXIT_ROUTING: "false"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'POLYGON_MAINNET_ENABLED: "false"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'ANCHOR_BACKEND: "mock"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'PQFABRIC_OPS_LISTEN_ADDR: ":8090"' "${overlay}/config-aws-staging-patch.yaml"
require_pattern 'replicas: 7' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'readOnlyRootFilesystem: true' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'runAsNonRoot: true' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'path: /readyz' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'path: /livez' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'containerPort: 8090' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'storageClassName: pq-fabric-staging-gp3-retain' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'secretName: pq-fabric-api-keys' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'secretName: pq-fabric-consortium-manifest' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'secretName: pq-fabric-peer-tls' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'secretName: pq-fabric-kms-ca' "${overlay}/validator-aws-staging-patch.yaml"
require_pattern 'replicas: 0' "${overlay}/relay-disabled-patch.yaml"
require_pattern 'kind: ExternalSecret' "${overlay}/external-secrets-aws.yaml"
require_pattern 'pq-fabric-staging-aws-secret-store' "${overlay}/external-secrets-aws.yaml"
require_pattern 'pq-fabric/staging/api' "${overlay}/external-secrets-aws.yaml"
require_pattern 'pq-fabric/staging/manifest' "${overlay}/external-secrets-aws.yaml"
require_pattern 'pq-fabric/staging/peer-tls' "${overlay}/external-secrets-aws.yaml"
require_pattern 'pq-fabric/staging/kms' "${overlay}/external-secrets-aws.yaml"

reject_pattern 'stringData:' "$overlay"
reject_pattern 'replace-with|BEGIN PRIVATE KEY|AKIA[0-9A-Z]{16}|aws_secret_access_key|kubeconfig|certificate-authority-data' "$overlay"
reject_pattern 'PUBLIC_EXIT_ROUTING: "true"|POLYGON_MAINNET_ENABLED: "true"|PQFABRIC_SIGNER_PROVIDER: "local"' "$overlay"
reject_pattern 'arn:aws:iam::[0-9]{12}:' config/examples/aws-staging.example.env

go run ./cmd/pilot-deploy-check \
  --profile staging \
  --env-file config/examples/aws-staging.example.env \
  --allow-placeholders \
  --format json \
  --out tmp/aws-staging-profile-check.json
rg -q '"status": "pass"' tmp/aws-staging-profile-check.json

if command -v kubectl >/dev/null 2>&1; then
  kubectl kustomize "$overlay" > tmp/aws-staging-render.yaml
  rg -q 'kind: ExternalSecret' tmp/aws-staging-render.yaml
  rg -q 'pq-fabric-staging-aws-secret-store' tmp/aws-staging-render.yaml
  rg -q 'image: ghcr.io/keithwegner/pq-fabric@sha256:' tmp/aws-staging-render.yaml
  rg -q 'containerPort: 8090' tmp/aws-staging-render.yaml
  rg -q 'storageClassName: pq-fabric-staging-gp3-retain' tmp/aws-staging-render.yaml
else
  echo "aws-staging-check: kubectl not installed; overlay source checks completed"
fi

echo "aws-staging-check: pass"
