#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

require_file() {
  local path="$1"
  if [[ ! -s "$path" ]]; then
    echo "release-artifacts-check: missing required file: $path" >&2
    exit 1
  fi
}

require_pattern() {
  local pattern="$1"
  local path="$2"
  if ! rg -q "$pattern" "$path"; then
    echo "release-artifacts-check: missing pattern '$pattern' in $path" >&2
    exit 1
  fi
}

require_file .github/workflows/release-artifacts.yml
require_file .github/workflows/production-readiness.yml
require_file Dockerfile
require_file scripts/release-provenance.sh
require_file docs/ci-cd.md
require_file docs/release-artifacts.md
require_file docs/deployment-k8s.md
require_file docs/deployment-terraform.md

require_pattern '^FROM --platform=\$BUILDPLATFORM golang:1\.25-bookworm AS build$' Dockerfile
require_pattern '^ARG TARGETOS=linux$' Dockerfile
require_pattern '^ARG TARGETARCH$' Dockerfile

release_workflow=.github/workflows/release-artifacts.yml
require_pattern 'ghcr\.io' "$release_workflow"
require_pattern 'keithwegner/pq-fabric' "$release_workflow"
require_pattern 'docker/setup-qemu-action@v4\.1\.0' "$release_workflow"
require_pattern 'docker/setup-buildx-action@v4\.1\.0' "$release_workflow"
require_pattern 'docker/login-action@v4\.2\.0' "$release_workflow"
require_pattern 'docker/build-push-action@v7\.2\.0' "$release_workflow"
require_pattern 'sigstore/cosign-installer@v4\.1\.2' "$release_workflow"
require_pattern 'actions/attest-build-provenance@v4\.1\.0' "$release_workflow"
require_pattern 'aquasecurity/trivy-action@v0\.36\.0' "$release_workflow"
require_pattern 'actions/upload-artifact@v7' "$release_workflow"
require_pattern 'packages: write' "$release_workflow"
require_pattern 'id-token: write' "$release_workflow"
require_pattern 'attestations: write' "$release_workflow"
require_pattern 'SYFT_VERSION: v1\.44\.0' "$release_workflow"
require_pattern 'cosign sign' "$release_workflow"
require_pattern 'cosign verify' "$release_workflow"
require_pattern 'tmp/sbom\.spdx\.json' "$release_workflow"
require_pattern 'tmp/image-digest\.txt' "$release_workflow"
require_pattern 'tmp/cosign-verify\.txt' "$release_workflow"
require_pattern 'v\*' "$release_workflow"

require_pattern 'PQFABRIC_RELEASE_MODE' scripts/release-provenance.sh
require_pattern 'published' scripts/release-provenance.sh
require_pattern 'image_digest_file' scripts/release-provenance.sh
require_pattern 'cosign_verify_file' scripts/release-provenance.sh
require_pattern 'published_requirements' scripts/release-provenance.sh

require_pattern 'release-artifacts' .github/workflows/production-readiness.yml
require_pattern 'ghcr\.io/keithwegner/pq-fabric' deployments/k8s/overlays/staging/kustomization.yaml
require_pattern 'sha256:' deployments/k8s/overlays/staging/kustomization.yaml
require_pattern 'ghcr\.io/keithwegner/pq-fabric' deployments/k8s/overlays/production-pilot/kustomization.yaml
require_pattern 'sha256:' deployments/k8s/overlays/production-pilot/kustomization.yaml
require_pattern 'ghcr\.io/keithwegner/pq-fabric' deployments/k8s/overlays/aws-staging/kustomization.yaml
require_pattern 'sha256:' deployments/k8s/overlays/aws-staging/kustomization.yaml
require_pattern 'ghcr\.io/keithwegner/pq-fabric@sha256:' deployments/terraform/examples/aws-three-region.tfvars.example
require_pattern 'release-artifacts / build, scan, sign, and attest' docs/ci-cd.md
require_pattern 'digest-pinned' docs/release-artifacts.md

echo "release-artifacts-check: pass"
