#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p tmp

echo "pilot-deploy-check: validating local profile"
go run ./cmd/pilot-deploy-check \
  --profile local \
  --env-file config/examples/local-dev.example.env \
  --format json \
  --out tmp/pilot-deploy-check-local.json

echo "pilot-deploy-check: validating production-pilot profile contract"
go run ./cmd/pilot-deploy-check \
  --profile production-pilot \
  --env-file config/examples/production-pilot.example.env \
  --allow-placeholders \
  --format json \
  --out tmp/pilot-deploy-check-production-pilot.json

echo "pilot-deploy-check: checking Kubernetes overlays"
for overlay in deployments/k8s/overlays/staging deployments/k8s/overlays/production-pilot; do
  test -s "${overlay}/kustomization.yaml"
  rg -q "validator-production-patch.yaml" "${overlay}/kustomization.yaml"
  rg -q "secretKeyRef|secretName" deployments/k8s/overlays/production-pilot/validator-production-patch.yaml
  rg -q "PQFABRIC_OPS_LISTEN_ADDR" "$overlay" deployments/k8s/overlays/production-pilot
  rg -q "readOnlyRootFilesystem: true" deployments/k8s/overlays/production-pilot/validator-production-patch.yaml
  rg -q "runAsNonRoot: true" deployments/k8s/overlays/production-pilot/validator-production-patch.yaml
done

if command -v kubectl >/dev/null 2>&1; then
  kubectl kustomize deployments/k8s/overlays/staging >/tmp/pq-fabric-k8s-staging.yaml
  kubectl kustomize deployments/k8s/overlays/production-pilot >/tmp/pq-fabric-k8s-production-pilot.yaml
  rg -q "secretKeyRef|secretName" /tmp/pq-fabric-k8s-production-pilot.yaml
  rg -q "containerPort: 8090" /tmp/pq-fabric-k8s-production-pilot.yaml
else
  echo "pilot-deploy-check: kubectl not installed; overlay source checks completed"
fi

echo "pilot-deploy-check: checking Terraform scaffold"
if command -v terraform >/dev/null 2>&1; then
  terraform -chdir=deployments/terraform init -backend=false >/tmp/pq-fabric-terraform-init.log
  terraform -chdir=deployments/terraform validate >/tmp/pq-fabric-terraform-validate.log
  rm -rf deployments/terraform/.terraform
else
  echo "pilot-deploy-check: terraform not installed; Terraform validation skipped"
fi

echo "pilot-deploy-check: running SQLite restore check"
go run ./cmd/sqlite-restore-check >tmp/sqlite-restore-check.json
rg -q '"status": "pass"' tmp/sqlite-restore-check.json

echo "pilot-deploy-check: running SQLite migration and backup check"
rm -rf tmp/pilot-backup-check
mkdir -p tmp/pilot-backup-check
go run ./cmd/pqfabric migrate-sqlite \
  --database-url tmp/pilot-backup-check/source.db \
  --apply \
  --format json \
  --out tmp/pilot-backup-migration.json
go run ./cmd/pqfabric migrate-sqlite \
  --database-url tmp/pilot-backup-check/source.db \
  --format json \
  --out tmp/pilot-backup-migration-dry-run.json
go run ./cmd/pqfabric backup \
  --database-url tmp/pilot-backup-check/source.db \
  --backup-db tmp/pilot-backup-check/backup.db \
  --force \
  --format json \
  --out tmp/pilot-backup-report.json
go run ./cmd/pqfabric restore-check \
  --database-url tmp/pilot-backup-check/backup.db \
  --format json \
  --out tmp/pilot-backup-restore-report.json
rg -q '"status": "pass"' tmp/pilot-backup-migration.json
rg -q '"status": "pass"' tmp/pilot-backup-report.json
rg -q '"status": "pass"' tmp/pilot-backup-restore-report.json

echo "pilot-deploy-check: running pilot bootstrap validation and smoke"
go run ./cmd/pilot-bootstrap validate \
  --spec config/examples/pilot-bootstrap.example.yaml \
  --format json \
  --out tmp/pilot-bootstrap-validate.json
rg -q '"status": "pass_with_unresolved"' tmp/pilot-bootstrap-validate.json
rg -q '"secret_evidence"' tmp/pilot-bootstrap-validate.json
rg -q '"secret_store_refs"' tmp/pilot-bootstrap-validate.json
rg -q '"raw_values_redacted": true' tmp/pilot-bootstrap-validate.json
go run ./cmd/pilot-bootstrap smoke \
  --spec config/examples/pilot-bootstrap.example.yaml \
  --format json \
  --out tmp/pilot-bootstrap-smoke.json
rg -q '"smoke_verify_receipt"' tmp/pilot-bootstrap-smoke.json
rg -q '"smoke_restore_verify"' tmp/pilot-bootstrap-smoke.json

echo "pilot-deploy-check: collecting release provenance"
./scripts/release-provenance.sh >/tmp/pq-fabric-release-provenance.log
rg -q '"schema_version": "pq-fabric.release-provenance.v1"' tmp/release-provenance.json
rg -q '"status": "pass|pass_with_skips"' tmp/release-provenance.json
rg -q '"go_module_inventory"' tmp/release-provenance.json
rg -q '"image_reference"' tmp/release-provenance.json
rg -q '"sbom_status"' tmp/release-provenance.json
rg -q '"cosign_status"' tmp/release-provenance.json

echo "pilot-deploy-check: pass"
