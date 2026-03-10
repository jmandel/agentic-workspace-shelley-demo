# RFC 0001: Workspace Manager to Runtime Handoff

- Status: draft
- Date: 2026-03-10

## Summary

The Workspace Manager should be a control-plane service. It creates, looks up,
suspends, resumes, clones, and deletes workspaces. It should not be required to
host or proxy the workspace runtime APIs of the workspaces it manages.

The protocol should make the handoff explicit:
- clients talk to the Manager to create or find a workspace
- the Manager returns stable workspace identity plus runtime endpoints
- clients then talk directly to the workspace runtime for topics, tools, files,
  commits, and ACP

This keeps the protocol compatible with both:
- a Manager that proxies everything through one host
- a Manager that boots a VM or container and hands back its own runtime URL

## Current State

The draft spec already has the right high-level split in intent:
- Manager lifecycle routes under `/apis/v1/namespaces/{ns}/workspaces`
- workspace APIs for files, tools, commits, and topics
- ACP endpoints for the workspace and its topics

But the current shape still leaves one important ambiguity:
- are the workspace REST routes part of the Manager's own API surface
- or are they runtime routes exposed by a separate workspace host

That ambiguity matters because these are very different deployment models.

Example:
- a relay-style host may want to expose everything from one domain and proxy to
  individual runtimes
- a VM-based system may have the Manager create `payments-debug` on a fresh VM
  and then hand clients a direct runtime URL for that VM

The reference implementation already points in this direction by returning both
`acp` and `api` from the manager, but the draft spec does not yet make that
handoff explicit enough.

## Problem

If the spec treats the Manager API space as also being the canonical host for
workspace runtime routes, it quietly forces one architecture:
- the Manager must proxy or front all workspace traffic
- runtime location becomes coupled to Manager URL structure
- implementations that create independent VMs or containers need an extra proxy
  layer just to appear compliant

That is unnecessary protocol coupling.

The protocol should instead separate:
- stable workspace identity
- control-plane addressability
- current runtime location

## Proposal

### 1. Separate control plane from runtime plane

The Manager API remains the canonical control plane:

```text
POST   /apis/v1/namespaces/{ns}/workspaces
GET    /apis/v1/namespaces/{ns}/workspaces
GET    /apis/v1/namespaces/{ns}/workspaces/{name}
DELETE /apis/v1/namespaces/{ns}/workspaces/{name}
PUT    /apis/v1/namespaces/{ns}/workspaces/{name}/suspend
PUT    /apis/v1/namespaces/{ns}/workspaces/{name}/resume
POST   /apis/v1/namespaces/{ns}/workspaces/{name}/clone
```

The workspace runtime is a separate plane discovered from the Manager response.

The runtime may be:
- a dedicated single-workspace process
- a container
- a VM
- a relay-proxied path on the same host

All are valid.

### 2. The Manager returns a handoff object

`POST /workspaces` and `GET /workspaces/{name}` should return a full handoff
object with both identity and runtime endpoints.

Suggested shape:

```yaml
id: payments-debug.acme@relay.example.com
namespace: acme
name: payments-debug
status: active
manager:
  self: https://manager.example.com/apis/v1/namespaces/acme/workspaces/payments-debug
runtime:
  api: https://ws-123.runtime.example.com
  acp: wss://ws-123.runtime.example.com/acp
topics:
  - name: general
    acp: wss://ws-123.runtime.example.com/acp/topics/general
  - name: debug-timeout
    acp: wss://ws-123.runtime.example.com/acp/topics/debug-timeout
```

Meaning:
- `id` is stable workspace identity
- `manager.self` is the control-plane handle
- `runtime.api` is the base URL for the workspace runtime REST API
- `runtime.acp` is the workspace ACP base
- `topics[].acp` gives ready-to-use topic endpoints for existing topics

If an implementation wants one-host proxying, it can still return:
- `manager.self` on the Manager host
- `runtime.api` on that same host
- `runtime.acp` on that same host

The important point is that this is a returned endpoint choice, not a protocol
mandate.

### 3. Runtime APIs are defined relative to `runtime.api`

Once a client has `runtime.api`, the runtime REST surface is discovered
relative to that base.

For example:
- `{runtime.api}/topics`
- `{runtime.api}/tools`
- `{runtime.api}/files/...`
- `{runtime.api}/commits`

This avoids hard-wiring the runtime API to the Manager's path scheme.

### 4. Keep identity separate from location

The workspace's stable identity is `id`.

The workspace's current reachable runtime is `runtime.api` and `runtime.acp`.

Those are not the same thing and should not be conflated.

This allows:
- runtime migration
- suspend/resume onto a new machine
- cloning to a different host
- future indirection through service discovery or relays

without changing workspace identity.

### 5. Use summaries for list responses

`GET /workspaces` should return summaries, not full handoff objects.

Suggested summary fields:
- `id`
- `namespace`
- `name`
- `status`
- `createdAt`

Detailed runtime endpoints belong on:
- `POST /workspaces`
- `GET /workspaces/{name}`

## Why This Works Well

- It matches the real operational split between lifecycle management and
  runtime interaction.
- It does not force Managers to proxy traffic they do not need to proxy.
- It still works for relay-style deployments that do want a single host.
- It fits Shelley's likely deployment model well: one process per workspace,
  with the Manager simply launching Shelley and returning its runtime address.
- It is already close to the reference implementation's practical shape.

## Recommended Spec Changes

1. Clarify that the Manager API is the control plane, not necessarily the host
   for runtime APIs.
2. Require `POST /workspaces` and `GET /workspaces/{name}` to return runtime
   endpoints explicitly.
3. Define workspace REST routes relative to the returned runtime API base, not
   by assuming they live on the Manager origin.
4. Keep workspace identity (`id`) distinct from runtime location.

## Open Questions

- Should topic ACP endpoints always be returned explicitly, or is deriving them
  from `runtime.acp` acceptable for clients that create topics themselves?
- Do we want a `capabilities` field in the handoff object so clients know which
  runtime features are implemented?
- Should the runtime REST base be named `api`, `apiBase`, or something more
  explicit like `runtimeApi`?
