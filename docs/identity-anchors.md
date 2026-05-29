# Identity Anchors

The identity anchor layer maps local pq-fabric validator identity metadata into deterministic anchor records. It is a local prototype boundary and does not claim FIPS certification, ACVTS validation, production post-quantum security, production smart-contract security, production BFT safety, or production anonymity.

## Anchor Record

A validator identity anchor contains:

- validator ID,
- region,
- signature algorithm,
- signature public-key fingerprint,
- KEM algorithm,
- KEM public-key fingerprint,
- metadata URI,
- metadata hash.

`core/anchors.IdentityRecordFromValidator` converts `core/identity.ValidatorIdentity` into this record. Fingerprints are deterministic hashes over algorithm name and public key bytes. The metadata hash is derived from canonical local metadata for repeatable tests.

## Mock Backend

`core/anchors.MockBackend` implements the anchor client interface without network or blockchain calls. It supports explicit local roles:

- `identity_admin`
- `credential_issuer`
- `governance_admin`
- `qc_anchorer`

Duplicate identity registration reverts with a duplicate error. Identity updates are explicit authorized operations.

## Mismatch Detection

`core/anchors.CompareIdentityToValidator` compares an anchor record with local validator metadata and reports mismatches. This is used by tests and the anchor demo. Consensus startup does not require on-chain identity lookup in this phase.

## Evidence

Run:

```bash
make anchor-demo
make anchor-evidence
```

Artifacts:

```text
tmp/anchor-evidence.json
tmp/anchor-evidence.txt
```

The evidence includes all seven validator identity anchors, a mismatch test outcome, and the on-chain/off-chain boundary statement.
