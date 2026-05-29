# Terraform Deployment Scaffold

This directory is a Phase 9 planning scaffold for a future controlled multi-region deployment. It is not a production deployment, does not create cloud resources by itself, and must not be applied as part of local verification.

The scaffold models three conceptual regions:

- `nyc`
- `london`
- `singapore`

It also models seven validators, seven private-testbed relays, and a 5-of-7 quorum threshold. The current files intentionally use Terraform locals and outputs only, so `terraform validate` can run without downloading a cloud provider or requiring credentials.

## Safe Validation

```bash
terraform -chdir=deployments/terraform init -backend=false
terraform -chdir=deployments/terraform validate
```

Do not run `terraform apply` for Phase 9. Future work must add provider configuration, remote state, secret management, network design, and operator review before any real deployment.

## Secrets

Do not commit credentials, private keys, wallet keys, kubeconfigs, `.tfvars`, `.auto.tfvars`, `.terraform/`, or state files. Example variable files under `examples/` use placeholder values only.
