# Local Deployment Path

Phase 9 adds a safe local deployment path for the pq-fabric prototype. It is not a production deployment path and must not be used to expose public routing, public exits, cloud resources, wallet keys, or live Polygon endpoints by default.

## Prerequisites

- Go toolchain for local demos and tests.
- Docker and Docker Compose for the containerized local testbed.
- Optional: `kubectl` and `terraform` for scaffold validation.
- Optional: Foundry `forge` for Solidity contract tests.

## Build The Local Image

```bash
make image
```

The image tag is `pq-fabric:local`. The Dockerfile builds one reusable image containing:

- `validator`
- `relay`
- `demo`
- `fault-demo`
- `routing-demo`
- `bundle-demo`
- `anchor-demo`
- `deployment-evidence`
- `pilot-deploy-check`
- `sqlite-restore-check`
- `healthcheck`

No secrets are embedded in the image.

## Validate Compose Configuration

```bash
make compose-config
```

This runs Docker Compose config rendering only. It does not start services.

## Start The Local Testbed

```bash
make compose-up
```

This starts seven validators and, through the `relays` profile, seven private-testbed relay services. Validators use durable local directories under:

```text
./data/validator-1
./data/validator-2
./data/validator-3
./data/validator-4
./data/validator-5
./data/validator-6
./data/validator-7
```

Validator host ports are bound to `127.0.0.1` only:

| Validator | URL |
|---|---|
| validator-1 | `http://127.0.0.1:8081` |
| validator-2 | `http://127.0.0.1:8082` |
| validator-3 | `http://127.0.0.1:8083` |
| validator-4 | `http://127.0.0.1:8084` |
| validator-5 | `http://127.0.0.1:8085` |
| validator-6 | `http://127.0.0.1:8086` |
| validator-7 | `http://127.0.0.1:8087` |

The Compose network is internal. Relays are for local private-testbed checks only and do not expose public exit routing.

## Inspect And Stop

```bash
make compose-logs
make compose-down
```

Remove generated local validator state only:

```bash
make compose-clean
```

## Local Demos And Evidence

```bash
make demo
make fault-demo
make routing-demo
make bundle-demo
make anchor-demo
make deployment-evidence
make pilot-deploy-check
```

Deployment evidence is written to:

```text
tmp/deployment-evidence.json
tmp/deployment-evidence.txt
tmp/pilot-deploy-check-production-pilot.json
tmp/sqlite-restore-check.json
tmp/release-provenance.json
```

## Smoke Test

```bash
make deploy-local-smoke
```

The smoke test validates Compose configuration, runs the local consensus demo, and regenerates deployment evidence. It does not leave long-running services active.

## Configuration

Safe templates live under:

```text
config/local/
config/examples/
```

Copy example `.env` files before editing them. Real `.env` files are ignored by Git.

## Non-Production Boundary

This path is a local prototype handoff path only. It does not claim production BFT safety, production self-healing, production anonymity, production post-quantum security, FIPS certification, ACVTS validation, audited smart-contract security, or production deployment readiness.
