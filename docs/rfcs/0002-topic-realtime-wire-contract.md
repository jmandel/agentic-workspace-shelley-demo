# RFC 0002: Topic Realtime Wire Contract

- Status: draft
- Date: 2026-03-10

## Summary

The draft spec defines topic ACP endpoints, but not the actual realtime wire
contract used by clients and runtimes for topic participation.

This RFC proposes a canonical topic realtime contract that:
- stays close to the Bun reference implementation's message vocabulary
- adds explicit prompt lifecycle events
- adds ordered event metadata
- requires late-join and reconnect support

## Context

Today the strongest "spec by example" is the Bun reference implementation:
- `prompt` from client
- `connected`, `text`, `tool_call`, `tool_update`, `done`, `error`, `system`
  from server

That is useful but incomplete:
- it does not define reconnect or replay
- it uses ad hoc `system` text for important state like queueing and thinking
- it has no explicit approval messages
- the checked-in Bun `test.ts` is stale relative to `wmlet.ts`, which is a sign
  that the contract is not pinned tightly enough

Shelley has already run into these gaps:
- prompt queueing needed an explicit protocol decision
- approval-required tools needed websocket request/response messages
- replay/catchup questions showed up immediately when comparing Shelley SSE with
  the minimal Bun websocket

## Decision

### Topic realtime endpoints

This RFC does not change the draft's endpoint model. It only defines what flows
across the topic realtime connection once established.

### Client to server messages

The minimal canonical client message set is:

```json
{ "type": "prompt", "promptId": "p_123", "data": "your message here" }
{ "type": "approval_response", "approvalId": "a_123", "approved": true, "approver": "alice@acme.com" }
```

`promptId` should be client-generated or server-assigned, but it must become a
stable handle for prompt lifecycle events.

### Server to client messages

The canonical server message set is:

```json
{ "type": "connected", "topic": "debug", "sessionId": "...", "protocolVersion": "v1" }
{ "type": "prompt_status", "promptId": "p_123", "status": "accepted" }
{ "type": "prompt_status", "promptId": "p_123", "status": "queued" }
{ "type": "prompt_status", "promptId": "p_123", "status": "started" }
{ "type": "text", "data": "response chunk" }
{ "type": "tool_call", "toolCallId": "...", "title": "Read", "status": "pending" }
{ "type": "tool_update", "toolCallId": "...", "status": "completed" }
{ "type": "approval_request", "approvalId": "a_123", "tool": "github", "action": "repo.push" }
{ "type": "done" }
{ "type": "error", "data": "error message" }
{ "type": "system", "data": "human-readable informational text" }
```

Notes:
- `system` remains allowed, but it is no longer the only carrier for meaningful
  state transitions like queueing
- `approval_request` is promoted to a first-class message
- `done` terminates one prompt turn, not the websocket session

### Event envelope

All server-originated realtime messages except `connected` should carry:
- `eventId`
- `timestamp`

This gives clients a stable ordering handle and creates a foundation for replay.

### Replay and reconnect

The protocol should support late joiners and reconnecting clients.

This RFC pins one requirement but leaves room for implementation choice:
- a topic realtime connection must allow a client to ask for events after a
  known event ID

The preferred shape is a connect-time parameter such as `since=<eventId>`, with
replayed history delivered as normal ordered events before the live tail.

This avoids putting transcript history into the `connected` payload itself.

## Consequences

Benefits:
- keeps the simple reference-impl message vocabulary where it already works
- makes queueing and approval semantics explicit
- gives runtimes a path to reconnect and catchup behavior
- reduces drift between implementations and tests

Costs:
- requires runtimes to retain ordered event history, not just live fanout
- adds more message types than the current Bun reference implementation

## Open Questions

- Should prompt status use a dedicated `prompt_status` message as proposed here,
  or should prompt lifecycle be folded into a more general event envelope?
- Is `since=<eventId>` the right replay mechanism, or should the protocol
  define a separate history bootstrap call?
- Should `tool_update` remain status-only, or should a later RFC extend it with
  typed result payloads?
