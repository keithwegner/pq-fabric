# Routing Testbed

The routing layer is a private local onion-routing testbed. It is not a public anonymity network, production privacy system, censorship-resistance system, public relay network, FIPS-certified system, ACVTS-validated system, or production post-quantum-secure deployment.

## Boundary

The testbed is intentionally limited to:

- named local relays,
- deterministic local tests,
- explicit three-hop paths,
- local-only test destinations,
- generated evidence artifacts.

It does not implement public relay discovery, public exits, traffic obfuscation, abuse controls, or production anonymity guarantees.

## Seven-Relay Topology

The local topology has seven relays:

| Relay | Region |
|---|---|
| `relay-1` | `nyc` |
| `relay-2` | `nyc` |
| `relay-3` | `london` |
| `relay-4` | `london` |
| `relay-5` | `singapore` |
| `relay-6` | `singapore` |
| `relay-7` | `singapore` |

The default demo path is:

```text
entry  = relay-1 / nyc
middle = relay-4 / london
exit   = relay-7 / singapore
```

## Telescoping Construction

The client creates a circuit by sending extension messages for each hop:

1. Establish a KEM-derived layer key with the entry relay.
2. Extend the circuit to the middle relay.
3. Extend the circuit to the exit relay.

Each extension message contains a circuit ID, relay ID, role, KEM ciphertext, nonce, and proof. Relays decapsulate through the configured crypto suite, verify the proof, and reject malformed material, wrong keys, or replayed extension messages.

The default suite is `dev`; set `PQ_FABRIC_CRYPTO_SUITE=pq` to exercise the implementation-backed ML-KEM-768 adapter where available. Passing these tests is engineering evidence, not certification.

## Layered Cells

Onion cells include:

- circuit ID,
- stream ID,
- payload type,
- sequence number,
- relay ID for the current layer,
- nonce,
- encrypted payload bytes,
- authentication tag.

The client wraps data in exit, middle, then entry layers. Entry and middle relays remove only their own layer and forward the remaining encrypted bytes. The exit relay removes the final layer and forwards the plaintext to an allowed local destination.

Response wrapping is modeled with the same per-hop keys on the reverse path for this prototype.

## Relay Visibility

Entry relay knows:

- immediate client/predecessor connection,
- next hop,
- local circuit state and entry layer key.

Middle relay knows:

- previous hop,
- next hop,
- local circuit state and middle layer key.

Exit relay knows:

- previous hop,
- allowed final destination,
- plaintext payload at the exit boundary,
- local circuit state and exit layer key.

No relay stores the full path in local session state.

## SOCKS5 Proxy

The local SOCKS5 proxy supports restricted CONNECT requests. Unsupported commands are rejected. Destinations must pass the exit policy before a CONNECT succeeds.

The demo uses local test destinations only:

```text
local-echo:7000
local-http:8080
```

## Stream Multiplexing

Logical stream IDs are unique per circuit. Multiple streams can use one circuit. Duplicate stream IDs and unknown stream IDs fail safely. Closing one stream does not tear down the entire circuit.

## Commands

Run focused routing tests:

```bash
make routing-tests
```

Run the readable demo:

```bash
make routing-demo
```

Regenerate evidence:

```bash
make routing-evidence
```

Artifacts:

```text
tmp/routing-evidence.json
tmp/routing-evidence.txt
```

## Evidence Fields

The JSON evidence includes:

- circuit ID,
- selected entry/middle/exit relay IDs,
- selected crypto suite,
- handshake success,
- streams opened and completed,
- rejected destination count,
- malformed handshake rejection count,
- SOCKS5 round-trip result,
- relay visibility summary,
- limitations statement.

## Remaining Limitations

- The relay runtime is local and in-process.
- SOCKS5 handling is intentionally small and restricted to controlled CONNECT flows.
- Exit policy is an allowlist for local test services only.
- There is no public relay discovery.
- There are no public exits.
- There is no traffic padding or metadata-resistance validation.
- There is no production anonymity, production privacy, or censorship-resistance claim.
