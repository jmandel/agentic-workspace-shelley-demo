# RFC 0004: Approval Workflow Semantics

- Status: draft
- Date: 2026-03-10

## Summary

The draft spec clearly wants approval-required tool access, but it does not yet
define the runtime workflow by which an approval is requested, decided, and
audited.

This RFC proposes that approval be a first-class workspace protocol concept,
not just an internal ACP callback.

## Context

The draft already includes:
- grants with `access: approval_required`
- approver lists
- audit expectations

The Bun reference implementation does not provide a real workspace-level
approval flow. It simply auto-approves ACP permission requests internally.

Shelley has already needed a user-visible approval path for workspace tools:
- agent requests an action that is approval-gated
- connected topic participants receive an approval request
- one participant responds
- the decision is recorded before the tool continues or fails

## Decision

### Approval is a workspace protocol object

Approval should be represented explicitly in the workspace protocol, with its
own identity and lifecycle.

It should not be modeled only as:
- an opaque internal ACP permission callback
- or implicit behavior folded into tool execution success/failure

### Approval request message

When a topic turn reaches an approval-required tool action, the runtime should
emit:

```json
{
  "type": "approval_request",
  "approvalId": "a_123",
  "toolCallId": "tc_123",
  "tool": "github",
  "action": "repo.push",
  "topic": "debug-timeout",
  "approvers": ["alice@acme.com"],
  "inputSummary": "push branch fix-timeout"
}
```

`approvalId` is the stable identifier for the approval workflow itself.

### Approval response message

A participant resolves the request with:

```json
{
  "type": "approval_response",
  "approvalId": "a_123",
  "approved": true,
  "approver": "alice@acme.com",
  "reason": "Reviewed and approved"
}
```

`toolCallId` should not be reused as the primary approval identifier.

### Approval result event

The runtime should emit an explicit approval decision event:

```json
{
  "type": "approval_decision",
  "approvalId": "a_123",
  "toolCallId": "tc_123",
  "approved": true,
  "approver": "alice@acme.com"
}
```

This avoids forcing clients to infer approval outcome only from downstream tool
events.

### Audit requirement

Every approval decision should be recorded with:
- approval identity
- tool and action
- subject
- approver
- decision
- timestamp

## Consequences

Benefits:
- turns approval into a real collaborative workflow, not an implementation
  detail
- makes approval UI and auditing interoperable across runtimes
- cleanly separates approval identity from tool execution identity

Costs:
- adds approval-specific message and log types to the protocol
- requires runtimes to keep pending approval state, not just immediate
  allow/deny decisions

## Open Questions

- Should approval also have a REST representation for offline approvers, or is a
  topic realtime flow sufficient for the initial protocol?
- Can multiple approvers respond, and if so what is the conflict-resolution
  rule?
- Should denial and timeout produce distinct `approval_decision` reasons?
