# Terraform Deployment Scaffold

The Terraform files under `deployments/terraform` are validation-only
scaffolding for future multi-region planning. They do not create cloud
resources, configure a provider, configure remote state, or represent a live
deployment.

## Layout

```text
deployments/terraform/providers.tf
deployments/terraform/main.tf
deployments/terraform/variables.tf
deployments/terraform/outputs.tf
deployments/terraform/modules/region/
deployments/terraform/examples/
```

The current scaffold models three conceptual regions:

- `nyc`
- `london`
- `singapore`

It models seven validators, seven private-testbed relays, the 5-of-7 quorum
threshold, a deployment profile label, and expected Kubernetes Secret reference
names. No real account IDs, credentials, private subnets, or provider resources
are hard-coded.

Secret-manager details remain outside Terraform in this slice. The expected
secret names and keys are documented under `deployments/secrets/`, and
`cmd/pilot-bootstrap validate` checks the provider-neutral contract before any
future cluster-specific adaptation.

## Safe Validation

```bash
make terraform-validate
```

This runs:

```bash
terraform -chdir=deployments/terraform init -backend=false
terraform -chdir=deployments/terraform validate
```

Do not run `terraform apply` for this scaffold. If a future operator chooses to
adapt it to a real provider, that work needs explicit review, provider
configuration, remote-state design, secret-manager integration, backup design,
network approval, and deployment approval.

## Example Variables

Example files live under:

```text
deployments/terraform/examples/
```

They are placeholders only. Real `.tfvars` and `.auto.tfvars` files are ignored by Git and must not be committed.

The `production-pilot` example intentionally keeps `remote_state_configured =
false` because this slice only records the expected readiness contract. It does
not create or select a backend.

## Non-Claims

This Terraform scaffold is not production infrastructure, a cloud deployment,
managed backup, secret-manager integration, production BFT safety, production
anonymity, FIPS certification, ACVTS validation, or audited smart-contract
security.
