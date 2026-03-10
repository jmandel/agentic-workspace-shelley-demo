# RFC 0002: Topic Realtime Wire Contract

- Status: proposed
- Date: 2026-03-10

## Summary

This RFC settles the demo-ready topic realtime contract.

It defines:
- exact client and server message shapes
- ordered event metadata
- prompt lifecycle events
- approval events
- reconnect/catchup behavior for an active topic session

It does not try to solve durable replay across runtime restart.

## Scope

This contract applies to one topic websocket connection at a time, for example:

```text
wss://.../acp/{namespace}/{workspace}/topics/{topic}
```

The same logical contract may also be exposed behind compatibility aliases.

## Session Model

Each active topic runtime has a `sessionId`.

Important meaning:
- `sessionId` identifies one live topic runtime session
- `eventId` ordering is only meaningful within one `sessionId`
- if the runtime restarts and `sessionId` changes, old `eventId` values are no
  longer resumable

For the demo contract, replay is guaranteed only within the current live
`sessionId`.

## Connect and Replay

### Connect request

Clients connect normally, with an optional `since` query parameter:

```text
wss://.../topics/debug-timeout?since=e_42
```

Meaning:
- if `since` is omitted, the server must send a bounded replay window for the
  current active turn and any unresolved approval requests
- if `since` is present, the server attempts to replay events strictly after
  that event ID within the current `sessionId`

### Replay window requirement

The runtime must retain an ordered in-memory replay window per active topic
session.

For the demo, that window must be large enough to cover:
- the current in-flight prompt, if any
- unresolved approval requests
- recent events needed for a reconnecting or late-joining client to catch up to
  the active turn

The protocol does not yet require replay after runtime restart.

If `since` cannot be fully honored because the requested event is no longer in
the retained window, the server must:
- replay from the oldest retained event it still has for that `sessionId`
- set `replayTruncated: true`

### First server message

The server must send a `connected` message first:

```json
{
  "type": "connected",
  "protocolVersion": "demo-v1",
  "topic": "debug-timeout",
  "sessionId": "s_123",
  "latestEventId": "e_88",
  "replayRequestedSince": "e_42",
  "replayFrom": "e_43",
  "replayTruncated": false
}
```

Field meanings:
- `protocolVersion`: fixed as `demo-v1` for this contract
- `latestEventId`: newest event known at connect time
- `replayRequestedSince`: the client-supplied `since`, or `null`
- `replayFrom`: the first event actually replayed after connect, or `null`
- `replayTruncated`: `true` if the server could not honor the requested replay
  point and started from a later retained event

After `connected`, the server sends zero or more replayed events in order,
followed by the live tail.

Replayed events must carry `"replay": true`.

## Client Messages

### `prompt`

Clients submit prompts with:

```json
{
  "type": "prompt",
  "promptId": "p_123",
  "data": "Please debug the timeout"
}
```

Rules:
- `promptId` is required
- `promptId` must be unique within the current topic session from that client
- the runtime echoes the same `promptId` on all prompt-scoped events

### `approval_response`

Clients resolve approvals with:

```json
{
  "type": "approval_response",
  "approvalId": "a_123",
  "decision": "approved",
  "reason": "Reviewed and approved"
}
```

Rules:
- `decision` must be `approved` or `denied`
- client messages must not include `approver`
- the runtime derives approver identity from authenticated connection context

## Server Event Envelope

All server messages except `connected` must include:
- `eventId`
- `timestamp`

Format:

```json
{
  "type": "text",
  "eventId": "e_44",
  "timestamp": "2026-03-10T12:00:01Z",
  "replay": false,
  "promptId": "p_123",
  "data": "Looking into the timeout path now..."
}
```

Rules:
- `eventId` is opaque but strictly ordered within one `sessionId`
- `timestamp` is RFC3339 UTC
- `replay` is `true` only for replayed events; it may be omitted or `false` for
  live events

## Server Messages

### `prompt_status`

The runtime must emit prompt lifecycle updates:

```json
{
  "type": "prompt_status",
  "eventId": "e_45",
  "timestamp": "2026-03-10T12:00:02Z",
  "promptId": "p_123",
  "status": "accepted"
}
```

Allowed `status` values for the demo:
- `accepted`
- `queued`
- `started`
- `cancelled`

Rules:
- every prompt must produce `accepted`
- prompts submitted while another prompt is active must also produce `queued`
- when the runtime begins executing the prompt, it must emit `started`

### `text`

Assistant text is streamed as:

```json
{
  "type": "text",
  "eventId": "e_46",
  "timestamp": "2026-03-10T12:00:03Z",
  "promptId": "p_123",
  "data": "I found the timeout path in the worker."
}
```

For the demo, `data` is text-only.

### `tool_call`

Tool invocation start:

```json
{
  "type": "tool_call",
  "eventId": "e_47",
  "timestamp": "2026-03-10T12:00:04Z",
  "promptId": "p_123",
  "toolCallId": "tc_123",
  "tool": "bash",
  "title": "bash",
  "status": "pending"
}
```

### `tool_update`

Tool progress/result update:

```json
{
  "type": "tool_update",
  "eventId": "e_48",
  "timestamp": "2026-03-10T12:00:05Z",
  "promptId": "p_123",
  "toolCallId": "tc_123",
  "tool": "bash",
  "status": "completed",
  "data": "timeout path found"
}
```

Rules:
- `status` must be one of `running`, `completed`, or `failed`
- `data` is optional text for the demo
- structured or binary tool payloads are out of scope for this contract

### `approval_request`

Approval pause:

```json
{
  "type": "approval_request",
  "eventId": "e_49",
  "timestamp": "2026-03-10T12:00:06Z",
  "promptId": "p_123",
  "approvalId": "a_123",
  "toolCallId": "tc_124",
  "tool": "github",
  "action": "repo.push",
  "approvers": ["alice@acme.com"],
  "inputSummary": "push branch fix-timeout",
  "expiresAt": "2026-03-10T12:05:06Z"
}
```

### `approval_decision`

Approval resolved:

```json
{
  "type": "approval_decision",
  "eventId": "e_50",
  "timestamp": "2026-03-10T12:01:00Z",
  "promptId": "p_123",
  "approvalId": "a_123",
  "toolCallId": "tc_124",
  "decision": "approved",
  "approver": "alice@acme.com",
  "reason": "Reviewed and approved"
}
```

Allowed `decision` values:
- `approved`
- `denied`
- `timed_out`

### `done`

Prompt completion:

```json
{
  "type": "done",
  "eventId": "e_51",
  "timestamp": "2026-03-10T12:01:05Z",
  "promptId": "p_123",
  "status": "completed"
}
```

Allowed `status` values:
- `completed`
- `failed`
- `cancelled`

Every prompt must end with exactly one `done`.

### `error`

Prompt-scoped error:

```json
{
  "type": "error",
  "eventId": "e_52",
  "timestamp": "2026-03-10T12:01:04Z",
  "promptId": "p_123",
  "data": "tool execution failed"
}
```

For the demo:
- `error` may appear before `done`
- if `error` is emitted for a prompt, that prompt must still end with `done`
  using `status: "failed"`

### `system`

Human-readable informational text:

```json
{
  "type": "system",
  "eventId": "e_53",
  "timestamp": "2026-03-10T12:01:06Z",
  "data": "client joined"
}
```

`system` is allowed for informational text, but it must not be the only carrier
for protocol state such as queueing, approvals, or prompt lifecycle.

## Demo Guarantees

This RFC defines the demo guarantee as:
- multi-client live fanout on one topic
- reconnect/catchup within one active `sessionId`
- late joiners can see the current active turn and pending approvals through the
  replay window
- prompt lifecycle is explicit and machine-readable

## Non-Goals For This Contract

Explicitly deferred:
- durable replay across runtime restart
- full transcript bootstrap from websocket alone
- image or binary content parts
- multiplexing multiple topics over one websocket
