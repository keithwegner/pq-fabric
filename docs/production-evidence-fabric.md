# Production Evidence Fabric Roadmap

This document defines the first production-facing shape for `pq-fabric`: a
permissioned evidence fabric for a regulated pilot.

The pilot product is an API/CLI service where approved consortium participants
submit hashes and metadata, receive 5-of-7 quorum-confirmed receipts, verify
those receipts later, and optionally track testnet anchoring status.

## V1 workflow

1. A client submits an `EvidenceSubmission` to `POST /v1/evidence`.
2. The receiving validator canonicalizes the submission into an evidence event.
3. The validator proposes that event through the existing 7-validator consensus
   path.
4. A receipt is returned only after a 5-of-7 precommit quorum certificate exists.
5. The receipt is persisted by evidence ID, receipt ID, idempotency key, commit
   height, submitter, and quorum-certificate hash.
6. A client verifies the receipt through `POST /v1/verify` or exports the
   receipt and verifies its canonical hashes and quorum evidence independently.

## External API

- `POST /v1/evidence` submits a hash-only evidence event.
- `GET /v1/evidence/{id}` returns the receipt for an evidence ID.
- `GET /v1/receipts/{receipt_id}` returns a receipt by receipt ID.
- `POST /v1/verify` verifies a receipt, receipt ID, or evidence ID.
- `GET /v1/anchors/{qc_hash}` returns local anchoring status for a QC hash.
- `GET /v1/audit/recent` returns recent API audit records for admin-scoped
  keys.
- `GET /v1/ops/report` returns an admin-scoped operator report with readiness,
  peer health, recent receipts, audit summaries, metrics, and receipt
  verification spot checks.

The current production-facing API stores hashes, metadata, receipts, and quorum
evidence only. It does not store full payloads or sensitive source artifacts.

## CLI

The `pqfabric` CLI wraps the external API:

```bash
pqfabric submit \
  --url http://127.0.0.1:8081 \
  --category incident-report \
  --artifact-hash sha256:<artifact-digest> \
  --metadata-hash sha256:<metadata-digest> \
  --organization bestgate \
  --idempotency-key incident-123 \
  --anchor

pqfabric status --url http://127.0.0.1:8081 --receipt-id <receipt-id>
pqfabric verify --url http://127.0.0.1:8081 --receipt-id <receipt-id>
pqfabric export-receipt --url http://127.0.0.1:8081 --receipt-id <receipt-id> --out receipt.json
pqfabric anchor --url http://127.0.0.1:8081 --qc-hash <qc-hash>
pqfabric report --url http://127.0.0.1:8081 --format text --audit-limit 50 --receipt-limit 20
```

Set `PQFABRIC_API_TOKEN` or pass `--token` when validators require API-key
authentication. The client still sends a bearer token; the validator stores and
checks only its configured token hash.

Generate a server-side config hash for an operator-generated token:

```bash
pqfabric auth hash-token --token <operator-generated-token>
```

The command prints a `sha256:<hex>` value for the API-key file. Do not commit
the raw token or a real local API-key file.
`config/examples/api-keys.example.json` is a disabled shape example only; copy
it to an ignored local path and replace it with real operator-managed hashes.

Generate a validator membership manifest from the selected suite:

```bash
pqfabric manifest generate \
  --suite pq \
  --consortium-id pilot-consortium \
  --membership-version 1 \
  --threshold 5 \
  --public-url-template 'https://{id}.validators.example.com' \
  --out config/local/consortium.manifest.json
```

The manifest contains public URLs, operator labels, signature/KEM public keys,
key fingerprints, key IDs, and expected peer TLS URI SAN values for the seven
active validators. It also carries an optional `signing_key_ref` for the
external KMS key resource used by that validator. It contains no private keys.
Verify a current manifest and history set with:

```bash
pqfabric manifest verify \
  --manifest config/local/consortium.manifest.json \
  --history config/local/consortium.manifest.json
```

## API-key authorization

Validators can load scoped API keys from `PQFABRIC_API_KEYS_FILE` or
`--api-keys-file`. The file contains hashed token records with an ID,
organization, role list, optional disabled flag, and optional expiration time.

Supported roles:

- `evidence:submit` for `POST /v1/evidence`.
- `evidence:read` for `GET /v1/evidence/{id}` and
  `GET /v1/receipts/{receipt_id}`.
- `evidence:verify` for `POST /v1/verify`.
- `anchor:read` for `GET /v1/anchors/{qc_hash}`.
- `admin:read` for `GET /v1/audit/recent`.

The legacy `API_BEARER_TOKEN` setting remains a local-development fallback
outside production mode. It should not be used for a regulated pilot.

The `/v1` API records audit entries with request ID, key ID, organization,
method, path, status, duration, client address, and denial reason. Audit records
do not include raw API keys or source artifact contents. Lightweight in-memory
rate limiting is configured with `PQFABRIC_RATE_LIMIT_PER_MINUTE` and
`PQFABRIC_RATE_LIMIT_BURST`.

## Production guardrails

Production mode is enabled with:

```bash
PQ_FABRIC_PRODUCTION_MODE=1
PQ_FABRIC_CRYPTO_SUITE=pq
PQFABRIC_API_KEYS_FILE=./config/local/api-keys.json
PQFABRIC_CONSORTIUM_MANIFEST=./config/local/consortium.manifest.json
PQFABRIC_CONSORTIUM_MANIFEST_HISTORY=./config/local/consortium.manifest.json
PQFABRIC_SIGNER_PROVIDER=cloud-kms
PQFABRIC_KMS_ENDPOINT=https://kms-signing-gateway.example.invalid
PQFABRIC_KMS_KEY_ID=<optional-override-or-use-manifest-signing_key_ref>
PQFABRIC_KMS_TOKEN=<operator-managed-token-if-required>
PQFABRIC_KMS_CA_FILE=./secrets/kms-ca.crt
STORAGE=sqlite
PQFABRIC_DATABASE_URL=./data/validator-1/validator.db
PQFABRIC_PEER_TLS_CERT_FILE=./secrets/validator-1.crt
PQFABRIC_PEER_TLS_KEY_FILE=./secrets/validator-1.key
PQFABRIC_PEER_TLS_CA_FILE=./secrets/consortium-ca.crt
PQFABRIC_LOG_FORMAT=json
PQFABRIC_OTEL_ENABLED=false
PQFABRIC_OTEL_SERVICE_NAME=pq-fabric-validator
PQFABRIC_OTEL_EXPORTER_OTLP_ENDPOINT=
PQFABRIC_OTEL_EXPORTER_OTLP_HEADERS=
PQFABRIC_OTEL_EXPORTER_OTLP_INSECURE=false
```

Startup fails in production mode if the development crypto suite is selected or
if scoped API keys, current manifest, manifest history, SQLite storage, database
URL, external signer config, peer mTLS files, or HTTPS peer URLs are missing.
The current build supports `PQFABRIC_SIGNER_PROVIDER=cloud-kms` through an
external signing endpoint and `PQFABRIC_SIGNER_PROVIDER=local` only when
`PQFABRIC_ALLOW_LOCAL_SIGNER=1` is explicitly set for controlled local/pilot
exceptions. `hsm` remains a recognized extension point that fails closed until a
provider is implemented. This is still a regulated-pilot guardrail, not a
certification claim.

## Operator observability

Production-mode validators default to JSON logs. Local/demo validators default
to text logs. Set `PQFABRIC_LOG_FORMAT=text|json` to override this explicitly.

`/metrics` remains an internal peer endpoint and emits low-cardinality
Prometheus text metrics for API requests, evidence submissions, duplicate
submissions, receipt verification results, quorum availability, peer health and
lag, commit latency, signer/KMS errors, storage errors, invalid signatures, and
anchor status lookups.

`/ready` returns HTTP 200 only when required readiness checks pass. It reports
running state, storage reachability, active manifest membership, signer/KMS
reachability, API auth configuration, peer mTLS configuration, peer
reachability, and quorum availability. In production it remains protected by
peer mTLS because it is on the internal validator surface.

Kubernetes probes should use `PQFABRIC_OPS_LISTEN_ADDR` to expose only `/livez`
and `/readyz` on a separate minimal probe listener. The probe listener does not
serve `/metrics`, consensus endpoints, `/v1` API routes, raw node snapshots, or
secret-bearing configuration.

OpenTelemetry tracing is disabled by default. If
`PQFABRIC_OTEL_ENABLED=true`, `PQFABRIC_OTEL_EXPORTER_OTLP_ENDPOINT` must be a
valid OTLP/HTTP endpoint URL or startup fails. Traces cover API requests,
evidence submission, consensus proposal/commit paths, peer requests, and KMS
signing without recording raw API tokens, private keys, payload bodies, or full
metadata.

## Consortium model

The first deployment model is a known seven-validator consortium with a fixed
5-of-7 quorum threshold. Validator identities are explicit, permissioned, and
managed through the consortium manifest and operator governance outside the
public API.

Validator replacement should be manual for v1:

1. Record the retiring validator ID, key fingerprint, and final observed height.
2. Add the replacement validator identity to the next consortium manifest.
3. Point `signing_key_ref` to the new KMS key resource and keep the old manifest
   available so old receipts remain verifiable.
4. Roll the new manifest only after at least five operators approve it.
5. Issue peer certificates with URI SAN
   `spiffe://<consortium_id>/validator/<validator_id>`.

## Controlled deployment readiness

`make pilot-deploy-check` validates the local and production-pilot profile
contracts, checks the staging and production-pilot Kubernetes overlays, runs
the pilot bootstrap validation/smoke path, performs a local SQLite
schema migration dry-run plus backup/restore receipt verification, validates
Terraform when available, and records release provenance evidence. It is
evidence for operator review only: it does not apply manifests, apply
Terraform, create cloud resources, fetch or upload real secrets, publish
images, sign images, or enable Polygon mainnet anchoring.

`cmd/pilot-bootstrap validate --spec config/examples/pilot-bootstrap.example.yaml`
checks the provider-neutral secret reference shape, External Secrets
`secretStoreRef` entries, and resolved mounted material when available. The
report includes redacted secret evidence for expected mount paths,
resolved/unresolved status, content class, and safe fingerprints only for
manifests and certificate material. External secret values are reported as
unresolved rather than printed. `cmd/pilot-bootstrap smoke` then generates
temporary API keys, a seven-validator manifest/history, peer certificates with
`spiffe://<consortium_id>/validator/<validator_id>` URI SANs, fake cloud-KMS
signers, and SQLite databases, issues a receipt, verifies it, copies/restores
the SQLite DB, and verifies the restored receipt.

`pqfabric migrate-sqlite`, `pqfabric backup`, and `pqfabric restore-check`
provide the first local upgrade-safety gate. The reports include schema version,
SQLite integrity result, checksums, record counts, and recent receipt
verification against manifest history when operators provide manifests.

## What a receipt proves

A valid receipt proves that:

- the submitted evidence hash and metadata hash were canonicalized into a stable
  event hash,
- the event was included in a committed validator block,
- at least five known validators signed the same precommit quorum certificate,
- the receipt can carry the membership version and validator-set hash used when
  it was issued,
- the receipt hashes, block hash, QC hash, signer list, and submission still
  match each other.

## What a receipt does not prove

A receipt does not prove that:

- the original artifact is truthful,
- the original artifact is available,
- the original artifact was ever stored by `pq-fabric`,
- the metadata is complete,
- the validators are legally independent,
- the implementation is formally verified,
- the crypto is FIPS 140-3 certified or ACVTS validated,
- Polygon anchoring happened unless an anchor transaction is present and
  independently verified.

## Deliberate v1 exclusions

- No public validator admission.
- No public relay network or public exits.
- No full payload custody.
- No mainnet anchoring requirement.
- No FIPS, ACVTS, production post-quantum, production BFT, production privacy,
  or smart-contract audit claims.
