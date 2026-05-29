# Secret Contract

This directory documents the provider-neutral secret contract for the staging
and production-pilot Kubernetes overlays. Do not place real secrets here.

The overlays expect these Kubernetes Secret names:

- `pq-fabric-api-keys`: `api-keys.json`
- `pq-fabric-consortium-manifest`: `current.json`, `v1.json`
- `pq-fabric-peer-tls`: `validator-1.crt` through `validator-7.crt`, matching `.key` files, and `ca.crt`
- `pq-fabric-kms`: `token`
- `pq-fabric-kms-ca`: `ca.crt`
- `pq-fabric-otel`: optional `otlp-headers`

The mounted validator paths are:

- `/etc/pq-fabric/secrets/api-keys.json`
- `/etc/pq-fabric/manifest/current.json`
- `/etc/pq-fabric/manifest/history/v1.json`
- `/etc/pq-fabric/tls/${NODE_ID}.crt`
- `/etc/pq-fabric/tls/${NODE_ID}.key`
- `/etc/pq-fabric/tls/ca.crt`
- `/etc/pq-fabric/kms/ca.crt`

Production-pilot validation checks the references, expected mounted paths,
External Secrets `secretStoreRef`, and resolved secret content when operators
provide a mounted file tree or Kubernetes Secret manifest. Validation reports
raw values as redacted and never prints tokens or private keys. It does not
create a secret manager, upload credentials, sign images, or deploy cloud
resources.

`deployments/secrets/external-secret-contract.example.yaml` documents the
same target Secret names through provider-neutral ExternalSecret-style
references and points them at
`ClusterSecretStore/pq-fabric-production-pilot-secret-store`. It contains
references only, not secret values.
