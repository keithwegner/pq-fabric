# Crypto validation

This repo includes a compact ACVTS-style vector harness for the Phase 1 post-quantum adapters. It is intended for repeatable engineering validation only. Passing these tests is not FIPS 140-3 certification, ACVTS validation, or a production security claim.

## Scope

Current vector coverage:

- ML-KEM-768 deterministic key derivation, deterministic encapsulation, decapsulation, shared-secret agreement, malformed ciphertext rejection, and tampered ciphertext mismatch checks.
- ML-DSA-87 deterministic key derivation, deterministic signature generation, signature verification, tampered signature rejection, and wrong-message rejection.

The fixture format is intentionally small so more ACVTS-style cases can be added without changing adapter callers.

## Fixture Layout

Fixtures live under:

```text
tests/crypto_vectors/mlkem768/
tests/crypto_vectors/mldsa87/
```

Each JSON file contains:

- `metadata.algorithm`
- `metadata.parameter_set`
- `metadata.fixture_source`
- `metadata.fixture_date`
- `metadata.expected_result_count`
- `metadata.pass_fail_summary`
- `cases[]` with a stable `case_id`

The current fixtures store deterministic inputs plus expected output hashes or shared-secret bytes. Test failures include the `case_id` so a failing vector can be isolated quickly.

## Commands

Run all crypto vectors:

```bash
make crypto-vectors
```

Run one algorithm:

```bash
make crypto-vectors-mlkem
make crypto-vectors-mldsa
```

Run the ordinary local validation suite:

```bash
make verify
```

## Adding Fixtures

When adding fixtures:

- Keep one algorithm and parameter set per JSON file.
- Use stable `case_id` values.
- Update `expected_result_count` whenever cases are added or removed.
- Prefer deterministic seeds and deterministic adapter paths where the implementation supports them.
- Keep generated pass/fail checks in the focused adapter vector tests.
- Do not describe these fixtures as certification evidence.

Future Phase 2 extensions can add parsers for raw ACVTS prompt/expected-result JSON files under this same directory structure.
