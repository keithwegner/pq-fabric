# Release Artifacts

The `release-artifacts` workflow turns validated source into auditable
container artifacts without deploying anything.

## What It Produces

- Multi-architecture Linux images for `amd64` and `arm64`.
- Published images at `ghcr.io/keithwegner/pq-fabric` on `main` and `v*` tags.
- Tags `sha-<shortsha>`, `main`, and the immutable `vX.Y.Z` tag for release
  tags.
- Keyless cosign signatures using GitHub OIDC.
- Build provenance attestations, a source SBOM, image digest evidence, cosign
  verification evidence, and Trivy image-scan output.

## Required Deployment Shape

Staging and production-pilot manifests should use digest-pinned image
references, for example:

```text
ghcr.io/keithwegner/pq-fabric@sha256:<published-digest>
```

Local Compose intentionally remains on `pq-fabric:local`.

## Boundary

This workflow publishes and signs artifacts only. It does not apply Kubernetes
manifests, run Terraform apply, create cloud resources, fetch real secrets,
enable Polygon mainnet, or make certification claims.
