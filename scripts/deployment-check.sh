#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

required_paths=(
  Dockerfile
  docker-compose.yml
  config/local/validators.yaml
  config/local/relays.yaml
  config/local/routing.yaml
  config/local/anchors.yaml
  config/local/bundle.yaml
  deployments/k8s/base/kustomization.yaml
  deployments/k8s/overlays/staging/kustomization.yaml
  deployments/k8s/overlays/production-pilot/kustomization.yaml
  deployments/k8s/overlays/aws-staging/kustomization.yaml
  deployments/secrets/README.md
  deployments/secrets/external-secret-contract.example.yaml
  deployments/secrets/aws-staging-external-secret-contract.example.yaml
  config/examples/production-pilot.example.env
  config/examples/aws-staging.example.env
  config/examples/pilot-bootstrap.example.yaml
  deployments/terraform/main.tf
  docs/deployment-local.md
  docs/deployment-k8s.md
  docs/deployment-aws-staging.md
  docs/deployment-terraform.md
  docs/operations-runbook.md
)

for path in "${required_paths[@]}"; do
  if [[ ! -e "$path" ]]; then
    echo "missing required deployment path: $path" >&2
    exit 1
  fi
done

echo "deployment path files present"

if command -v docker >/dev/null 2>&1; then
  docker --version
else
  echo "docker not installed; image and compose commands will be skipped"
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  docker compose version
else
  echo "docker compose not installed; compose validation will be skipped"
fi

if command -v kubectl >/dev/null 2>&1; then
  kubectl version --client
else
  echo "kubectl not installed; k8s validation will be skipped"
fi

if command -v terraform >/dev/null 2>&1; then
  terraform version
else
  echo "terraform not installed; terraform validation will be skipped"
fi

echo "deployment check complete: controlled deployment readiness scaffold, no cloud resources created"
