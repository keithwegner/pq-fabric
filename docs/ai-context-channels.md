# AI Context Virtual Channels

The AI context layer is a local/mock interface scaffold. It does not call external AI APIs and does not claim production AI correctness, production data sovereignty, live model integration, production transport reliability, FIPS certification, ACVTS validation, or production anonymity.

## Channel Mapping

- `conversation`: user/assistant transcript items.
- `working_memory`: durable intermediate facts, notes, and planning state.
- `execution`: tool calls, code execution requests, command results, and task actions.
- `retrieval`: retrieved documents, search results, embedding metadata, and external context references.

Each context item has an ID, channel type, priority, size estimate, sequence number, digest, content, and logical creation tick.

## Context Window

The manager enforces a shared byte budget across channels and deterministic per-channel policies. Execution has the highest default priority. Retrieval has lower priority and can be evicted first under total-budget pressure. Working memory can be snapshotted through the existing storage snapshot interface for restart tests.

Context frame assembly is deterministic: the same retained channel items produce the same frame and digest. Relevant context changes alter the digest.

## Mock OpenAI-Compatible Shape

`bundle/ai_context` defines local chat/completions-style request and response structs:

- `ChatCompletionRequest`
- `ChatMessage`
- `ChatCompletionResponse`
- `ChatChoice`

`MockProvider` returns deterministic local text derived from the request digest. It requires no API key, performs no network request, and has no live model dependency. The bundle demo records both request and response digests as evidence.

## Evidence Boundary

The demo proves local scheduling, envelope creation, custody evidence, retransmission deduplication, reconciliation, and mock request/response recording. It does not prove model quality, production data controls, production custody, or reliable delivery across real networks.
