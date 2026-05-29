# Claim Safety Review

Use this document before sharing the prototype externally. The repo is a local engineering prototype and must be described conservatively.

## Safe Language

- local prototype
- engineering validation
- deterministic test harness
- private testbed
- mock backend
- off-chain PQ verification
- Polygon-compatible anchoring
- hashes and metadata anchored on EVM-compatible contracts
- ACVTS-style vector harness
- not FIPS certified
- not ACVTS validated
- development crypto is not post-quantum secure
- deployment scaffold
- future controlled deployment planning
- local evidence artifacts

## Unsafe Language

Do not use these phrases as claims:

- FIPS certified
- ACVTS validated
- production post-quantum secure
- production anonymous
- production BFT-safe
- audited smart contracts
- zero packet loss
- public censorship-resistant onion network
- deployed across three continents
- live Polygon deployment
- production AI infrastructure
- production data sovereignty
- production self-healing infrastructure
- production cloud deployment readiness

## Required Qualifiers

- Crypto vectors are engineering evidence only.
- PQ adapters are not a module validation or certification claim.
- Consensus tests are not formal verification.
- Failure evidence is local and logical-tick based.
- Routing is private-testbed only and has no public exits.
- Bundle/AI context uses a local mock provider and no external API calls.
- Polygon contracts anchor hashes and metadata only.
- Deployment files are scaffolding and validation aids only.

## Reviewer Checklist

1. Check README and release notes for unsafe positive claims.
2. Confirm evidence artifacts are local and reproducible.
3. Confirm no secrets, wallet files, kubeconfigs, Terraform state, or raw data directories are packaged.
4. Confirm any future-facing deployment language says scaffold, planned, intended, or future work.
5. Confirm certification-related language says not certified or engineering validation only.
