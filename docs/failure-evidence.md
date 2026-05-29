# Failure Evidence

Phase 5 adds a deterministic local fault-evidence harness for the seven-validator prototype. It is private-testbed engineering evidence only. It is not production fault tolerance, production BFT safety, production self-healing infrastructure, production anonymity, FIPS certification, ACVTS certification, or a zero-packet-loss guarantee.

## Health Model

Validator health records include:

- validator ID,
- region,
- current height,
- current round/view,
- latest committed block hash,
- latest state digest,
- last heartbeat logical tick,
- status.

Supported statuses:

- `healthy`
- `degraded`
- `suspected_failed`
- `failed`
- `recovering`
- `recovered`

## Heartbeats And Detection

The local harness emits heartbeat records from validator snapshots. Each heartbeat carries validator ID, height, round/view, committed hash, state digest, and a logical tick.

The detector uses configurable logical tick thresholds:

- missed suspect threshold -> `suspected_failed`
- missed failure threshold -> `failed`
- lag behind reference height -> `degraded`
- inconsistent hash or state digest at the same height -> `failed`
- caught-up heartbeat matching reference height/hash/digest -> `recovered`

Tests use logical ticks so failure detection does not depend on arbitrary wall-clock sleeps.

## Remediation Controller

The Phase 5 remediation controller lives in `consensus/fault`. It is an in-process local harness that can:

- stop simulated validators,
- record failure evidence,
- mark validators as `recovering`,
- restart validators with isolated durable state directories,
- request catch-up from healthy peers,
- validate committed quorum certificates while applying missing blocks,
- confirm all validators converge to the same height/hash/state digest.

This is not a production orchestrator.

## Catch-Up Behavior

Recovered validators reload durable state when the fault demo runs with durable storage. They then request committed blocks from peers, validate hash-chain continuity and quorum certificates, apply blocks in order, and preserve idempotent transaction behavior.

The recovered validator must converge to the same:

- committed height,
- latest block hash,
- state digest.

## Message Preservation

Message preservation in this prototype means application-level transaction IDs are accounted for during a failure window:

- submitted transaction count,
- committed unique transaction count,
- duplicate/replayed transaction count,
- pending/retried transaction count,
- final state digest,
- final convergence result.

It does not claim transport-level zero packet loss.

## Run The Fault Demo

```bash
make fault-demo
```

Regenerate evidence artifacts:

```bash
make failure-evidence
```

Artifacts:

```text
tmp/failure-evidence.json
tmp/failure-evidence.txt
```

The readable scenario:

1. Start seven validators.
2. Commit normally.
3. Fail validator-6 and validator-7.
4. Detect suspected and failed status through missed heartbeats.
5. Continue committing with 5-of-7 quorum.
6. Submit messages during the failure window.
7. Restart failed validators.
8. Catch up failed validators from committed peers.
9. Verify final convergence.
10. Emit structured evidence.

## Evidence Fields

The JSON report includes:

- timing mode,
- validator count,
- failed validator count,
- quorum threshold,
- commit count during failure,
- detection latency in logical ticks,
- remediation latency in logical ticks,
- recovery/catch-up latency in logical ticks,
- total scenario duration in logical ticks,
- transaction/message preservation metrics,
- final height/hash/state digest,
- final convergence result,
- structured event records.

Each event record includes event type, validator ID where applicable, observed status, previous status, height, round/view, block hash, state digest, logical tick, optional timestamp, and reason.

## Remaining Limitations

- The harness is local and in-process.
- Heartbeat thresholds are logical test parameters, not production SLOs.
- Restart behavior is simulated through local validator objects and durable data directories.
- There is no production scheduler, cloud remediation system, or operator approval workflow.
- There are no slashing rules or formal fault proofs.
- Consensus remains a first-principles local prototype.
- Routing remains private-testbed only.
