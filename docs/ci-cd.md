# CI/CD

`pq-fabric` uses GitHub Actions as a production-path gate. The workflows are
intended to catch regressions, security issues, and deployment-evidence drift
before code reaches `main`. They do not deploy cloud resources, run Terraform
apply, fetch real secrets, or enable public/mainnet services. Image publishing
is limited to signed GHCR artifacts from the `release-artifacts` workflow on
`main` and `v*` tags.

## Workflows

- `ci`: runs Go tests, vet, `make verify`, docs/repo hygiene, focused race
  tests, Foundry contract tests, and a macOS smoke suite.
- `production-readiness`: installs Terraform and kubectl, renders Kubernetes
  overlays, validates Terraform, runs pilot bootstrap/backup/deploy checks,
  regenerates deployment and e2e evidence, and uploads evidence artifacts.
- `security`: runs CodeQL, gitleaks secret scanning, and Trivy filesystem
  dependency scanning for high/critical library vulnerabilities.
- `release-artifacts`: builds multi-architecture images, scans the image,
  publishes to `ghcr.io/keithwegner/pq-fabric` on `main` and `v*` tags, signs
  published images with keyless cosign, emits provenance, and uploads release
  evidence artifacts.

All workflows support `workflow_dispatch`, run on PRs and pushes to `main`, and
also run on a weekly schedule to catch toolchain or advisory drift.

## Required Checks

Before treating `main` as production-path ready, require these checks to pass:

- `ci / go validation`
- `ci / race validation`
- `ci / polygon contract validation`
- `ci / macos smoke`
- `production-readiness / controlled pilot evidence`
- `security / codeql`
- `security / secret scan`
- `security / dependency scan`
- `release-artifacts / build, scan, sign, and attest`

Branch protection for `main` should require those checks before merge.

## Release Boundary

The current CD path publishes signed container artifacts but intentionally stops
before cloud deployment, Kubernetes apply, Terraform apply, Polygon mainnet, or
certification claims.
