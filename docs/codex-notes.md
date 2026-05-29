# Codex notes for pq-fabric

Use this repo as one Go monorepo. Do not split consensus, routing, bundle, contracts, or crypto adapters into separate repositories without an explicit architectural decision.

## Before changing code

Run the current baseline:

```bash
go test ./...
go run ./cmd/demo
```

Skim the roadmap and status docs before choosing scope:

```bash
sed -n '1,260p' ../pq_fabric_remaining_phases_codex_brief.md
sed -n '1,220p' docs/implementation-status.md
sed -n '1,220p' docs/architecture.md
```

## After changing code

Run formatting and validation:

```bash
make fmt
make lint-lite
make crypto-vectors
make verify
make fault-demo
make routing-tests
make routing-demo
make bundle-tests
make bundle-demo
make anchor-tests
make anchor-demo
make deployment-check
make deployment-evidence
make docs-check
make repo-hygiene
make e2e-evidence
```

If the change touches a narrow package, also run its focused tests while iterating.

## Architectural boundaries

- Keep consensus callers behind `core/crypto` signature interfaces.
- Keep routing callers behind `core/crypto` KEM interfaces.
- Do not rewrite consensus, routing, or bundle subsystems unless a narrow adapter or metadata change requires it.
- Preserve the seven-validator local demo and 5-of-7 quorum behavior unless the current phase explicitly changes consensus.
- Phase 4 consensus now has explicit height/round/stage records, deterministic proposer rotation, prevote/precommit certificates, conservative lock state, and deterministic state digests. Future work should extend those seams instead of replacing the prototype with an external BFT framework.
- Do not describe the Phase 4 consensus tests as production BFT safety, formal verification, or a complete fault-evidence system.
- Phase 5 adds `consensus/health`, `consensus/fault`, and `cmd/fault-demo` as a local evidence harness. Keep it private-testbed/local-prototype only and do not turn it into production orchestration without an explicit phase request.
- The fault evidence uses logical ticks for deterministic tests. Do not convert those values into external performance claims.
- Message-preservation evidence means accepted application-level transaction IDs are committed once or deduplicated on replay. Do not describe it as transport-level zero packet loss.
- Routing testbed work lives under `routing/*` and `cmd/routing-demo`. Keep it private-testbed only: local relay allowlists, local test services, no public relay discovery, no public exits, no censorship-resistance positioning, and no production anonymity or privacy claims.
- Routing code must consume `core/crypto/suite` KEM interfaces. Do not hard-code concrete KEM implementations into relay or circuit callers.
- The SOCKS5 surface is restricted to local CONNECT flows with explicit exit-policy validation.
- Bundle protocol work lives under `bundle/*` and `cmd/bundle-demo`. Keep it local-prototype only: deterministic virtual channels, custody/quorum evidence, retransmission, reconciliation, and mock AI request/response shapes. Do not call external AI APIs.
- Bundle custody may reuse consensus vote/quorum primitives as evidence, but do not change consensus quorum behavior or present that as production BFT safety.
- AI context channels are model-context scaffolding only. Do not describe them as production AI infrastructure, production data sovereignty, live model integration, or guaranteed reliability.
- Anchor work lives under `core/anchors`, `contracts/client`, `contracts/polygon`, and `cmd/anchor-demo`. Keep Polygon/EVM details behind the anchor interface. Local tests should use the mock backend.
- Do not make on-chain lookup required for consensus startup unless a future phase explicitly requests it.
- PQ signature and quorum-certificate validation remain off-chain. Contracts anchor hashes and metadata only.
- Do not commit private keys, API keys, secret RPC URLs, Foundry broadcast artifacts, or generated deployment outputs.
- Evidence API hardening uses scoped hashed API-key records. Do not commit real `api-keys*.json` files; only the disabled example template belongs in source.
- Do not describe the contracts as audited or production smart-contract security.
- Deployment path work lives under `Dockerfile`, `docker-compose.yml`, `config/`, `deployments/k8s`, `deployments/terraform`, `cmd/deployment-evidence`, and deployment docs. Keep it a safe local/prototype handoff path unless a future phase explicitly requests production hardening.
- Do not run `terraform apply`, create cloud resources, push images to registries, use live RPC endpoints, expose public relay exits, or require real kubeconfigs/cloud credentials in local validation.
- Prefer `make compose-config`, `make k8s-validate`, `make terraform-validate`, and `make deployment-evidence` for validation. Optional tools should skip clearly when absent.
- Keep real `.env`, `.tfvars`, kubeconfigs, wallet files, private keys, Terraform state, Foundry broadcast artifacts, generated data, and generated evidence out of source control.
- Final handoff work lives in `internal/e2e`, `cmd/e2e-demo`, `cmd/e2e-evidence`, final docs, and packaging scripts. Do not add new protocol ambitions under the final handoff layer.
- `make final-verify` is the strongest safe local validation target. It must not require live cloud, live Polygon, public network access, public routing exits, or external AI APIs.
- `make package-handoff` and `make package-evidence` write archives under `dist/`, which is ignored. Inspect packages before sharing.
- Keep package changes small enough that future phases can review them by subsystem.
- Keep the local demo default on memory storage unless the user explicitly asks for durable demo state.
- For validator daemon durability, prefer `--storage durable --data-dir ./data/<validator-id>` or the equivalent `STORAGE` and `DATA_DIR` environment variables.

## Crypto warnings

- The default local suite is `dev`.
- The `dev` suite is development-only and uses Ed25519/X25519 placeholders. It is not post-quantum secure.
- The `pq` suite provides implementation-backed ML-KEM-768 and ML-DSA-87 adapters for engineering validation.
- Deterministic key helpers are for tests and local prototypes only.
- Do not claim FIPS 140-3 certification, ACVTS certification, or production post-quantum security based on this repo.

## Routing warning

The onion-routing work is private-testbed only. Do not describe it as production-ready anonymity, do not add a public exit path without explicit authorization/policy work, and do not imply abuse controls exist before they are implemented.
