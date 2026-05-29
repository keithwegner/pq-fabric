# Polygon Contracts

`contracts/polygon` is a self-contained Foundry project for Polygon/EVM-compatible anchor contracts. The contracts store hashes, metadata, and references only. They do not verify ML-DSA signatures, ML-KEM material, post-quantum validator signatures, quorum membership, consensus safety, bundle custody correctness, or routing privacy.

## Contracts

- `IdentityAnchorRegistry`: validator identity metadata and key fingerprints.
- `CredentialAnchorRegistry`: credential hash anchors associated with subject and issuer identities.
- `GovernanceAnchorRegistry`: governance proposal hash anchors and simple lifecycle state.
- `QuorumCertificateAnchorRegistry`: QC hash anchors with height, round, block hash or event hash, threshold, and signer count.

Each contract uses minimal self-contained access control. The initial owner is authorized and can authorize additional accounts. No OpenZeppelin dependency is used.

## Duplicate Policy

Duplicate artifact anchors revert:

- duplicate identity registration reverts,
- duplicate credential hash reverts,
- duplicate governance proposal hash reverts,
- duplicate QC hash reverts.

Identity updates use a separate authorized update call.

## Tests

Run:

```bash
make contract-tests
```

If Foundry is installed, this runs `forge test` under `contracts/polygon`. If Foundry is absent, the target prints a clear skip message and returns success so ordinary Go validation is not blocked.

The checked-in tests cover registration, lookup, updates, unauthorized calls, malformed inputs, duplicate/replay behavior, QC metadata, and emitted events.

## Go Anchor Interface

Go code consumes `core/anchors.Client`, not Polygon-specific types. The interface covers:

- `RegisterIdentity`
- `GetIdentity`
- `AnchorCredential`
- `GetCredential`
- `AnchorGovernanceProposal`
- `GetGovernanceProposal`
- `AnchorQuorumCertificate`
- `GetQuorumCertificateAnchor`
- `Status`

`contracts/client` is an optional Polygon client boundary placeholder. It reads configuration from environment variables and fails safely when not configured. Generated ABI bindings and live RPC calls are intentionally not required for local tests.

## Optional Deployment

Deployment scripts live under `contracts/polygon/script`. They read `PQ_FABRIC_ANCHOR_OWNER` for the initial owner. RPC URLs and private keys must be supplied through local Foundry environment variables or CLI flags and must not be committed.

No live deployment is required for local verification.

## Non-Claims

These contracts are not audited. They are not production smart-contract security, production governance safety, production BFT safety, production post-quantum security, FIPS certification, ACVTS validation, or production anonymity.
