# Release Notes

## v0.1.0-local-prototype

Date: 2026-05-22

## Implemented Capabilities

- Seven-validator local consensus demo with 5-of-7 quorum.
- Development and PQ crypto suite abstraction with focused vector tests.
- Durable file-backed validator state for local restart testing and SQLite storage for regulated-pilot hardening.
- Consensus hardening with height/round/stage metadata, proposer rotation, lock state, and deterministic state digests.
- Controlled fault evidence with failure detection, remediation, catch-up, convergence, and message-preservation metrics.
- Private local routing testbed with three-hop circuits, layered cells, restricted SOCKS5-style flow, local services, stream multiplexing, and evidence artifacts.
- Bundle protocol scaffold with virtual AI context channels, priority scheduling, custody quorum evidence, retransmission, deduplication, reconciliation, and local mock AI request/response shape.
- Polygon-compatible anchor contracts and Go mock backend for identity, credential, governance, and QC hash anchors.
- Local deployment path with Docker, Compose, Kubernetes, Terraform scaffolding, deployment evidence, runbooks, and config templates.
- Final integrated e2e evidence command and handoff/evidence packaging.
- Evidence fabric v1 foundation with hash-only submissions, quorum-confirmed receipts, receipt verification, durable evidence indexes, `/v1` API endpoints, `pqfabric` CLI, readiness/metrics endpoints, scoped API-key support, production-mode crypto guardrails, operator docs, and a regulated-pilot threat model.
- Regulated-pilot API hardening with scoped hashed API keys, role checks, `pqfabric auth hash-token`, `/v1/audit/recent`, durable audit records, lightweight per-key rate limiting, and production-mode requirement for an API-key file.
- Consortium identity hardening with JSON membership manifests, `pqfabric manifest generate`, active validator key-fingerprint validation, receipt membership version/hash metadata, a validated `cloud-kms` signer adapter, and a signer-provider boundary that still fails closed for native HSM mode.
- Operator observability with structured logs, optional OTLP/HTTP OpenTelemetry tracing, stronger readiness checks, richer low-cardinality Prometheus metrics, alert-rule templates, `GET /v1/ops/report`, and `pqfabric report`.
- Controlled deployment readiness with `local`, `staging`, and `production-pilot` profile validation, a dedicated `PQFABRIC_OPS_LISTEN_ADDR` probe surface, production-pilot Kubernetes overlays with Secret references and hardened pod settings, provider-neutral secret contract docs, SQLite restore verification, and release provenance evidence.
- Pilot bootstrap readiness with `cmd/pilot-bootstrap`, `make pilot-bootstrap-check`, External Secrets store-ref contract validation, redacted secret-source evidence, generated seven-validator production-mode smoke, per-validator URI-SAN peer TLS, fake cloud-KMS signers, and SQLite restore verification.
- SQLite backup and upgrade safety with schema migration version tracking, `pqfabric migrate-sqlite`, `pqfabric backup`, `pqfabric restore-check`, checksum/integrity reports, and receipt spot-check verification.
- Release provenance evidence with schema-versioned JSON output, Go module inventory, image reference/digest status, optional SBOM generation, optional cosign verification, and explicit skipped statuses for unavailable tools.
- GitHub Actions CI/CD gates for Go tests/vet, focused race tests, Foundry contracts, production-readiness evidence, CodeQL, gitleaks, Trivy, scheduled drift checks, and manual dispatch.
- Signed release-artifact workflow for multi-architecture GHCR images, image scanning, SBOM/provenance evidence, keyless cosign signing, build attestations, and digest-pinned deployment placeholders.
- AWS/EKS staging activation path with a manual GitHub environment-gated workflow, signed digest verification, AWS OIDC, External Secrets references for AWS Secrets Manager, PVC-backed SQLite, post-deploy smoke evidence, and explicit non-claims.

## Validation Performed

The final handoff flow is:

```bash
make final-verify
make e2e-demo
make e2e-evidence
make package-handoff
make package-evidence
```

Optional checks skip clearly when local tools are unavailable.

## Evidence Generated

- `tmp/failure-evidence.json`
- `tmp/routing-evidence.json`
- `tmp/bundle-evidence.json`
- `tmp/anchor-evidence.json`
- `tmp/deployment-evidence.json`
- `tmp/pilot-deploy-check-production-pilot.json`
- `tmp/pilot-bootstrap-validate.json`
- `tmp/pilot-bootstrap-smoke.json`
- `tmp/pilot-backup-migration.json`
- `tmp/pilot-backup-report.json`
- `tmp/pilot-backup-restore-report.json`
- `tmp/sqlite-restore-check.json`
- `tmp/release-provenance.json`
- `tmp/image-digest.txt`
- `tmp/cosign-verify.txt`
- `tmp/go-modules.txt`
- `tmp/sbom.spdx.json` when `syft` is installed
- `tmp/aws-staging-render.yaml` from static validation or the AWS staging workflow
- `tmp/aws-staging-deploy-summary.json` from the AWS staging workflow
- `tmp/e2e-evidence.json`
- GitHub Actions artifacts from `production-readiness`, `release-artifacts`, and
  manual `aws-staging-deploy` runs

## Known Limitations

- Local prototype only.
- Development crypto remains the default.
- No production consensus proof or production liveness claim.
- No production self-healing infrastructure.
- No public routing or public exits.
- No external AI API calls.
- No live Polygon deployment.
- No production cloud deployment.
- No smart-contract audit.
- Evidence fabric now includes peer mTLS enforcement for internal validator endpoints, manifest-history receipt verification, a SQLite transactional storage backend with schema version checks, a cloud-kms remote signer adapter, operator observability/reporting, controlled deployment readiness checks, backup/restore reporting, provider-neutral External Secrets evidence, AWS staging ExternalSecret references, signed release provenance evidence, and a provider-neutral pilot bootstrap smoke. It does not yet include native HSM signing, live Polygon testnet transactions, centralized audit shipping, managed dashboards, live alert delivery, production cloud deployment, managed backups, or release promotion automation.

## Non-Claims

This release does not claim FIPS certification, ACVTS certification, production post-quantum security, production BFT safety, production anonymity, production privacy, production self-healing, production AI infrastructure, production data sovereignty, audited smart contracts, live Polygon deployment, or live cloud deployment.

## Recommended Human Review

- Run final validation and inspect e2e evidence.
- Review claim safety language before sharing.
- Review contracts and generated ABI strategy before any live chain work.
- Review secret management and deployment controls before any controlled infrastructure work.
