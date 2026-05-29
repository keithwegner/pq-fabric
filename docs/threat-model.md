# Regulated Pilot Threat Model

This threat model covers the v1 evidence fabric pilot. It is scoped to a known
seven-validator consortium and hash-only evidence receipts.

## Assets

- Evidence submissions: category, artifact hash, metadata hash, submitting
  organization, idempotency key, and optional anchor request.
- Evidence receipts and exported quorum evidence.
- Validator signing keys and KEM keys.
- Validator durable state, commit log, evidence indexes, and idempotency indexes.
- API key tokens, hashed API-key config, audit records, and deployment secrets.
- Structured logs, metrics, traces, readiness reports, and operator incident
  reports.
- Consortium manifests with membership version, active validator IDs, operator
  labels, public URLs, algorithms, key fingerprints, and key IDs.
- Optional testnet anchoring configuration.

## Trust assumptions

- Validators are permissioned and operated by known parties.
- At least five validators are honest and available for a receipt to be issued.
- Clients keep source artifacts outside `pq-fabric` and submit only hashes.
- Operators preserve old validator manifests so old receipts remain verifiable.

## Threats and required controls

| Threat | V1 control |
|---|---|
| Malicious client submits malformed evidence | Submission validation rejects missing schema, category, artifact hash, metadata hash, organization, or idempotency key. |
| Replay or duplicate submission | Idempotency keys and evidence IDs are indexed and return the existing receipt. |
| Minority validator outage | Existing 5-of-7 quorum path continues while up to two validators are unavailable. |
| Quorum unavailable | Fewer than five valid precommit votes cannot form a receipt. |
| Stale or divergent validator | Commit verification checks previous hash, state digest, scheduled proposer, stage, threshold, and QC signatures. |
| Development crypto used in production | Production mode fails startup when the `dev` crypto suite is selected. |
| Unknown or mismatched validator identity | Production mode requires a consortium manifest/history, verifies active validator key fingerprints, and requires peer mTLS certificates with validator URI SANs. |
| Accidental fallback from KMS/HSM to local signing | Production mode rejects local signing unless explicitly allowed; the cloud-kms adapter validates the remote public key against the manifest before startup succeeds. |
| Unauthenticated API use | Validators can require scoped API keys for `/v1` API calls; production mode requires an API-key file with hashed tokens. |
| Overprivileged client token | `/v1` handlers require scoped roles such as `evidence:submit`, `evidence:read`, `evidence:verify`, `anchor:read`, or `admin:read`. |
| Brute force or accidental client loops | `/v1` applies lightweight per-key rate limits and a stricter anonymous limiter for invalid/missing credentials. |
| Operator blind spot during API misuse | `/v1` writes audit records with key ID, organization, method, path, status, duration, and denial reason without logging raw tokens. |
| Operator blind spot during quorum or storage degradation | `/ready`, `/metrics`, alert templates, and `GET /v1/ops/report` surface quorum availability, peer lag, storage errors, signer/KMS errors, invalid signatures, and verification spot checks. |
| Observability leaks secrets | Logs, metrics, traces, and reports must not include raw API tokens, private keys, payload bodies, or full metadata. Metrics use route patterns instead of evidence IDs or receipt IDs. |
| Key loss or replacement | V1 requires manual consortium manifest governance, retention of old manifests for receipt verification, and startup validation against manifest history. |
| Sensitive payload leakage | V1 stores hashes, metadata, receipts, and quorum evidence only; full artifacts are out of scope. |
| Operator mistake | Runbooks and release checks must validate config, crypto mode, secrets, storage, and receipt verification before rollout. |
| Kubernetes probe accidentally bypasses peer mTLS | The optional `PQFABRIC_OPS_LISTEN_ADDR` surface exposes only `/livez` and `/readyz`; `/metrics`, internal `/ready`, and consensus endpoints stay on the peer-mTLS validator listener. |
| Broken backup or restore process | `pqfabric backup`, `pqfabric restore-check`, and `make pilot-deploy-check` run local SQLite integrity, checksum, and receipt verification checks so operators have evidence before adapting the process to a managed backup system. |
| Unsafe schema upgrade | SQLite schema versioning and `pqfabric migrate-sqlite` provide dry-run and explicit-apply migration checks; startup rejects unsupported future schema versions. |
| Miswired secret source before rollout | `cmd/pilot-bootstrap validate` checks the provider-neutral secret reference contract, External Secrets store refs, required keys, manifest/API/TLS/KMS content when available, and unresolved status without printing raw secret values. |
| Release artifact ambiguity | `scripts/release-provenance.sh` records Go/tool versions, module inventory, image reference/digest status, optional SBOM generation, and optional cosign verification with explicit skipped statuses. |
| Shared or wrong validator TLS material | Kubernetes overlays mount per-validator certificate and key files, and the bootstrap smoke issues URI-SAN certificates shaped as `spiffe://<consortium_id>/validator/<validator_id>` before forming a receipt. |
| Chain/RPC outage | Testnet anchoring is optional; receipt validity does not depend on anchoring. |

## Open production gaps

The following controls remain required before broad production use:

- Client-side mTLS for the public `/v1` API if the pilot requires it; validator-to-validator mTLS is implemented for internal endpoints.
- Native HSM providers and governance automation beyond the current cloud-kms signing endpoint boundary.
- PostgreSQL or managed database support beyond the current SQLite backend.
- Centralized audit shipping, SIEM integration, managed dashboards, and live
  alert delivery.
- Real cloud/Kubernetes deployment resources with provider-specific secret-manager integration.
- Formal incident response, managed backup restore, and disaster recovery drills.
- Independent security review of contracts and validator networking.
- Load, soak, and partition testing under realistic network conditions.

These gaps should be treated as blocking for high-risk production environments,
but they do not block a tightly controlled regulated pilot.
