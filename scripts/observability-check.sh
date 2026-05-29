#!/usr/bin/env bash
set -euo pipefail

alerts="deployments/observability/prometheus-alerts.yaml"
docs=(
  "docs/operations-runbook.md"
  "docs/production-evidence-fabric.md"
  "docs/implementation-status.md"
)

test -s "$alerts"
for name in \
  PQFabricReadinessFailure \
  PQFabricLostQuorum \
  PQFabricValidatorDrift \
  PQFabricPeerDown \
  PQFabricSignerFailure \
  PQFabricStorageError \
  PQFabricRepeatedUnauthorizedAccess \
  PQFabricInvalidSignatures \
  PQFabricHighDuplicateSubmissions \
  PQFabricAnchorUnavailable; do
  rg -q "alert: ${name}" "$alerts"
done

for doc in "${docs[@]}"; do
  test -s "$doc"
done

rg -q "pqfabric report" docs/operations-runbook.md docs/production-evidence-fabric.md
rg -q "OpenTelemetry" docs/implementation-status.md docs/production-evidence-fabric.md
rg -q "prometheus-alerts.yaml" docs/operations-runbook.md

echo "observability-check: pass"
