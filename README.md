# pq-fabric

Runnable local Go prototype for a post-quantum Byzantine infrastructure platform.

This repo is organized as one platform with three separable subsystems:

1. **Consensus and identity layer** — seven local validators, deterministic proposer rotation, explicit round/stage votes, 5-of-7 quorum certificates, validator lock state, deterministic catch-up after node recovery, and a controlled local fault-evidence harness.
2. **Byzantine Bundle Protocol scaffolding** — CCSDS-inspired virtual channels, priority-weighted AI context scheduling, canonical bundle envelopes, custody confirmation through 5-of-7 validator quorum evidence, idempotent retransmission, deterministic reconciliation, and a local mock OpenAI-compatible interface shape.
3. **Private-testbed routing scaffold** — seven local relays, three-hop telescoping circuit construction, per-hop KEM handshakes, layered encryption cells, restricted SOCKS5 CONNECT handling, stream multiplexing, and local-only exit policy evidence.
4. **Polygon-compatible anchoring scaffold** — self-contained Foundry contracts and a Go anchor interface for identity, credential, governance, and quorum-certificate hash anchors. Validators still verify PQ signatures and quorum certificates off-chain.

## Important prototype limitation

This prototype is not a FIPS-certified, ACVTS-certified, or production-ready post-quantum-secure implementation.

The crypto layer is intentionally abstracted. The default runnable local demo still uses deterministic development adapters so tests and the seven-validator demo remain stable:

- `DEV-ED25519-SLOT-FOR-ML-DSA-87` for signatures.
- `DEV-X25519-SLOT-FOR-ML-KEM-768` for circuit key establishment.

Those development adapters are not post-quantum secure. Phase 1 adds a selectable `pq` suite with implementation-backed ML-DSA-87 and ML-KEM-768 adapters for engineering validation, while preserving the `dev` suite for deterministic local tests.

Crypto suite selection:

```bash
PQ_FABRIC_CRYPTO_SUITE=dev go run ./cmd/demo
PQ_FABRIC_CRYPTO_SUITE=pq go test ./core/crypto/...
```

The default is `dev` when `PQ_FABRIC_CRYPTO_SUITE` is unset. Passing local tests with the `pq` suite is not a certification claim.

## Final local quickstart

From a fresh checkout or extracted handoff bundle:

```bash
make final-verify
make e2e-demo
make e2e-evidence
make package-handoff
make package-evidence
```

Generated integrated evidence:

```text
tmp/e2e-evidence.json
tmp/e2e-evidence.txt
```

Generated handoff packages:

```text
dist/pq-fabric-handoff.tar.gz
dist/pq-fabric-evidence.tar.gz
```

Optional tools may be unavailable locally. `forge` is required only for Solidity test execution, Docker daemon access is required only for image builds, and `kubectl`/Terraform are used only for scaffold validation. Missing optional tools should produce clear skip messages.

This quickstart does not deploy anything, does not call external AI APIs, does not contact Polygon, and does not enable public routing.

## Evidence fabric API and CLI

The production-facing pilot surface is the hash-only evidence fabric API. It is
separate from the internal consensus endpoints and returns receipts only after a
5-of-7 quorum certificate is formed.

Start the local validator topology with Compose or run validators manually, then
submit through the CLI:

```bash
go run ./cmd/pqfabric submit \
  --url http://127.0.0.1:8081 \
  --category incident-report \
  --artifact-hash sha256:<artifact-digest> \
  --metadata-hash sha256:<metadata-digest> \
  --organization bestgate \
  --idempotency-key incident-123
```

Verify and export receipts:

```bash
go run ./cmd/pqfabric verify --url http://127.0.0.1:8081 --receipt-id <receipt-id>
go run ./cmd/pqfabric export-receipt --url http://127.0.0.1:8081 --receipt-id <receipt-id> --out receipt.json
go run ./cmd/pqfabric report --url http://127.0.0.1:8081 --format text
```

Production guardrails can be enabled with `PQ_FABRIC_PRODUCTION_MODE=1`,
`PQ_FABRIC_CRYPTO_SUITE=pq`, `PQFABRIC_API_KEYS_FILE=<path>`,
`PQFABRIC_CONSORTIUM_MANIFEST=<path>`,
`PQFABRIC_CONSORTIUM_MANIFEST_HISTORY=<paths>`, `STORAGE=sqlite`,
`PQFABRIC_DATABASE_URL=<sqlite-dsn>`, `PQFABRIC_SIGNER_PROVIDER=cloud-kms`,
cloud KMS endpoint/key config, and peer mTLS cert/key/CA files.
Production mode rejects the development crypto suite, non-SQLite storage,
missing manifest history, missing peer mTLS config, non-HTTPS peer URLs, and
local signing unless `PQFABRIC_ALLOW_LOCAL_SIGNER=1` is explicitly set for a
controlled local/pilot exception. The older `API_BEARER_TOKEN` path remains
available only as a local-development fallback.

Operator observability is available through structured logs, internal
Prometheus text metrics, stronger `/ready` checks, optional OTLP/HTTP tracing
with `PQFABRIC_OTEL_ENABLED=true`, and the admin-scoped `GET /v1/ops/report`
or `pqfabric report` command. Alert templates live under
`deployments/observability/prometheus-alerts.yaml`.

Generate a token hash for an API-key file without storing the raw token:

```bash
go run ./cmd/pqfabric auth hash-token --token <operator-generated-token>
```

Generate a validator membership manifest:

```bash
go run ./cmd/pqfabric manifest generate --suite pq --out config/local/consortium.manifest.json
go run ./cmd/pqfabric manifest verify --manifest config/local/consortium.manifest.json --history config/local/consortium.manifest.json
```

For cloud KMS custody, set each validator's manifest `signing_key_ref` or pass
`PQFABRIC_KMS_KEY_ID`, and configure `PQFABRIC_KMS_ENDPOINT` plus optional
`PQFABRIC_KMS_TOKEN` and `PQFABRIC_KMS_CA_FILE`. The validator fetches the
remote public key at startup and refuses to sign if it does not match the
manifest.

Scoped API keys support `evidence:submit`, `evidence:read`,
`evidence:verify`, `anchor:read`, and `admin:read`. The `/v1` API also records
request audit entries, exposes `GET /v1/audit/recent` for admin keys, and
applies lightweight per-key rate limits. This is still regulated-pilot
hardening, not a FIPS, ACVTS, or full production claim.

Operator details live in:

```text
docs/production-evidence-fabric.md
docs/threat-model.md
docs/operations-runbook.md
```

## Run the local demo

```bash
make demo
```

The demo does the following:

1. Starts seven validators on localhost ports `9101` through `9107`.
2. Commits a block with all seven validators online.
3. Stops validator-6 and validator-7.
4. Commits another block with exactly 5-of-7 quorum.
5. Restarts the stopped validators.
6. Runs catch-up so all seven converge to the same height/hash.
7. Demonstrates idempotent retransmission deduplication.

Expected summary:

```text
commit 1: height=1 round=0 votes=7/7 proposer=validator-1 state=<digest>
commit 2 under failure: height=2 round=0 votes=5/7 threshold=5 proposer=validator-2 state=<digest>
demo complete: 5-of-7 quorum committed during a 2-node failure; remediated nodes caught up deterministically.
```

Consensus remains a local prototype. It now models proposal, prevote, precommit, and commit evidence, rotates proposers deterministically from height/round/validator ordering, advances rounds when a scheduled proposer is unavailable, rejects duplicate or conflicting votes, and includes a deterministic state digest in committed block metadata. This is not a production BFT safety claim or formal verification.

## Run tests

```bash
make verify
make final-verify
```

Run the focused post-quantum adapter vector harness:

```bash
make crypto-vectors
```

The vector harness is engineering validation only. It is not FIPS 140-3 certification or ACVTS validation.

## Run controlled failure evidence

```bash
make fault-demo
make failure-evidence
```

The fault demo runs a deterministic local scenario around the seven-validator prototype:

1. Starts seven validators with isolated durable state under `tmp/fault-demo-data`.
2. Commits normally.
3. Stops validator-6 and validator-7.
4. Records heartbeat-based suspected/failed evidence using logical ticks.
5. Commits application-level messages while the network has exactly five live validators.
6. Restarts the failed validators, runs catch-up, and verifies all seven converge to the same height/hash/state digest.
7. Emits message-preservation metrics showing accepted transaction IDs are committed once or deduplicated on replay.

Generated evidence artifacts:

```text
tmp/failure-evidence.json
tmp/failure-evidence.txt
```

This is local, private-testbed evidence. It is not a production fault-tolerance, production BFT safety, production self-healing, or zero-packet-loss claim.

## Run private routing testbed evidence

```bash
make routing-tests
make routing-demo
make routing-evidence
```

The routing demo is a private local testbed only. It initializes seven local relays named `relay-1` through `relay-7`, selects a three-hop path, performs per-hop KEM handshakes through the crypto suite abstraction, sends local echo and HTTP test traffic through layered onion cells, exercises a restricted SOCKS5 CONNECT flow, runs multiple logical streams over one circuit, rejects a disallowed destination, and writes evidence artifacts:

```text
tmp/routing-evidence.json
tmp/routing-evidence.txt
```

The default exit policy allows only local test destinations such as `local-echo:7000` and `local-http:8080`. There is no public relay discovery or public exit routing. This is not production anonymity, production privacy, censorship resistance, FIPS certification, ACVTS validation, or production post-quantum security.

## Run bundle protocol evidence

```bash
make bundle-tests
make bundle-demo
make bundle-evidence
```

The bundle demo creates local AI context virtual channels for conversation, working memory, execution, and retrieval. It schedules context items deterministically, builds canonical bundle envelopes, confirms custody using 5-of-7 validator quorum evidence, simulates interruption/retransmission, deduplicates duplicate transaction IDs, reconciles missing bundle state, and records a deterministic mock OpenAI-compatible request/response without calling any external AI API.

Generated evidence artifacts:

```text
tmp/bundle-evidence.json
tmp/bundle-evidence.txt
```

This is a local protocol scaffold only. It is not production AI infrastructure, production data sovereignty, production transport reliability, production BFT safety, live model integration, FIPS certification, ACVTS validation, or production anonymity.

## Run Polygon anchor evidence

```bash
make anchor-tests
make contract-tests
make anchor-demo
make anchor-evidence
```

The anchor demo uses the local mock anchor backend. It converts the seven local validator identities into anchor records, anchors one credential hash, anchors one governance proposal hash, anchors one quorum-certificate hash, checks lookup/mismatch behavior, and writes evidence artifacts:

```text
tmp/anchor-evidence.json
tmp/anchor-evidence.txt
```

Foundry contract tests live under `contracts/polygon/test`. If `forge` is not installed, `make contract-tests` prints a skip message instead of blocking Go validation. The contracts store hashes and metadata only; PQ signature validation and quorum-certificate validation remain off-chain in validators. This is not a smart-contract security audit, live Polygon deployment claim, production governance safety claim, or on-chain PQ verification claim.

## Run as Docker Compose

```bash
make image
make compose-config
make compose-up
```

Validator-1 auto-proposes every 15 seconds. Docker Compose uses durable file-backed validator state by default and mounts isolated local directories under `./data/validator-1` through `./data/validator-7`. Generated `data/` files are ignored by Git.

The Compose file also models seven private-testbed relay services behind the `relays` profile. Relays are local-only services and do not implement public relay discovery or public exit routing.

Host ports map as follows:

| Validator | URL |
|---|---|
| validator-1 | `http://127.0.0.1:8081` |
| validator-2 | `http://127.0.0.1:8082` |
| validator-3 | `http://127.0.0.1:8083` |
| validator-4 | `http://127.0.0.1:8084` |
| validator-5 | `http://127.0.0.1:8085` |
| validator-6 | `http://127.0.0.1:8086` |
| validator-7 | `http://127.0.0.1:8087` |

Manual proposal against validator-1:

```bash
./scripts/manual-propose.sh "manual governance update"
```

Inspect state:

```bash
curl -s http://127.0.0.1:8081/state | python3 -m json.tool
curl -s http://127.0.0.1:8081/peers | python3 -m json.tool
```

Stop the local testbed:

```bash
make compose-down
```

Remove generated local validator state:

```bash
make compose-clean
```

## Deployment path

The deployment path now includes controlled deployment readiness checks for
`local`, `staging`, and `production-pilot` profiles. It does not deploy to
cloud, Kubernetes, Polygon, or any public network.

```bash
make deployment-check
make k8s-validate
make terraform-validate
make pilot-bootstrap-check
make pilot-backup-check
make pilot-deploy-check
make deployment-evidence
```

Generated deployment evidence artifacts:

```text
tmp/deployment-evidence.json
tmp/deployment-evidence.txt
tmp/pilot-deploy-check-local.json
tmp/pilot-deploy-check-production-pilot.json
tmp/pilot-bootstrap-validate.json
tmp/pilot-bootstrap-smoke.json
tmp/pilot-backup-migration.json
tmp/pilot-backup-report.json
tmp/pilot-backup-restore-report.json
tmp/sqlite-restore-check.json
tmp/release-provenance.json
tmp/go-modules.txt
tmp/sbom.spdx.json # optional, only when syft is installed
```

Local configuration templates live under:

```text
config/local/
config/examples/
```

Kubernetes scaffolding lives under:

```text
deployments/k8s/
deployments/secrets/
```

Terraform scaffolding lives under:

```text
deployments/terraform/
```

The production-pilot Kubernetes overlay uses Secret references, mounted
manifest/TLS/KMS files, SQLite storage, cloud-KMS signer placeholders, HTTPS
peer URLs, per-validator peer certificate mounts, non-root security settings,
and `PQFABRIC_OPS_LISTEN_ADDR=:8090` for `/livez` and `/readyz` probes.
`/metrics` and internal readiness remain on the peer-mTLS validator surface.

`cmd/pilot-bootstrap` validates the provider-neutral secret contract and runs a
temporary seven-validator production-mode smoke using generated API keys,
manifest history, URI-SAN peer certificates, fake cloud-KMS signers, and SQLite
databases. It proves the bootstrap shape locally only; it does not fetch real
secrets, apply Kubernetes manifests, create cloud resources, or sign images.
The validation report includes redacted `secret_evidence` entries for expected
mount paths, External Secrets `secretStoreRef` references, resolved/unresolved
status, content class, and safe fingerprints only for reviewable public material
such as manifests and certificates. Tokens and private keys are never printed or
fingerprinted.

`scripts/release-provenance.sh` writes `tmp/release-provenance.json`,
`tmp/release-provenance.txt`, `tmp/go-modules.txt`, `tmp/image-digest.txt`, and
`tmp/cosign-verify.txt`. It records Go/tool versions, module inventory, image
reference/digest status, SBOM status, cosign verification evidence, and whether
the run is local or published. Missing optional local tools are reported as
explicit skips, while published release mode fails unless digest, SBOM, clean
git state, and cosign verification evidence are present.

SQLite upgrade safety is available through:

```bash
go run ./cmd/pqfabric migrate-sqlite --database-url ./data/validator.db
go run ./cmd/pqfabric migrate-sqlite --database-url ./data/validator.db --apply
go run ./cmd/pqfabric backup --database-url ./data/validator.db --backup-db ./tmp/validator.backup.db --manifest ./manifest.json --history ./manifest.json
go run ./cmd/pqfabric restore-check --database-url ./tmp/validator.backup.db --manifest ./manifest.json --history ./manifest.json
```

These commands report schema version, integrity check result, checksums, record
counts, and restored receipt verification status. They are local SQLite pilot
evidence only, not a managed backup system.

Do not run `terraform apply` for this phase. The Terraform files are
validation-only scaffolding and contain no real cloud account, provider,
remote-state, or credential configuration.

## CI/CD

GitHub Actions are split into three production-path gates:

- `ci` runs Go tests/vet, `make verify`, focused race tests, Foundry contract
  tests, docs/repo hygiene, and a macOS smoke suite.
- `production-readiness` validates Compose, Kubernetes overlays, Terraform,
  pilot bootstrap, SQLite backup/restore, deployment evidence, release
  provenance, and e2e evidence, then uploads evidence artifacts.
- `security` runs CodeQL, gitleaks secret scanning, and Trivy dependency
  scanning for high/critical library vulnerabilities.
- `release-artifacts` builds multi-architecture images, scans them, publishes
  signed artifacts to `ghcr.io/keithwegner/pq-fabric` on `main` and `v*` tags,
  and uploads digest/SBOM/provenance evidence.

See `docs/ci-cd.md` and `docs/release-artifacts.md` for the required-check and
artifact policy. The current CD boundary publishes signed images only; it does
not apply Kubernetes manifests, run Terraform apply, or deploy cloud resources.

## Packaging and handoff

Build local binaries or the Docker image with:

```bash
go build ./...
make image
```

Create a source/docs handoff archive that excludes generated state, tmp output, `.env` files, private keys, wallet files, and Terraform state:

```bash
make package-handoff
```

Create an evidence-only archive:

```bash
make package-evidence
```

Generated evidence artifacts under `tmp/` are copied into controlled package folders when packaging. Do not include raw `data/` directories or secrets in a handoff bundle.

## Validator storage

The local demo still defaults to memory storage so it starts cleanly every run:

```bash
go run ./cmd/demo
```

The validator daemon supports memory, durable JSON/JSONL, and SQLite storage:

```bash
go run ./cmd/validator --storage memory
go run ./cmd/validator --storage durable --data-dir ./data/validator-1
go run ./cmd/validator --storage sqlite --database-url ./data/validator-1/validator.db
```

Equivalent environment variables are also supported:

```bash
STORAGE=durable DATA_DIR=./data/validator-1 go run ./cmd/validator
STORAGE=sqlite PQFABRIC_DATABASE_URL=./data/validator-1/validator.db go run ./cmd/validator
```

Durable storage persists:

- latest committed height/hash,
- latest committed state digest and lock metadata,
- committed block log,
- quorum certificate JSON for committed blocks,
- validator identity key reference metadata,
- idempotency/replay ledger records,
- optional checkpoint/snapshot records.

This is prototype durability for local research and restart testing, not production-grade database hardening.

## Documentation index

- `docs/architecture.md` — subsystem architecture and deployment scaffold overview.
- `docs/implementation-status.md` — current implementation state and validation evidence.
- `docs/crypto-validation.md` — ACVTS-style vector harness boundary.
- `docs/failure-evidence.md` — controlled local fault evidence.
- `docs/routing-testbed.md` — private-testbed routing model.
- `docs/bundle-protocol.md` and `docs/ai-context-channels.md` — bundle/AI context scaffolding.
- `docs/identity-anchors.md` and `docs/contracts-polygon.md` — anchor interface and contracts.
- `docs/deployment-local.md` — local Docker Compose workflow.
- `docs/deployment-k8s.md` — Kubernetes scaffold.
- `docs/deployment-terraform.md` — Terraform scaffold.
- `docs/operations-runbook.md` — local operations and evidence workflow.
- `docs/final-handoff.md` — final evaluator handoff guide.
- `docs/evidence-index.md` — evidence artifact interpretation.
- `docs/final-architecture-summary.md` — concise final architecture summary.
- `docs/claim-safety-review.md` — safe and unsafe external language.
- `docs/release-notes.md` — `v0.1.0-local-prototype` notes.

## Repository layout

```text
cmd/demo                 local seven-validator demo
cmd/fault-demo           controlled local fault-evidence demo
cmd/bundle-demo          bundle protocol and AI context evidence demo
cmd/anchor-demo          local Polygon anchor interface evidence demo
cmd/deployment-evidence  local deployment evidence generator
cmd/pilot-bootstrap      secret contract validator and bootstrap smoke
cmd/pilot-deploy-check   deployment profile contract validator
cmd/sqlite-restore-check local SQLite receipt restore verifier
cmd/e2e-demo             integrated local demo
cmd/e2e-evidence         integrated evidence generator
cmd/healthcheck          container-local HTTP health probe helper
cmd/relay                local private-testbed relay daemon entrypoint
cmd/validator            validator daemon entrypoint
config/local             safe local topology/config templates
config/examples          placeholder environment templates
contracts/client         optional Polygon client boundary placeholder
core/anchors             anchor interface, mock backend, identity/QC mapping
core/crypto              crypto interfaces and compatibility exports
core/crypto/dev          development-only deterministic adapters
core/crypto/mlkem        ML-KEM-768 adapter
core/crypto/mldsa        ML-DSA-87 adapter
core/crypto/suite        dev/pq suite selector
core/identity            local genesis validator identities
core/messages            canonical JSON and hashing helpers
core/deployment          deployment profile validation helpers
core/storage             memory, durable file-backed, and SQLite validator storage
consensus/protocol       blocks, proposals, votes, quorum certificates
consensus/health         deterministic heartbeat, status, and evidence records
consensus/fault          local failure/remediation/catch-up evidence harness
consensus/state          deterministic transaction application and state digest
consensus/validator      HTTP validator runtime
bundle/ai_context        AI context virtual channel manager and local mock provider
bundle/ccsds             compatibility virtual channel scheduler
bundle/channel           channel budgets, compression hooks, eviction, scheduler
bundle/custody           custody transfer as validator quorum confirmation
bundle/evidence          bundle demo/evidence scenario
bundle/protocol          canonical bundle envelope and digest helpers
bundle/retransmit        idempotent retransmission ledger and reconnect queue
bundle/reconcile         deterministic bundle state reconciliation helper
routing/circuit          private-testbed telescoping circuit model
routing/relay            relay runtime, local session state, and exit policy
routing/socks5           restricted local SOCKS5 CONNECT proxy
routing/stream           logical stream ID manager
routing/testbed          local routing scenario and evidence harness
contracts/polygon        Foundry project for Polygon-compatible anchors
deployments/k8s          Kubernetes scaffold for future controlled deployments
deployments/secrets      provider-neutral Kubernetes secret contract examples
deployments/terraform    Terraform scaffold for future multi-region planning
tests/crypto_vectors     compact ACVTS-style crypto fixtures
```

## Next engineering steps

1. Human reviewer runs `make final-verify` and inspects `tmp/e2e-evidence.json`.
2. Human reviewer checks `docs/claim-safety-review.md` before external sharing.
3. Any future work should be explicitly scoped; this handoff does not imply production deployment readiness.
