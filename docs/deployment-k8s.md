# Kubernetes Deployment Scaffold

Kubernetes manifests now cover local scaffolding plus controlled staging and
production-pilot readiness overlays. They are still scaffolding only: they are
not a live cluster deployment, do not include real secrets, and are not expected
to be applied during normal local verification.

## Layout

```text
deployments/k8s/base/namespace.yaml
deployments/k8s/base/configmap.yaml
deployments/k8s/base/validator-statefulset.yaml
deployments/k8s/base/validator-service.yaml
deployments/k8s/base/relay-statefulset.yaml
deployments/k8s/base/relay-service.yaml
deployments/k8s/base/networkpolicy.yaml
deployments/k8s/base/kustomization.yaml
deployments/k8s/overlays/local/
deployments/k8s/overlays/three-region-simulation/
deployments/k8s/overlays/staging/
deployments/k8s/overlays/production-pilot/
deployments/k8s/overlays/aws-staging/
deployments/secrets/
```

## Validate Locally

```bash
make k8s-validate
```

The target uses `kubectl kustomize` for the base, staging, production-pilot,
and AWS staging overlays when `kubectl` is installed. It does not require a
live cluster and does not create resources.

Run the broader deployment readiness check with:

```bash
make pilot-deploy-check
make aws-staging-check
```

That check validates the local and production-pilot profile contracts, checks
overlay source files, runs the pilot bootstrap validation/smoke path, runs a
local SQLite migration/backup/restore verification, and collects dry-run
release provenance.

## Model

- Seven validators run as a StatefulSet with stable pod ordinals.
- Seven relays run as a private-testbed StatefulSet.
- Validator data uses persistent volume claim templates.
- Non-secret runtime values are stored in a ConfigMap.
- No public LoadBalancer service is defined.
- A NetworkPolicy limits pod traffic to the namespace plus DNS.
- The production-pilot overlay disables relays, requires HTTPS peer URLs,
  mounts API-key, manifest/history, per-validator peer TLS, and KMS CA secrets,
  and uses SQLite under the validator data volume.
- Staging, production-pilot, and AWS staging overlays use a digest-pinned
  `ghcr.io/keithwegner/pq-fabric` placeholder. Replace the placeholder digest
  with the digest emitted by the `release-artifacts` workflow before any real
  pilot rollout.
- The AWS staging overlay adds ExternalSecret references for
  `ClusterSecretStore/pq-fabric-staging-aws-secret-store`, expects AWS Secrets
  Manager remote keys under `pq-fabric/staging/*`, uses storage class
  `pq-fabric-staging-gp3-retain`, and keeps the remote `cloud-kms` signer
  endpoint contract rather than native AWS KMS ML-DSA signing.
- Each validator pod derives `NODE_ID` from its StatefulSet ordinal and uses
  `/etc/pq-fabric/tls/${NODE_ID}.crt` plus `${NODE_ID}.key`; the peer
  certificate must contain URI SAN
  `spiffe://<consortium_id>/validator/<validator_id>`.
- The production-pilot overlay sets `PQFABRIC_OPS_LISTEN_ADDR=:8090` and uses
  `/livez` and `/readyz` for Kubernetes probes. `/metrics`, `/ready`, and
  internal consensus endpoints remain on the peer-mTLS validator surface.
- The production-pilot overlay adds non-root security context, a read-only root
  filesystem, resource requests/limits, and a storage-class placeholder.

## Secrets

No Secret manifest with real data is committed. The provider-neutral secret
contract lives in `deployments/secrets/README.md` and
`deployments/secrets/secret-contract.example.yaml`. External-secret reference
shape is documented in
`deployments/secrets/external-secret-contract.example.yaml`; it includes the
expected `ClusterSecretStore` reference and remote keys without secret values.
AWS staging references are documented in
`deployments/secrets/aws-staging-external-secret-contract.example.yaml` and
`docs/deployment-aws-staging.md`.
`config/examples/pilot-bootstrap.example.yaml` is the bootstrap validation spec.
`pilot-bootstrap validate` records redacted secret-source evidence for expected
mount paths, resolved/unresolved state, content class, and safe fingerprints for
public material only. Do not commit private keys, wallet files, kubeconfigs,
cloud credentials, RPC URLs with secrets, or generated deployment output.

## Required Future Work Before Cluster Use

- Choose and review a real cluster provider.
- Define storage classes and backup behavior.
- Bind the External Secrets contract to the selected provider's reviewed
  `ClusterSecretStore` or `SecretStore` in a real cluster.
- Enforce provenance/signature policy in the target cluster admission path.
- Require signed, digest-pinned GHCR images before applying manifests in a real
  cluster.
- Wire readiness, metrics, OpenTelemetry, and alert templates into the target
  cluster's managed observability stack.
- Add operator approval gates.
- Review network policy for the target cluster CNI.

## Non-Claims

These manifests do not establish production BFT safety, production anonymity,
production post-quantum security, production smart-contract security, FIPS
certification, ACVTS validation, cloud deployment, or cluster deployment.
