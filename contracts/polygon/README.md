# Polygon Anchor Contracts

This directory is a self-contained Foundry project for pq-fabric anchor contracts. The contracts are Polygon/EVM-compatible demonstrators for storing hashes, metadata, and references. They do not verify ML-DSA, ML-KEM, post-quantum signatures, quorum membership, or consensus safety on-chain.

## Structure

```text
foundry.toml
src/
test/
script/
```

## Contracts

- `IdentityAnchorRegistry`: validator identity metadata and key fingerprints.
- `CredentialAnchorRegistry`: credential hash anchors linked to known validator identities.
- `GovernanceAnchorRegistry`: governance proposal hashes and simple lifecycle state.
- `QuorumCertificateAnchorRegistry`: quorum-certificate hash anchors with height, round, block/event hash, threshold, and signer-count metadata.

All duplicate artifact anchors revert. Identity updates are explicit and authorized. Access control is intentionally minimal: the deployer or configured owner is authorized and can grant or revoke additional authorized accounts.

## Tests

Run from this directory when Foundry is installed:

```bash
forge test
```

The repository Makefile wraps this as:

```bash
make contract-tests
```

If `forge` is not installed, the Makefile target prints a clear skip message so local Go verification is not blocked.

## Optional Testnet Deployment

Deployment scripts live under `script/`. They read the optional `PQ_FABRIC_ANCHOR_OWNER` environment variable for the initial owner. RPC URLs, private keys, and broadcast configuration must be supplied through the ordinary Foundry CLI/environment, not committed to this repository.

Example shape:

```bash
cd contracts/polygon
PQ_FABRIC_ANCHOR_OWNER=0x0000000000000000000000000000000000000001 \
  forge script script/DeployAllAnchors.s.sol:DeployAllAnchors \
  --rpc-url "$POLYGON_AMOY_RPC_URL" \
  --broadcast
```

Testnet deployment is optional and outside the normal local verification path. Do not commit generated broadcast artifacts containing sensitive data.

## Boundary

On-chain records are evidence anchors only. pq-fabric validators remain responsible for off-chain identity/key validation, post-quantum signature validation, quorum-certificate validation, consensus safety, routing policy, and bundle custody semantics. These contracts are not audited and are not production smart-contract security claims.
