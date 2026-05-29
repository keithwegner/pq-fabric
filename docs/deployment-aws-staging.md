# AWS/EKS Staging Deployment

The AWS staging path is the first concrete cluster-runtime path for the
regulated pilot. It targets an already-provisioned private EKS cluster and
deploys only a signed, digest-pinned GHCR image through a manual GitHub
Actions workflow.

It does not create VPCs, EKS clusters, load balancers, public ingress,
Terraform resources, Polygon resources, or certification evidence.

## Prerequisites

- Private EKS cluster reachable from GitHub Actions through AWS OIDC.
- External Secrets Operator installed in the cluster.
- `ClusterSecretStore/pq-fabric-staging-aws-secret-store` already configured for
  AWS Secrets Manager.
- StorageClass `pq-fabric-staging-gp3-retain` already configured for retained
  validator PVCs.
- Remote signer endpoint reachable from validator pods over HTTPS. The current
  validator uses `PQFABRIC_SIGNER_PROVIDER=cloud-kms`; AWS hosts/secrets this
  signer but is not used as a native ML-DSA KMS provider.
- Signed image digest from the `release-artifacts` workflow.

## GitHub Environment

Create GitHub environment `staging-aws` and provide:

```text
vars.AWS_ROLE_ARN
vars.AWS_REGION
vars.EKS_CLUSTER_NAME
secrets.PQFABRIC_STAGING_API_TOKEN
secrets.PQFABRIC_STAGING_ADMIN_TOKEN
```

The workflow uses OIDC with `aws-actions/configure-aws-credentials@v6.1.2`.
No static AWS access keys are required or expected.

## AWS Secrets Manager Layout

The overlay uses ExternalSecret references only. Store payloads under:

```text
pq-fabric/staging/api              api-keys.json
pq-fabric/staging/manifest         current.json, history/v1.json
pq-fabric/staging/peer-tls         ca.crt, validator-1.crt/key ... validator-7.crt/key
pq-fabric/staging/kms              token, ca.crt
pq-fabric/staging/otel             otlp-headers
```

Manifests should use `signing_key_ref` values shaped like:

```text
aws-signer://pq-fabric/staging/validator-1
```

Those refs identify keys to the remote signer endpoint; they are not native AWS
KMS asymmetric signing keys.

## Deploy Workflow

Run `.github/workflows/aws-staging-deploy.yml` manually with:

```text
image_ref=ghcr.io/keithwegner/pq-fabric@sha256:<digest>
namespace=pq-fabric
dry_run=true
```

The dry-run path verifies the cosign signature, authenticates to EKS, checks
External Secrets CRDs and store presence, renders the overlay, and performs a
server-side dry-run.

Set `dry_run=false` only after dry-run evidence is reviewed. The apply path
waits for ExternalSecrets, waits for the validator StatefulSet rollout, opens a
temporary `kubectl port-forward`, submits hash-only evidence, verifies the
receipt, exports an ops report, and uploads evidence artifacts.

Generated workflow evidence:

```text
tmp/aws-staging-render.yaml
tmp/aws-staging-cosign-verify.txt
tmp/aws-staging-deploy-summary.json
tmp/aws-staging-smoke.json
tmp/aws-staging-ops-report.json
```

## Rollback

Rollback is image-digest based. Re-run `aws-staging-deploy` with a prior signed
digest and `dry_run=true`, review the evidence, then re-run with
`dry_run=false`. Do not roll back by mutable tags.

## Validation

Local static validation:

```bash
make aws-staging-check
make k8s-validate
```

The checks validate the overlay shape, ExternalSecret references, digest-pinned
image placeholder, workflow guardrails, production-mode config, PVCs, relays
disabled, and non-claim boundaries. They do not contact AWS or Kubernetes.

## Non-Claims

This path is a staging activation path only. It does not claim production
deployment readiness, production BFT safety, production post-quantum security,
payload custody, public routing, public validator admission, Polygon mainnet,
Terraform apply, cloud resource creation, native AWS KMS ML-DSA signing, or
certification.
