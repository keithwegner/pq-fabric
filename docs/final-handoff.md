# Final Handoff

## Executive Summary

`pq-fabric` is a runnable local Go prototype that demonstrates a seven-validator quorum network, durable validator state, controlled recovery evidence, private-testbed routing, bundle/custody and AI-context scaffolding, mock-backed Polygon-compatible anchors, and safe deployment scaffolding.

The handoff objective is reproducibility. A reviewer should be able to run local validation, generate evidence, inspect the architecture, and package the repo without cloud infrastructure, public networks, live Polygon, or external AI APIs.

## What The Prototype Demonstrates

- Seven local validators with 5-of-7 quorum certificates.
- Deterministic consensus height/round/stage behavior, proposer rotation, lock state, and state digests.
- Durable file-backed validator state for restart/idempotency tests plus SQLite storage for the regulated-pilot path.
- Controlled two-validator failure, remediation, catch-up, and message-preservation evidence.
- Private local three-hop routing testbed with relay-local visibility, stream multiplexing, SOCKS5-style flow, and restricted local exits.
- Bundle protocol scaffolding with virtual AI context channels, priority scheduling, custody confirmation, retransmission, deduplication, and reconciliation.
- Local mock OpenAI-compatible request/response shape with no external API call.
- Polygon-compatible anchor contracts plus Go mock anchor backend for identity, credential, governance, and quorum-certificate hash anchors.
- Docker Compose, Kubernetes, Terraform, and signed-release scaffolding for local handoff and future controlled deployment planning.

## Prototype-Only Boundaries

- Development crypto remains the default for deterministic local tests.
- The `pq` suite is engineering validation only.
- Consensus is a first-principles local prototype, not formally verified BFT.
- Self-healing is an in-process/local evidence harness.
- Routing is private-testbed only and has no public exits.
- Bundle and AI context handling are local deterministic scaffolds.
- Polygon anchoring stores hashes/metadata only; PQ and QC verification remain off-chain.
- Deployment files are scaffolding and validation targets only.
- Signed release artifacts are deployable inputs, not proof of a live deployment.

## Run Everything

```bash
make final-verify
make e2e-demo
make e2e-evidence
make package-handoff
make package-evidence
```

Optional tools may be skipped with clear messages:

- Docker daemon for image builds.
- Syft and cosign for local SBOM/signature evidence.
- Foundry `forge` for Solidity tests.
- `kubectl` for Kubernetes rendering.
- Terraform for scaffold validation.

## Demos

```bash
make demo
make fault-demo
make routing-demo
make bundle-demo
make anchor-demo
make deployment-evidence
make e2e-demo
```

## Tests

```bash
go test ./...
make lint-lite
make crypto-vectors
make routing-tests
make bundle-tests
make anchor-tests
make contract-tests
```

## Evidence

```bash
make failure-evidence
make routing-evidence
make bundle-evidence
make anchor-evidence
make deployment-evidence
make e2e-evidence
```

Artifacts are generated under `tmp/` and are ignored by Git.

## Packaging

```bash
make package-handoff
make package-evidence
```

Artifacts are written under `dist/`:

- `dist/pq-fabric-handoff.tar.gz`
- `dist/pq-fabric-evidence.tar.gz`

The handoff archive excludes `.git`, generated validator data, raw tmp runtime state except selected evidence copied into an `evidence/` folder, real env files, non-example tfvars, Terraform state, `.terraform/`, private keys, wallet files, kubeconfigs, compiled binaries, and local logs.

## Tooling Assumptions

- Go 1.25 or newer module behavior.
- Docker and Docker Compose for local container checks when available.
- Terraform and kubectl are optional validation tools.
- Foundry is optional for Solidity tests.

## Non-Claims

This handoff does not claim FIPS certification, ACVTS certification, production post-quantum security, production BFT safety, production anonymity, production self-healing, production AI infrastructure, production data sovereignty, audited smart contracts, live Polygon deployment, live cloud deployment, or deployment readiness for production use.

## Suggested Human Review

1. Run `make final-verify`.
2. Run `make e2e-evidence`.
3. Inspect `tmp/e2e-evidence.json` and `docs/evidence-index.md`.
4. Review `docs/claim-safety-review.md` before using any external-facing language.
5. Review contracts separately before any real testnet work.
6. Review secret handling before connecting any live infrastructure.
