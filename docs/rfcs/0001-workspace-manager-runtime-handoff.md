# RFC 0001: Workspace Manager and Runtime API Handoff

- Status: draft
- Date: 2026-03-10

## Summary

The protocol should distinguish between:
- a manager API that creates and locates workspaces
- a runtime API that operates inside one already-running workspace

This lets a Shelley process remain a single-workspace runtime without forcing
workspace identity into every runtime path.

## Context

Right now the draft materials blur two concerns:
- creating or finding a workspace from outside the runtime
- talking to topics, files, tools, and websocket streams inside that workspace

That creates tension around URL shape:
- if every API lives under `/workspaces/{id}/...`, multi-tenant gateways are
  easy to model, but a dedicated single-workspace runtime has to expose an
  artificial embedded workspace path
- if a runtime exposes root-scoped `/topics`, `/files`, `/tools`, and
  `/ws/topic/{name}`, a single-workspace process is simple, but there is no
  clean protocol-defined handoff for "create a new workspace and tell me where
  it lives"

For Shelley specifically, `1 process = 1 workspace` is a reasonable deployment
model. The missing piece is not necessarily a namespaced runtime path. The
missing piece is a first-class manager-to-runtime handoff.

## Decision

The protocol should define two layers.

### 1. Manager API

The manager API owns lifecycle and discovery:
- create workspace
- list workspaces
- inspect workspace status
- stop, delete, or otherwise manage a workspace runtime

Example shape:

```json
{
  "workspaceId": "ws_123",
  "name": "payments-debug",
  "status": "ready",
  "runtimeBaseUrl": "https://runtime.example.com/w/ws_123",
  "runtimeWsBaseUrl": "wss://runtime.example.com/w/ws_123",
  "capabilities": ["topics", "files", "tools"]
}
```

Key point:
- `workspaceId` is the stable identity
- `runtimeBaseUrl` is where the runtime is currently reachable

Those should not be treated as the same thing.

### 2. Workspace Runtime API

The runtime API owns in-workspace operations:
- topics
- files
- tools
- workspace websocket streams

The runtime API may be mounted in either form:
- root-scoped for a dedicated runtime process:
  - `/topics`
  - `/files`
  - `/tools`
  - `/ws/topic/{name}`
- gateway-scoped for a shared host:
  - `/workspaces/{id}/topics`
  - `/workspaces/{id}/files`
  - `/workspaces/{id}/tools`
  - `/workspaces/{id}/ws/topic/{name}`

The logical API should be the same in both cases. Only the mounting changes.

## Consequences

Benefits:
- fits Shelley cleanly as a single-workspace runtime
- still allows a multi-workspace gateway or relay layer
- avoids baking deployment topology into workspace identity
- gives clients an explicit handoff from creation to runtime use

Costs:
- introduces a real control-plane/runtime split into the protocol
- requires clients to understand discovery first, then runtime interaction
- requires the protocol to define what fields a manager must return on create

## Open Questions

- Should the manager return a single `runtimeBaseUrl` plus conventions for WS,
  or distinct HTTP and WS base URLs?
- Should `capabilities` be part of the create response, discovery response, or
  both?
- Do we want a canonical path prefix for gateway-mounted runtimes, or should
  that remain deployment-defined as long as discovery returns the actual URL?
- If a workspace is relocated or restarted, what stability guarantees do we
  want for `runtimeBaseUrl`?
