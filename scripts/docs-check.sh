#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

required_docs=(
  README.md
  docs/implementation-status.md
  docs/final-handoff.md
  docs/evidence-index.md
  docs/final-architecture-summary.md
  docs/claim-safety-review.md
  docs/codex-notes.md
  docs/ci-cd.md
  docs/crypto-validation.md
  docs/failure-evidence.md
  docs/routing-testbed.md
  docs/bundle-protocol.md
  docs/ai-context-channels.md
  docs/identity-anchors.md
  docs/contracts-polygon.md
  docs/release-artifacts.md
  docs/deployment-local.md
  docs/deployment-k8s.md
  docs/deployment-terraform.md
  docs/operations-runbook.md
  docs/release-notes.md
  deployments/secrets/README.md
  deployments/secrets/external-secret-contract.example.yaml
  config/examples/production-pilot.example.env
  config/examples/pilot-bootstrap.example.yaml
)

for doc in "${required_docs[@]}"; do
  if [[ ! -s "$doc" ]]; then
    echo "missing required doc: $doc" >&2
    exit 1
  fi
done

echo "docs-check: required final docs present"
