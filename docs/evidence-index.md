# Evidence Index

All evidence is local engineering evidence. It is not certification, production readiness, or a live deployment record.

## Crypto Vector Evidence

- Generate: `make crypto-vectors`
- Output: terminal pass/fail output.
- Shows: ML-KEM-768 and ML-DSA-87 adapter vector fixtures pass deterministic engineering checks.
- Does not show: FIPS certification, ACVTS validation, production key management, or production post-quantum security.
- Failure interpretation: inspect the failing vector case ID and adapter package.

## Consensus Demo Evidence

- Generate: `make demo`
- Output: terminal transcript.
- Shows: 7-of-7 commit, 5-of-7 commit during two-validator failure, deterministic catch-up, and idempotent retransmission behavior.
- Does not show: formal BFT proof, production liveness, or production safety.
- Failure interpretation: inspect validator startup, quorum formation, and local port collisions.

## Failure Evidence

- Generate: `make failure-evidence`
- Output:
  - `tmp/failure-evidence.json`
  - `tmp/failure-evidence.txt`
- Shows: deterministic health transitions, failure detection, remediation start/completion, catch-up convergence, and application-level message preservation.
- Does not show: production orchestration, production monitoring, or transport-level zero packet loss.
- Failure interpretation: inspect logical tick thresholds and durable temp state under `tmp/fault-demo-data`.

## Routing Evidence

- Generate: `make routing-evidence`
- Output:
  - `tmp/routing-evidence.json`
  - `tmp/routing-evidence.txt`
- Shows: seven local relays, three-hop circuit construction, per-hop handshakes, local echo/HTTP flow, stream multiplexing, destination rejection, and relay-local visibility.
- Does not show: public anonymity, privacy guarantees, censorship resistance, traffic obfuscation, or public exit safety.
- Failure interpretation: inspect crypto suite selection and local exit policy.

## Bundle Evidence

- Generate: `make bundle-evidence`
- Output:
  - `tmp/bundle-evidence.json`
  - `tmp/bundle-evidence.txt`
- Shows: AI context virtual channels, priority scheduling, canonical bundle envelopes, custody quorum confirmation, retransmission, duplicate suppression, reconciliation, and local mock AI response digesting.
- Does not show: production AI behavior, live model integration, production data sovereignty, or reliable transport over real networks.
- Failure interpretation: inspect channel budgets, transaction IDs, and custody quorum evidence.

## Anchor Evidence

- Generate: `make anchor-evidence`
- Output:
  - `tmp/anchor-evidence.json`
  - `tmp/anchor-evidence.txt`
- Shows: local mock anchor registration and lookup for validator identities, credentials, governance proposals, and QC hashes, plus duplicate/mismatch behavior.
- Does not show: live Polygon deployment, on-chain PQ verification, smart-contract audit, or production governance safety.
- Failure interpretation: inspect mock roles and duplicate/replay policy.

## Evidence Fabric API Evidence

- Generate: `go test ./core/evidence ./core/storage ./consensus/validator`
- Output: terminal pass/fail output.
- Shows: hash-only submission validation, canonical payload round-trip, quorum-confirmed receipt construction, receipt verification, durable evidence indexes, SQLite storage behavior, SQLite schema migration checks, local backup/restore reporting, scoped API-key role enforcement, audit records, lightweight rate limiting, consortium manifest/history validation, peer mTLS checks, cloud-kms signer startup/signing checks, HSM fail-closed behavior, readiness checks, low-cardinality metrics, OpenTelemetry exporter config, operator reports, and production-mode rejection of development crypto or missing identity/auth/storage/transport/key-custody config.
- Does not show: native HSM signing, centralized audit shipping/SIEM delivery, managed dashboards, live Polygon anchoring, or a certified production deployment.
- Failure interpretation: inspect `core/evidence`, `core/storage`, and `/v1` validator handler tests.

## Deployment Evidence

- Generate: `make deployment-evidence`
- Output:
  - `tmp/deployment-evidence.json`
  - `tmp/deployment-evidence.txt`
- Shows: Dockerfile presence, Compose topology validation, validator/relay service counts, local-only networking, Kubernetes rendering when available, Terraform validation when available, config templates, secret guardrails, redacted bootstrap secret-source evidence, release provenance status, and signed-image placeholder wiring.
- Does not show: cloud deployment, Kubernetes deployment, Terraform apply, live Polygon, or public network readiness.
- Failure interpretation: inspect optional tool availability and config rendering errors.

## AWS Staging Evidence

- Generate locally: `make aws-staging-check`
- Generate in GitHub Actions: run `aws-staging-deploy` manually.
- Output:
  - `tmp/aws-staging-render.yaml`
  - `tmp/aws-staging-cosign-verify.txt`
  - `tmp/aws-staging-deploy-summary.json`
  - `tmp/aws-staging-smoke.json`
  - `tmp/aws-staging-ops-report.json`
- Shows: AWS staging overlay render, ExternalSecret references, signed digest
  verification, optional EKS server-side dry-run/apply, readiness, hash-only
  submit, receipt verification, and ops report evidence.
- Does not show: EKS cluster creation, AWS secret creation, Terraform apply,
  public ingress, production deployment, native AWS KMS ML-DSA signing, or
  certification.
- Failure interpretation: inspect image signature verification, AWS OIDC
  environment variables, EKS access, External Secrets readiness, signer
  endpoint reachability, validator rollout, and receipt verification.

## Secret And Release Evidence

- Generate: `make pilot-bootstrap-check`, `make release-provenance-check`, and `make release-artifacts-check`
- Output:
  - `tmp/pilot-bootstrap-validate.json`
  - `tmp/pilot-bootstrap-smoke.json`
  - `tmp/release-provenance.json`
  - `tmp/image-digest.txt`
  - `tmp/cosign-verify.txt`
  - `tmp/go-modules.txt`
- Shows: External Secrets store refs, expected secret mount paths, resolved/unresolved status, redacted content evidence, Go module inventory, image reference/digest status, SBOM status, cosign verification status, and static release-artifact workflow guardrails.
- Does not show: raw tokens, private keys, cloud secret fetches, cloud deployment, Kubernetes apply, Terraform apply, or release promotion.

## Integrated E2E Evidence

- Generate: `make e2e-evidence`
- Output:
  - `tmp/e2e-evidence.json`
  - `tmp/e2e-evidence.txt`
- Shows: one local integrated run across consensus/failure/durability, routing, bundle/mock AI, anchors, and deployment validation.
- Does not show: production-grade integration, public infrastructure behavior, cloud deployment, public exits, or external AI behavior.
- Failure interpretation: inspect the subsystem section that reports failure first; the e2e command reuses the same deterministic local harnesses used by the focused evidence commands.
