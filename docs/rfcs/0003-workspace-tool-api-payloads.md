# RFC 0003: Workspace Tool API Payloads

- Status: draft
- Date: 2026-03-10

## Summary

The draft spec defines tool concepts and routes, but not the concrete request
and response payloads for connecting tools, listing them, inspecting them, or
granting access.

This RFC proposes a practical workspace tool payload model that:
- keeps the draft's workspace-scoped tool resources
- reconciles the draft's registry concept with self-contained hosted
  registrations
- standardizes action metadata and transport config

## Context

The draft already says:
- tools are first-class resources
- tools may come from a broader registry
- tools are connected to a workspace with provider identity and grants

The Bun reference implementation does not implement `/tools` at all, so it does
not answer payload questions.

Shelley has already had to make concrete choices here:
- a workspace-hosted `POST /tools`
- persisted tool bindings
- grants as child resources
- action metadata richer than a plain string list, because the runtime needs
  descriptions and input schemas to expose usable model tools
- transport config that must distinguish MCP stdio from streamable HTTP

## Decision

### Resource model

The public workspace-level resource is a connected tool binding.

It may be created from:
- a registry reference
- or an inline/self-contained tool definition

Responses should normalize to the same full connected-tool shape regardless of
how the binding was created.

### Connect tool request

The connect request should support this normalized shape:

```json
{
  "tool": "gmail",
  "provider": "alice@acme.com",
  "description": "Read, search, and send Gmail",
  "protocol": {
    "type": "mcp",
    "transport": {
      "type": "streamable_http",
      "endpoint": "https://gmail-mcp.example.com"
    }
  },
  "actionDefs": [
    {
      "name": "read",
      "description": "Read a message",
      "inputSchema": { "type": "object" }
    },
    {
      "name": "send",
      "description": "Send a message",
      "inputSchema": { "type": "object" }
    }
  ],
  "credentialRef": "secret://gmail/alice",
  "config": {}
}
```

Notes:
- `tool` is the stable tool name or registry reference
- `actionDefs` is the canonical rich action shape
- plain string `actions` may still be accepted as a shorthand for compatibility,
  but responses should normalize to rich action definitions
- `protocol` is a typed object, not a free-form string plus opaque config blob

### Connected tool response

The canonical connected tool response should include:
- `toolId`
- `tool`
- `provider`
- `description`
- `protocol`
- `actionDefs`
- `credentialRef` or equivalent reference
- `status`
- `createdAt`
- `grants`

Optional operational fields may include:
- `lastCheckedAt`
- `lastError`
- `log`

### Grant payloads

Grant creation remains a child resource under `/tools/{tool}/grants`.

The payload should look like:

```json
{
  "subject": "agent:claude",
  "actions": ["read"],
  "access": "allowed",
  "approvers": [],
  "scope": {}
}
```

This RFC does not try to solve subject grammar or approval semantics; those are
covered by separate RFCs.

## Consequences

Benefits:
- makes tool payloads interoperable across runtimes
- gives model-facing runtimes the metadata they need to expose useful tools
- keeps the door open for registry-backed tools without requiring a registry for
  every implementation

Costs:
- responses become richer than the current draft examples
- runtimes must normalize shorthand action input into a stable richer form

## Open Questions

- Should the canonical request require registry reference plus optional
  overrides, or should fully inline tool definitions remain first-class?
- Should `actionDefs` be mandatory in responses even if the request used plain
  string `actions`?
- Should transport-specific config live entirely inside `protocol.transport`, or
  is a top-level `config` escape hatch still necessary?
