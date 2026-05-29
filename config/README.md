# Configuration Templates

These files are safe templates for local Phase 9 deployment paths. They contain no secrets, private keys, wallet keys, or real RPC endpoints.

Use `config/local/` for local topology and subsystem defaults. Use `config/examples/*.example.env` as copy-only templates:

```bash
cp config/examples/local-dev.example.env .env.local
```

`config/examples/api-keys.example.json` is a disabled placeholder shape for
scoped API-key records; it is intentionally not usable as a live production-mode
key file. Generate real token hashes with:

```bash
go run ./cmd/pqfabric auth hash-token --token <operator-generated-token>
```

Generate a consortium manifest for a controlled pilot with:

```bash
go run ./cmd/pqfabric manifest generate --suite pq --out config/local/consortium.manifest.json
go run ./cmd/pqfabric manifest verify --manifest config/local/consortium.manifest.json --history config/local/consortium.manifest.json
```

`config/examples/consortium.manifest.example.json` shows the shape only. It
contains placeholders and must not be used as a live manifest.

Production-mode validator runs also need a SQLite database URL and peer mTLS
cert/key/CA files. Keep those files under ignored local paths or a secret
manager; do not commit live certificates, private keys, or database files.

`config/examples/pilot-bootstrap.example.yaml` is the provider-neutral
production-pilot bootstrap contract. It validates External Secrets store refs,
expected mount paths, and resolved mounted material when supplied, while
redacting raw token and private-key values from reports.

Real `.env`, API-key files, `.tfvars`, wallet, key, kubeconfig, and generated deployment files are ignored by Git and must not be committed.
