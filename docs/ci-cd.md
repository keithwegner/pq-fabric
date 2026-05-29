# CI/CD

`pq-fabric` uses GitHub Actions as a production-path gate. The workflows are
intended to catch regressions, security issues, and deployment-evidence drift
before code reaches `main`. They do not deploy cloud resources, run Terraform
apply, publish images, fetch real secrets, or enable public/mainnet services.

## Workflows

- `ci`: runs Go tests, vet, `make verify`, docs/repo hygiene, focused race
  tests, Foundry contract tests, and a macOS smoke suite.
- `production-readiness`: installs Terraform and kubectl, renders Kubernetes
  overlays, validates Terraform, runs pilot bootstrap/backup/deploy checks,
  regenerates deployment and e2e evidence, and uploads evidence artifacts.
- `security`: runs CodeQL, gitleaks secret scanning, and Trivy filesystem
  dependency scanning for high/critical library vulnerabilities.

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

Branch protection or a repository ruleset should require those checks before
merge once the repo is managed through pull requests.

## Release Boundary

The current CD path is evidence-only. It produces deployment and release
provenance artifacts, but intentionally stops before registry publishing,
signing, cloud deployment, Kubernetes apply, Polygon mainnet, or certification
claims.
