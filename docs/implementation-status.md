# pq-fabric implementation status

This document records the current prototype status for future implementation phases. It is engineering evidence only; it is not a certification claim.

## Current implemented capabilities

- Local seven-validator Go demo with validator IDs `validator-1` through `validator-7`.
- 5-of-7 quorum certificate formation from unique validator precommit votes for the same height, round, stage, and block hash.
- Explicit consensus height/round/stage metadata, deterministic proposer rotation, timeout-driven round advancement, conservative validator lock state, and deterministic state digests.
- Signed proposals and signed votes behind narrow crypto interfaces.
- Two-node failure demo where validator-6 and validator-7 stop, the remaining five validators commit, and the stopped validators catch up after restart.
- Memory storage for fast tests and the local demo.
- Durable file-backed validator storage for local restart tests and SQLite storage for the regulated-pilot production mode.
- Persisted committed block log, latest height/hash/state digest, quorum certificate JSON, validator identity reference metadata, lock metadata, and idempotency/replay ledger entries.
- In-memory vote cache, peer-health observation, and deterministic catch-up validation.
- Deterministic Phase 5 health model with heartbeat records, statuses, logical-tick failure detection, remediation evidence, and convergence evidence.
- Controlled fault demo that stops two validators, continues committing with 5-of-7 quorum, restarts failed validators, catches them up, and emits structured evidence artifacts under `tmp/`.
- Application-level message preservation metrics for submitted, committed, duplicate/replayed, and pending transaction IDs during a failure window.
- CCSDS-inspired virtual channel model for AI context channels: conversation, working memory, execution, and retrieval.
- Priority-weighted AI context scheduler with deterministic context budget enforcement, compression hooks, and eviction policies.
- Canonical bundle envelope with deterministic digesting and validation.
- Custody transfer represented as 5-of-7 validator quorum confirmation over custody event digests.
- Idempotent sequence-numbered retransmission ledger and reconnect queue keyed by transaction ID and bundle ID.
- Deterministic bundle state reconciliation after interruption.
- Local mock OpenAI-compatible request/response shape with deterministic responses and no external API calls.
- Bundle evidence demo that emits structured artifacts under `tmp/`.
- Private-testbed seven-relay routing topology with three-hop telescoping circuit construction, per-hop KEM-derived layer keys, layered encryption cells, restricted SOCKS5 CONNECT handling, stream multiplexing, exit-policy checks, and routing evidence artifacts.
- Polygon-compatible Foundry contract structure for identity, credential, governance, and quorum-certificate hash anchors.
- Go anchor-client interface with deterministic local mock backend.
- Validator identity metadata mapping to anchor records and deterministic QC hash anchor helpers.
- Local anchor demo and evidence artifacts under `tmp/`.
- Phase 9 local deployment path with one reusable Docker image, Docker Compose topology for seven validators and seven private-testbed relays, safe config templates, Kubernetes scaffold, Terraform scaffold, deployment evidence generator, local smoke script, and operational runbooks.
- Phase 10 final handoff layer with `make final-verify`, integrated e2e demo/evidence, docs and repo hygiene checks, final handoff documentation, release notes, and handoff/evidence package scripts.
- Evidence fabric v1 foundation with hash-only `EvidenceSubmission` canonicalization, quorum-confirmed `EvidenceReceipt` generation, receipt verification, durable evidence indexes, SQLite storage, peer mTLS for internal validator endpoints, cloud-kms remote signer custody, `/v1/evidence`, `/v1/receipts`, `/v1/verify`, `/v1/anchors`, `/v1/audit/recent`, `/v1/ops/report`, `pqfabric` CLI including `pqfabric report`, strengthened `/ready`, richer `/metrics`, a dedicated `PQFABRIC_OPS_LISTEN_ADDR` probe surface for `/livez` and `/readyz`, structured logs, optional OpenTelemetry tracing, scoped hashed API-key support, request audit records, lightweight rate limiting, consortium manifest/history validation, receipt membership metadata, signer-provider selection, and production-mode guardrails that reject development crypto and require API keys, manifest history, SQLite, external signing, and peer mTLS.

## Current limitations

- Durable local storage is still available as file-backed JSON/JSONL for local research and deterministic tests; production mode now requires the SQLite transactional backend.
- Consensus is a first-principles quorum prototype, not a production BFT protocol. It now has deterministic rounds, proposer rotation, lock checks, state digests, and controlled local fault evidence, but does not include production view-change proofs, slashing, formal verification, or production fault tolerance.
- The self-healing layer is an in-process/local test harness. It is not production orchestration, production monitoring, or a production restart controller.
- Routing is a private testbed artifact only. It is not a public anonymity network, public SOCKS5 proxy, production privacy system, censorship-resistance system, or production exit system.
- Bundle protocol pieces are still local scaffolding; they now include canonical envelopes, custody confirmation, retransmission, reconciliation, and mock AI context handling, but not production transport reliability, live model integration, production data sovereignty, or production-grade custody infrastructure.
- Polygon contracts have Foundry tests checked in, but local contract execution depends on Foundry being installed. `make contract-tests` skips clearly when `forge` is absent.
- Deployment artifacts are scaffolding except for the manual AWS staging workflow. Compose is local-only, Kubernetes manifests include staging, production-pilot, and AWS staging overlays with Secret references and hardened pod settings, and Terraform has no provider resources or remote backend. The AWS path targets an already-provisioned private EKS cluster only; it does not create cloud resources, public ingress, Polygon resources, or production claims.
- Integrated e2e evidence reuses deterministic local harnesses and subprocess-safe validation checks. It is not process-level production integration and does not exercise public infrastructure.
- The evidence fabric API is a regulated-pilot foundation. It does not implement native HSM signing, live testnet anchoring, centralized audit shipping, managed dashboards/SIEM delivery, production governance automation, production cloud deployment, release promotion automation, or certification.

## Crypto status

- The default local demo and deterministic tests use development-only adapters:
  - `DEV-ED25519-SLOT-FOR-ML-DSA-87` for signatures.
  - `DEV-X25519-SLOT-FOR-ML-KEM-768` for KEM/key agreement.
- These development adapters are not post-quantum secure and must not be represented as production crypto.
- The Phase 1 adapter structure adds selectable `dev` and `pq` suites behind the existing `core/crypto` interfaces.
- The `pq` suite uses implementation-backed ML-KEM-768 and ML-DSA-87 adapters from CIRCL for engineering validation.
- Phase 2 adds compact ACVTS-style JSON vector fixtures and make targets for ML-KEM-768 and ML-DSA-87 adapter validation.
- Passing local tests or vector tests does not imply FIPS 140-3 certification, ACVTS validation, or production readiness.

## Consensus status

- Implemented: signed proposals, prevote/precommit stages, deterministic proposer rotation, round advancement when the scheduled proposer is unavailable, 5-of-7 precommit quorum certificate formation, duplicate/conflicting vote rejection, unknown-validator rejection, wrong hash/stage rejection, conservative lock-state checks, deterministic state-transition digests, two-node-failure quorum commit, deterministic catch-up validation, and a local fault-evidence harness.
- The Phase 5 harness records heartbeat status changes, suspected/failed transitions, remediation start/completion, lagging recovery, final convergence, and message-preservation metrics.
- Not implemented: production BFT hardening, slashing, formal safety proofs, weighted validator sets, durable mempool policy, proposer fairness analysis, or production-grade self-healing infrastructure.

## Routing status

- Implemented: private seven-relay topology, explicit three-hop path selection, per-hop KEM extension messages, replay rejection for circuit extension, wrong-key and malformed-handshake rejection, layered onion cell wrapping/unwrapping, relay-local predecessor/successor state, local-only exit policy, restricted SOCKS5 CONNECT handling, stream ID multiplexing, local echo/HTTP services, and routing evidence artifacts.
- Not implemented: public relay discovery, public exits, production abuse controls, traffic padding, metadata-resistance validation, censorship-circumvention features, or production anonymity guarantees.

## Bundle protocol status

- Implemented: canonical bundle envelopes, stable bundle digests, virtual channels for conversation/working-memory/execution/retrieval, deterministic priority scheduling, context budget enforcement, no-op and gzip compression hooks, deterministic eviction, store-and-forward retransmission, transaction ID deduplication, quorum-confirmed custody events, reconciliation repair for missing bundle state, working-memory snapshots, local mock OpenAI-compatible request/response handling, and bundle evidence artifacts.
- Not implemented: production Bundle Protocol interoperability, production custody infrastructure, live AI model calls, production data-sovereignty controls, production transport reliability, or signed envelope key management beyond the local validator quorum evidence scaffold.

## Smart contract status

- Implemented: self-contained Foundry contracts for identity, credential, governance proposal, and quorum-certificate hash anchoring; minimal role checks; duplicate/replay reverts; Foundry tests for success, unauthorized calls, malformed inputs, duplicates, and events; optional deployment scripts; Go anchor interface; local mock backend; validator identity anchor mapping; deterministic QC hash anchoring helpers; anchor demo/evidence artifacts.
- Not implemented: generated Go ABI bindings, required live RPC integration, live Polygon deployment, on-chain PQ signature verification, smart-contract audit, or production governance safety.

## Deployment status

- Implemented: multi-command Dockerfile, local `pq-fabric:local` image target, bundled `pqfabric` CLI, Docker Compose config with seven validators, seven relay services under the `relays` profile, durable validator data directories, localhost-bound validator ports, internal Compose network, local demo/evidence service profiles, safe config templates under `config`, Kubernetes manifests under `deployments/k8s`, staging and production-pilot overlays with digest-pinned GHCR placeholders, AWS staging overlay with ExternalSecret references for AWS Secrets Manager, provider-neutral secret contract under `deployments/secrets`, External Secrets store-ref contract examples, redacted bootstrap secret evidence, pilot bootstrap validator/smoke command, SQLite migration/backup/restore-check commands, Terraform planning scaffold under `deployments/terraform`, deployment evidence artifacts, local deployment smoke script, SQLite restore check, signed release provenance evidence, and runbooks.
- Implemented Make targets: `image`, `compose-config`, `compose-up`, `compose-down`, `compose-logs`, `compose-clean`, `deploy-local`, `deploy-local-smoke`, `deployment-check`, `k8s-validate`, `terraform-validate`, `deployment-evidence`, `pilot-bootstrap-check`, `pilot-backup-check`, `pilot-deploy-check`, `aws-staging-check`, `sqlite-restore-check`, `release-provenance`, `release-provenance-check`, `release-artifacts-check`, and `package-handoff`.
- Not implemented: cloud resource creation, EKS cluster creation, Terraform apply, live Polygon deployment, live RPC dependency, managed observability deployment, production cloud deployment, or release promotion automation.

## Evidence fabric status

- Implemented: hash-only external API and CLI, first-class submission/receipt/verification types, evidence ID/receipt ID/idempotency/QC indexes, receipt export, manifest-history verification against receipt membership version/hash, optional anchor-status field, scoped hashed API keys with roles, `pqfabric auth hash-token`, `pqfabric manifest generate`, `pqfabric manifest verify`, `pqfabric report`, `pqfabric migrate-sqlite`, `pqfabric backup`, `pqfabric restore-check`, `/v1/audit/recent`, `/v1/ops/report`, memory/durable/SQLite audit and receipt records, SQLite schema migration tracking, backup integrity/checksum reports, lightweight per-key rate limiting, strengthened readiness, low-cardinality Prometheus text metrics, `PQFABRIC_OPS_LISTEN_ADDR` probe surface, structured logs, optional OTLP/HTTP OpenTelemetry tracing, alert-rule templates, JSON consortium manifest validation, peer mTLS for internal validator endpoints, per-validator Kubernetes TLS projection, cloud-kms remote signer validation/signing, receipt membership version/hash fields, HSM fail-closed boundary, provider-neutral bootstrap validation/smoke evidence with redacted secret-source reporting, AWS staging ExternalSecret references and deploy smoke workflow, threat model, consortium operator notes, and stricter production-mode startup checks.
- Not implemented: native HSM signing, automated governance approval for validator enrollment or replacement, live Polygon testnet transactions, centralized audit shipping, managed dashboards/SIEM delivery, production cloud deployment resources, or release promotion automation.

## Final handoff status

- Implemented: `cmd/e2e-demo`, `cmd/e2e-evidence`, `internal/e2e`, `make final-verify`, `make docs-check`, `make repo-hygiene`, `make package-handoff`, `make package-evidence`, final handoff docs, evidence index, final architecture summary, claim safety review, and local release notes.
- Integrated evidence covers consensus/fault/durability, routing, bundle/mock AI, anchors, deployment scaffold validation, tool availability, and explicit non-claims.
- Not implemented: managed observability stack, external dashboards, public deployment, live model calls, live Polygon transactions, or certification package.

## CI/CD status

- Implemented: GitHub Actions `ci`, `production-readiness`, `security`, and `release-artifacts` workflows on PRs, pushes to `main`, manual dispatch, and weekly schedules where appropriate; plus manual `aws-staging-deploy` for AWS EKS staging.
- Implemented CI checks: Go tests/vet, `make verify`, focused race tests, Foundry contract tests, docs/repo hygiene, macOS smoke tests, Kubernetes/Terraform/deployment evidence checks, AWS staging static checks, CodeQL, gitleaks, Trivy high/critical library scanning, multi-architecture image builds, image scanning, GHCR publishing on `main`/`v*` tags, keyless cosign signing, and provenance artifact upload.
- Implemented repository protection: `main` requires the production-path checks, uses strict status checks, requires linear history, and disallows force pushes and deletions.
- Not implemented: production deployment, Terraform apply, cloud resource creation, release promotion automation, or managed environment rollout beyond the manual AWS staging path.

## Test evidence

Baseline before Phase 0/1 edits:

```text
go test ./...     PASS
go run ./cmd/demo PASS
```

Phase 0/1 validation after the adapter structure was added:

```text
make fmt        PASS
go test ./...   PASS
make lint-lite  PASS
go run ./cmd/demo PASS
make verify     PASS
```

Phase 2 crypto vector validation:

```text
make crypto-vectors       PASS
make crypto-vectors-mlkem PASS
make crypto-vectors-mldsa PASS
```

Phase 3 durable validator state validation:

```text
make fmt            PASS
go test ./...       PASS
make lint-lite      PASS
go run ./cmd/demo   PASS
make verify         PASS
make crypto-vectors PASS
```

Phase 4 consensus hardening validation:

```text
go test ./...       PASS
go run ./cmd/demo   PASS
```

Phase 5 self-healing and controlled failure evidence validation:

```text
make fmt              PASS
go test ./...         PASS
go run ./cmd/demo     PASS
make verify           PASS
make crypto-vectors   PASS
make fault-demo       PASS
make failure-evidence PASS
make lint-lite        PASS
```

Generated Phase 5 evidence artifacts:

```text
tmp/failure-evidence.json
tmp/failure-evidence.txt
```

Private routing testbed validation:

```text
go test ./routing/... PASS
go run ./cmd/routing-demo PASS
```

Generated routing evidence artifacts:

```text
tmp/routing-evidence.json
tmp/routing-evidence.txt
```

Expected demo evidence remains:

```text
commit 1: height=1 round=0 votes=7/7 proposer=validator-1 state=<digest>
commit 2 under failure: height=2 round=0 votes=5/7 threshold=5 proposer=validator-2 state=<digest>
demo complete: 5-of-7 quorum committed during a 2-node failure; remediated nodes caught up deterministically.
```

Local validation commands:

```bash
make fmt
make lint-lite
make crypto-vectors
make verify
make fault-demo
make failure-evidence
make routing-tests
make routing-demo
make routing-evidence
make bundle-tests
make bundle-demo
make bundle-evidence
make anchor-tests
make contract-tests
make anchor-demo
make anchor-evidence
```

Phase 7 bundle protocol validation:

```text
go test ./bundle/... PASS
go run ./cmd/bundle-demo PASS
```

Generated bundle evidence artifacts:

```text
tmp/bundle-evidence.json
tmp/bundle-evidence.txt
```

Phase 8 anchor validation:

```text
go test ./core/anchors/... ./contracts/client/... PASS
go run ./cmd/anchor-demo PASS
forge --version NOT AVAILABLE IN CURRENT ENVIRONMENT
make contract-tests SKIP WITH CLEAR MESSAGE WHEN forge IS ABSENT
```

Generated anchor evidence artifacts:

```text
tmp/anchor-evidence.json
tmp/anchor-evidence.txt
```

Phase 9 deployment path validation:

```text
make image               OPTIONAL, requires Docker daemon
make compose-config      PASS when Docker Compose is installed
make deployment-check    PASS
make k8s-validate        PASS when kubectl kustomize is installed
make terraform-validate  PASS when Terraform is installed
make deployment-evidence PASS
make pilot-bootstrap-check PASS
make pilot-backup-check PASS
make release-provenance-check PASS
make release-artifacts-check PASS
```

Generated deployment evidence artifacts:

```text
tmp/deployment-evidence.json
tmp/deployment-evidence.txt
```

Controlled deployment readiness validation:

```text
go test ./core/deployment ./consensus/validator PASS
go test ./core/backup ./cmd/pilot-bootstrap ./cmd/pilot-deploy-check ./cmd/sqlite-restore-check ./cmd/deployment-evidence PASS
make pilot-deploy-check PASS when optional tools are installed or skipped clearly
```

Generated readiness artifacts:

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
tmp/image-digest.txt
tmp/cosign-verify.txt
```

Phase 10 final handoff validation:

```text
make docs-check       PASS
make repo-hygiene     PASS
make e2e-demo         PASS
make e2e-evidence     PASS
make final-verify     PASS
make package-handoff  PASS
make package-evidence PASS
```

Generated final evidence and package artifacts:

```text
tmp/e2e-evidence.json
tmp/e2e-evidence.txt
dist/pq-fabric-handoff.tar.gz
dist/pq-fabric-evidence.tar.gz
```

## Known risks

- Development crypto is intentionally present for deterministic tests and must remain isolated from any production claims.
- Deterministic PQ key helpers are suitable for tests and local demos only; real deployments need key management and high-quality entropy.
- Consensus safety/liveness evidence is limited to deterministic local tests for the current quorum prototype. It is not a production BFT or formal verification claim.
- Phase 5 failure evidence uses a local in-process harness and logical ticks. It is useful repeatable engineering evidence, not proof of production fault tolerance or production self-healing behavior.
- Message-preservation evidence is application-level transaction ID accounting. It does not claim transport-level zero packet loss.
- Routing must remain private-testbed only until authorization, exit policy, auditing, and abuse controls exist.
- The routing demo proves local circuit semantics and relay-local visibility, not production anonymity, production privacy, censorship resistance, or public relay safety.
- Bundle and AI context demos are local deterministic scaffolds. They do not call external AI APIs and do not prove production AI correctness, data sovereignty, transport reliability, or production custody semantics.
- Polygon anchors store hashes and metadata only. They do not verify ML-DSA, ML-KEM, PQ signatures, validator membership, quorum correctness, or consensus safety on-chain.
- The Solidity contracts are not audited and should not be described as production smart-contract security.
- Deployment scaffolding must not be described as production readiness. Do not run `terraform apply`, do not create cloud resources, do not expose public exits, and do not commit secrets or generated deployment state.
- Final handoff packages must be checked for accidental secrets, generated state, wallet files, kubeconfigs, private keys, and misleading claims before sharing.
- Certification language must remain precise: engineering validation is not FIPS or ACVTS certification.

## Next recommended phase

This is the final implementation/handoff state for the requested roadmap. Future work should start with human review of `docs/final-handoff.md`, `docs/evidence-index.md`, and `docs/claim-safety-review.md` before scoping any new engineering phase.
