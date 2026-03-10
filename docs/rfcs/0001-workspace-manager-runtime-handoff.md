# RFC 0001: Clarify Workspace Manager to Runtime Handoff

- Status: draft
- Date: 2026-03-10

## Summary

The protocol draft already distinguishes:
- a Workspace Manager API that creates and manages workspaces
- a workspace runtime that serves topics, files, tools, and ACP endpoints

This RFC does not change that split.

Instead, it narrows the question to one missing detail: what exactly the
manager must return so a client can reliably hand off from manager discovery to
workspace interaction.

## Context

The current draft already defines:
- Manager routes under `/apis/v1/namespaces/{ns}/workspaces`
- workspace resource routes under `/apis/v1/namespaces/{ns}/workspaces/{name}/...`
- ACP endpoints such as:
  - `wss://relay.example.com/acp/acme/payments-debug`
  - `wss://relay.example.com/acp/acme/payments-debug/topics/debug-timeout`

That is a real control-plane/runtime split already.

The ambiguity is narrower:
- the example manager response includes an ACP `endpoint` and per-topic ACP URLs
- but it does not clearly define the complete handoff contract a client should
  use after creation or lookup
- in particular, it is not explicit whether clients should derive all runtime
  URLs from naming conventions, or whether the manager response is expected to
  carry canonical runtime connection details

This matters for implementations like Shelley, where `1 process = 1 workspace`
may be a reasonable runtime architecture even if the public protocol remains
namespaced and manager-scoped.

## Decision

The draft's canonical public route model stands:
- Manager and resource APIs remain namespaced under `/apis/v1/...`
- topic ACP endpoints remain canonical workspace runtime entrypoints
- this RFC does not propose a second equal public route shape

What this RFC adds is a clearer handoff requirement.

### Manager responses should be explicit handoff objects

When a client creates or fetches a workspace, the manager response should
contain enough information to transition cleanly into workspace use without
guessing undocumented URL rules.

At minimum, the handoff object should make these discoverable:
- workspace identity
- workspace status
- workspace ACP endpoint
- topic ACP endpoints, or a clearly specified derivation rule
- workspace REST resource base, if clients are expected to call runtime-scoped
  REST resources directly after discovery

Using the draft's current example shape, that likely means retaining fields
such as:

```yaml
id: payments-debug.acme@relay.example.com
namespace: acme
name: payments-debug
status: active
endpoint: wss://relay.example.com/acp/acme/payments-debug
topics:
  - name: general
    acp: wss://relay.example.com/acp/acme/payments-debug/topics/general
```

and deciding whether an additional REST base URL field is required.

### Public mount shape remains canonical

This RFC does not bless root-scoped runtime routes like `/topics` or `/files`
as a second canonical public form.

A single-workspace runtime may still exist internally behind a proxy, relay, or
manager, but that is an implementation choice. The public protocol should
continue to describe the namespaced route model already present in the draft.

## Consequences

Benefits:
- stays aligned with the existing protocol draft
- clarifies what a client may rely on after manager discovery
- supports dedicated single-workspace runtimes without requiring the protocol to
  standardize their internal mount shape
- reduces accidental dependence on ad hoc URL construction

Costs:
- requires the draft to be more precise about manager response fields
- may require adding one explicit REST base URL field if the current ACP-only
  handoff is not sufficient for real clients

## Open Questions

- Is the existing `endpoint` field sufficient, or should the manager also return
  a canonical REST base URL for workspace-scoped resources?
- Are per-topic ACP URLs required in the response, or should the spec define a
  deterministic derivation rule from the workspace endpoint?
- Should manager responses expose capability flags, or should clients infer
  features from protocol version and successful calls?
- Which fields are mandatory on `POST /workspaces` responses versus
  `GET /workspaces/{name}` responses?
