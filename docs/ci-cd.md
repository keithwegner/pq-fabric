# CI/CD

`pq-fabric` uses GitHub Actions as a production-path gate. The workflows are
intended to catch regressions, security issues, and deployment-evidence drift
before code reaches `main`. They do not deploy cloud resources, run Terraform
apply, fetch real secrets, or enable public/mainnet services. Image publishing
is limited to signed GHCR artifacts from the `release-artifacts` workflow on
`main` and `v*` tags. The only cluster-deploy workflow is the manual,
environment-gated AWS staging workflow.

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
- `aws-staging-deploy`: manual `workflow_dispatch` only; requires GitHub
  environment `staging-aws`, AWS OIDC, a digest-pinned signed GHCR image, an
  existing private EKS cluster, External Secrets, and a dry-run/apply choice.

The first four workflows support PRs and pushes to `main`; `ci`,
`production-readiness`, and `security` also run on a weekly schedule.
`aws-staging-deploy` never runs automatically.

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

## Branch Protection

`main` branch protection is configured to require the checks above with strict
up-to-date status checks before merge. The rule also requires linear history and
disables force pushes and branch deletion. Admin enforcement remains a repository
owner policy setting, not a product-runtime control.

## Release Boundary

The current CD path publishes signed container artifacts but intentionally stops
before production cloud deployment, Terraform apply, Polygon mainnet, or
certification claims. AWS staging apply is a manual pilot-runtime path only.
