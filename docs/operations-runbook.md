# Operations Runbook

This runbook covers local prototype operations only. It is intended for repeatable demos, evidence generation, and handoff. It is not production operations guidance.

## Startup Sequence

1. Validate the repo:

   ```bash
   make verify
   ```

2. Build the local image:

   ```bash
   make image
   ```

3. Validate Compose configuration:

   ```bash
   make compose-config
   ```

4. Start the local testbed:

   ```bash
   make compose-up
   ```

5. Check validator state:

   ```bash
   curl -s http://127.0.0.1:8081/state
   ```

6. Check pilot API readiness and metrics:

   ```bash
   curl -s http://127.0.0.1:8081/ready
   curl -s http://127.0.0.1:8081/metrics
   ```

   `/ready` is stricter than `/health`: it reports storage, signer/KMS,
   manifest membership, API auth, peer mTLS, peer reachability, and quorum
   availability checks.

## Shutdown Sequence

```bash
make compose-down
```

Clean generated local validator state when a fresh run is needed:

```bash
make compose-clean
```

## Validator Restart

For Compose, restart one validator:

```bash
docker compose restart validator-3
```

The validator reloads durable state from `./data/validator-3`. This is local prototype durability only.

## Relay Restart

Relays are local private-testbed services:

```bash
docker compose --profile relays restart relay-4
```

Relays do not provide public discovery or public exit routing.

## Evidence Workflow

```bash
make failure-evidence
make routing-evidence
make bundle-evidence
make anchor-evidence
make deployment-evidence
make pilot-bootstrap-check
make pilot-backup-check
make pilot-deploy-check
```

Artifacts are written under `tmp/`. They are generated evidence and are ignored by Git.

## Evidence Fabric API

Submit hash-only pilot evidence through the external v1 API or CLI:

```bash
go run ./cmd/pqfabric submit \
  --url http://127.0.0.1:8081 \
  --category incident-report \
  --artifact-hash sha256:<artifact-digest> \
  --metadata-hash sha256:<metadata-digest> \
  --organization bestgate \
  --idempotency-key incident-123
```

Then verify or export the receipt:

```bash
go run ./cmd/pqfabric verify --url http://127.0.0.1:8081 --receipt-id <receipt-id>
go run ./cmd/pqfabric export-receipt --url http://127.0.0.1:8081 --receipt-id <receipt-id> --out receipt.json
```

When scoped API keys are configured on validators, pass the raw client token
with `--token` or set `PQFABRIC_API_TOKEN`. Generate server-side token hashes
with:

```bash
go run ./cmd/pqfabric auth hash-token --token <operator-generated-token>
```

Use `GET /v1/audit/recent` with an `admin:read` key to inspect recent API
requests.

Generate a bounded operator incident report with:

```bash
go run ./cmd/pqfabric report \
  --url http://127.0.0.1:8081 \
  --token <admin-token> \
  --format text \
  --audit-limit 50 \
  --receipt-limit 20
```

The report includes node state, readiness checks, peer health, recent commits,
recent receipts, recent audit records, quorum participation, signer status, and
receipt verification spot checks. It does not include raw API tokens, private
keys, evidence payloads, or full metadata.

## Observability

Validators support `PQFABRIC_LOG_FORMAT=text|json`; production mode defaults to
JSON logs. Optional OpenTelemetry tracing is enabled with
`PQFABRIC_OTEL_ENABLED=true` and requires
`PQFABRIC_OTEL_EXPORTER_OTLP_ENDPOINT=<otlp-http-url>`. Optional collector
headers can be provided with `PQFABRIC_OTEL_EXPORTER_OTLP_HEADERS=key=value`.

Prometheus alert templates live at
`deployments/observability/prometheus-alerts.yaml`. Validate that the local
template and docs are present with:

```bash
make observability-check
```

## Controlled Deployment Readiness

Run the provider-neutral deployment readiness check with:

```bash
make pilot-deploy-check
```

The target validates the local and production-pilot profile contracts, checks
the staging and production-pilot Kubernetes overlays, validates Terraform when
the tool is installed, validates the provider-neutral secret contract including
External Secrets `secretStoreRef` references, runs a temporary seven-validator
production-mode bootstrap smoke, performs a local SQLite migration dry-run plus
backup/restore verification, performs a receipt restore spot check, and writes
release provenance evidence. It does not apply Terraform, apply Kubernetes
manifests, fetch or upload real secrets, sign images, publish images, or deploy
cloud resources.

Generated readiness artifacts include:

```text
tmp/pilot-deploy-check-local.json
tmp/pilot-deploy-check-production-pilot.json
tmp/pilot-bootstrap-validate.json
tmp/pilot-bootstrap-smoke.json
tmp/pilot-backup-migration.json
tmp/pilot-backup-migration-dry-run.json
tmp/pilot-backup-report.json
tmp/pilot-backup-restore-report.json
tmp/sqlite-restore-check.json
tmp/release-provenance.json
tmp/release-provenance.txt
tmp/go-modules.txt
tmp/sbom.spdx.json # optional, only when syft is installed
```

For Kubernetes, the production-pilot overlay uses `PQFABRIC_OPS_LISTEN_ADDR`
for `/livez` and `/readyz` probes. Internal `/ready`, `/metrics`, consensus,
and peer endpoints remain on the peer-mTLS validator surface.

## SQLite Backup And Upgrade Checks

Dry-run migrations before a rollout:

```bash
go run ./cmd/pqfabric migrate-sqlite --database-url <validator.db>
```

Apply known migrations only after review:

```bash
go run ./cmd/pqfabric migrate-sqlite --database-url <validator.db> --apply
```

Create and verify a local SQLite backup:

```bash
go run ./cmd/pqfabric backup \
  --database-url <validator.db> \
  --backup-db <validator.backup.db> \
  --manifest <current-manifest.json> \
  --history <history-v1.json>,<current-manifest.json>

go run ./cmd/pqfabric restore-check \
  --database-url <validator.backup.db> \
  --manifest <current-manifest.json> \
  --history <history-v1.json>,<current-manifest.json>
```

The backup report includes schema version, SQLite integrity check, source and
backup checksums, record counts, and recent receipt verification results. This
is a local operator gate; managed backups, offsite retention, and provider
snapshot policies still need separate implementation.

## Data Directories

Validator data lives under `data/validator-*` in local Compose. Do not copy or publish data directories as a production backup. For handoff, prefer documentation and selected evidence artifacts instead of raw state.

## Secret Handling

- Use `.env` or local config files for local-only settings.
- Do not commit `.env`, `.tfvars`, kubeconfigs, wallet files, private keys, API keys, cloud credentials, RPC URLs with secrets, Terraform state, or Foundry broadcast output.
- Use a secret manager for any future real deployment.
- The provider-neutral Kubernetes secret contract is documented under
  `deployments/secrets/`; it contains names and keys only, not real values.
- `config/examples/pilot-bootstrap.example.yaml` and
  `deployments/secrets/external-secret-contract.example.yaml` validate the
  secret reference shape and expected External Secrets store ref before a pilot
  rollout. `make pilot-bootstrap-check` also proves a generated local smoke path
  with temporary material only.
- `pilot-bootstrap validate` reports redacted `secret_evidence` for expected
  mount paths, resolved/unresolved state, content class, and safe fingerprints
  only for manifests and certificate material. Tokens and private keys are not
  printed or fingerprinted.
- `scripts/release-provenance.sh` records Go/tool versions, module inventory,
  image reference/digest status, SBOM status, cosign verification evidence, and
  whether the run is local or published. Published release mode is produced by
  the `release-artifacts` workflow and requires a clean git tree, digest-pinned
  GHCR image, SBOM, and successful keyless cosign verification. Local mode may
  report explicit skips when Docker, syft, cosign, or a registry digest is
  unavailable.
- The default anchor backend is `mock`.
- Production guardrails require `PQ_FABRIC_PRODUCTION_MODE=1`,
  `PQ_FABRIC_CRYPTO_SUITE=pq`, `PQFABRIC_API_KEYS_FILE=<path>`,
  `PQFABRIC_CONSORTIUM_MANIFEST=<path>`,
  `PQFABRIC_CONSORTIUM_MANIFEST_HISTORY=<paths>`,
  `PQFABRIC_SIGNER_PROVIDER=cloud-kms`, KMS endpoint plus manifest
  `signing_key_ref` key mapping,
  `STORAGE=sqlite`, `PQFABRIC_DATABASE_URL=<sqlite-dsn>`, and peer mTLS
  cert/key/CA files.
- OTel collector headers may contain secrets and must be supplied through a
  secret manager or ignored local file, never committed.
- Peer certificates must chain to the configured CA and carry URI SAN
  `spiffe://<consortium_id>/validator/<validator_id>`.
- Local signing in production mode is rejected unless
  `PQFABRIC_ALLOW_LOCAL_SIGNER=1` is set for a controlled exception.

## Known Limitations

- Consensus remains a local first-principles prototype.
- The failure harness uses logical ticks and in-process control.
- Routing remains private-testbed only.
- Bundle and AI context handling remains local/mock only.
- Polygon anchoring is optional and mock-backed for local validation.
- Kubernetes and Terraform files are controlled-readiness scaffolding, not a
  live cluster or cloud deployment.

No part of this runbook claims production BFT safety, production fault tolerance, production anonymity, production post-quantum security, FIPS certification, ACVTS validation, audited smart-contract security, or production deployment readiness.
