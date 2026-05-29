# Byzantine Bundle Protocol Prototype

This layer is a local deterministic prototype inspired by CCSDS virtual channels and Bundle Protocol custody transfer. It is not production Bundle Protocol interoperability, production transport reliability, production BFT safety, production AI infrastructure, production data sovereignty, FIPS certification, ACVTS validation, or production anonymity.

## Components

- `bundle/protocol`: canonical bundle envelope, validation, deterministic digesting, and duplicate bundle ID indexing.
- `bundle/channel`: virtual channel policies, compression hooks, deterministic eviction, context budget enforcement, and weighted scheduling.
- `bundle/custody`: custody events confirmed by validator quorum evidence.
- `bundle/retransmit`: transaction ID ledger and sequence-numbered reconnect queue.
- `bundle/reconcile`: deterministic comparison and repair of bundle state after interruption.
- `bundle/ai_context`: AI context channel manager and local mock OpenAI-compatible request/response shape.
- `bundle/evidence` and `cmd/bundle-demo`: readable local evidence scenario and artifact generation.

## Bundle Envelope

The envelope records bundle ID, source, destination, channel ID/type, sequence number, transaction ID, logical creation/expiration ticks, priority, payload digest, payload bytes, optional previous bundle ID, custody request/status, optional quorum certificate reference, optional signature reference, and compression metadata.

The digest is computed over deterministic canonical JSON excluding the derived bundle ID. Tests prove stable digests, payload/transaction changes changing the digest, malformed bundle rejection, expiration handling, and duplicate bundle ID safety.

## Virtual Channels

The prototype defines four AI context channels:

- `conversation`
- `working_memory`
- `execution`
- `retrieval`

Each channel has a priority weight, byte/item budget, compression policy, eviction policy, sequence counter, pending queue, and delivered ledger. The shared context window enforces a total byte budget across channels. Lower-priority retrieval can be evicted before higher-priority execution when the window is constrained.

## Scheduling

The scheduler is deterministic weighted round-robin over pending channel items. Identical inputs produce identical output. Higher priority channels receive more service, equal priority channels follow stable channel ordering, and pending lower-priority channels are not starved under normal capacity.

## Compression And Eviction

No-op compression is the default. A gzip hook is available through the standard library for local tests. Compression metadata is explicit and malformed compressed data fails safely.

Eviction policies are deterministic. The default is oldest-first inside a channel; global budget pressure evicts from the lowest-priority non-empty channel first.

## Custody Transfer

Custody confirmation is represented as validator quorum evidence over a custody event digest. A custody event includes bundle ID, transaction ID, source node, custody holder, destination node, bundle digest, sequence number, logical tick, and a quorum certificate.

Confirmation requires at least five unique valid validator votes for the same custody event digest. Duplicate votes do not count. Unknown validators, invalid signatures, and votes for different bundle digests fail validation. Durable storage can persist custody confirmation through the existing idempotency and snapshot interfaces.

## Retransmission And Reconciliation

The retransmission queue tracks sequence numbers per source/destination/channel stream. Pending or unconfirmed bundles are returned for retransmission after interruption. Duplicate transaction IDs are deduplicated through the ledger, including after restart when durable storage is used.

The reconciliation layer compares latest sequence numbers, committed transaction IDs, custody status, context digest, pending bundle IDs, and bundle records. Missing bundles can be copied from a peer state, duplicates are ignored, and digest conflicts are reported instead of silently overwritten.

## Commands

```bash
make bundle-tests
make bundle-demo
make bundle-evidence
```

Artifacts:

```text
tmp/bundle-evidence.json
tmp/bundle-evidence.txt
```

The evidence includes channel definitions, scheduling decisions, bundle IDs, sequence numbers, transaction IDs, custody events, quorum size, retransmission count, duplicate transaction count, reconciled bundle count, final state digest, mock AI request digest, and mock AI response digest.
