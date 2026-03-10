# RFC 0003: Workspace Tool API Payloads

- Status: proposed
- Date: 2026-03-10

## Summary

This RFC settles the demo-ready hosted tool API.

It defines:
- exact payloads for `POST /tools`, `GET /tools`, and `GET /tools/{tool}`
- exact payloads for grant creation
- the canonical MCP transport shapes for `stdio` and `streamable_http`
- how shorthand `actions` and rich `actionDefs` relate

For the demo, the hosted workspace tool API is the source of truth. Remote MCP
tool discovery is explicitly out of scope.

## Resource Model

For the demo contract, one workspace tool resource is one connected tool
binding.

Canonical identity:
- `name` is the tool identifier within one workspace

The contract does not introduce a separate opaque `toolId` yet.

## Routes

```text
GET    /tools
POST   /tools
GET    /tools/{tool}
DELETE /tools/{tool}
POST   /tools/{tool}/grants
DELETE /tools/{tool}/grants/{grantId}
```

These routes are relative to the workspace runtime base.

## Tool Create Request

### Required fields

`POST /tools` must accept:

```json
{
  "name": "github",
  "description": "GitHub repository operations",
  "provider": "alice@acme.com",
  "protocol": "mcp",
  "transport": {
    "type": "stdio",
    "command": "uvx",
    "args": ["mcp-server-github"],
    "env": {}
  },
  "actions": ["repo.read", "pr.create"]
}
```

Required fields:
- `name`
- `protocol`
- `transport`
- either `actions` or `actionDefs`

For the demo, `protocol` must be `"mcp"`.

### Rich action form

If richer action metadata is available, clients should send:

```json
{
  "name": "github",
  "description": "GitHub repository operations",
  "provider": "alice@acme.com",
  "protocol": "mcp",
  "transport": {
    "type": "streamable_http",
    "url": "https://github-mcp.example.com",
    "headers": {}
  },
  "actionDefs": [
    {
      "name": "repo.read",
      "description": "Read repository metadata and files",
      "inputSchema": {
        "type": "object",
        "properties": {
          "repo": { "type": "string" },
          "path": { "type": "string" }
        },
        "required": ["repo"],
        "additionalProperties": false
      }
    },
    {
      "name": "pr.create",
      "description": "Create a pull request",
      "inputSchema": {
        "type": "object",
        "properties": {
          "repo": { "type": "string" },
          "title": { "type": "string" }
        },
        "required": ["repo", "title"],
        "additionalProperties": false
      }
    }
  ]
}
```

Rules:
- `actions` and `actionDefs` may both be sent, but if both are present they
  must agree on action names
- `actionDefs[].name` is required
- `actionDefs[].inputSchema`, when present, must be a JSON Schema object
- `description`, `provider`, and `credentialRef` are optional for the demo

### Optional fields

Optional create fields:

```json
{
  "credentialRef": "secret://github/alice",
  "config": {}
}
```

Meaning:
- `credentialRef` is an opaque reference for runtime-specific secret lookup
- `config` is an escape hatch for provider-specific non-transport options

The canonical transport definition still lives under `transport`.

## Transport Union

### MCP stdio

```json
{
  "type": "stdio",
  "command": "uvx",
  "args": ["mcp-server-github"],
  "env": {
    "GITHUB_TOKEN": "${secret://github/alice}"
  }
}
```

Required:
- `type`
- `command`

Optional:
- `args`
- `env`
- `cwd`

### MCP streamable HTTP

```json
{
  "type": "streamable_http",
  "url": "https://github-mcp.example.com",
  "headers": {
    "Authorization": "Bearer ${secret://github/alice}"
  }
}
```

Required:
- `type`
- `url`

Optional:
- `headers`

The demo contract supports exactly these two MCP transport types.

## Tool Response Shape

`POST /tools` returns the same full object shape as `GET /tools/{tool}`.

### Summary shape

`GET /tools` returns a list of summary objects:

```json
[
  {
    "name": "github",
    "description": "GitHub repository operations",
    "provider": "alice@acme.com",
    "protocol": "mcp",
    "transport": {
      "type": "stdio",
      "command": "uvx",
      "args": ["mcp-server-github"],
      "env": {}
    },
    "actions": ["repo.read", "pr.create"],
    "actionDefs": [
      { "name": "repo.read", "description": "Read repository metadata and files" },
      { "name": "pr.create", "description": "Create a pull request" }
    ],
    "status": "ready",
    "createdAt": "2026-03-10T12:00:00Z"
  }
]
```

### Detail shape

`GET /tools/{tool}` returns the full tool object:

```json
{
  "name": "github",
  "description": "GitHub repository operations",
  "provider": "alice@acme.com",
  "protocol": "mcp",
  "transport": {
    "type": "stdio",
    "command": "uvx",
    "args": ["mcp-server-github"],
    "env": {}
  },
  "actions": ["repo.read", "pr.create"],
  "actionDefs": [
    {
      "name": "repo.read",
      "description": "Read repository metadata and files",
      "inputSchema": {
        "type": "object"
      }
    },
    {
      "name": "pr.create",
      "description": "Create a pull request",
      "inputSchema": {
        "type": "object"
      }
    }
  ],
  "credentialRef": "secret://github/alice",
  "config": {},
  "status": "ready",
  "createdAt": "2026-03-10T12:00:00Z",
  "grants": [
    {
      "grantId": "g_123",
      "subject": "agent:*",
      "actions": ["repo.read"],
      "access": "allowed",
      "approvers": [],
      "scope": {}
    }
  ],
  "log": []
}
```

Rules:
- responses always include both `actions` and `actionDefs`
- if the request only provided `actions`, the server must synthesize minimal
  `actionDefs` using those names
- `status` for the demo may be:
  - `ready`
  - `unreachable`
  - `error`

## Grant Create Request

`POST /tools/{tool}/grants` accepts:

```json
{
  "subject": "agent:*",
  "actions": ["repo.read"],
  "access": "allowed",
  "approvers": [],
  "scope": {}
}
```

Rules:
- `subject` is required
- `actions` must be a non-empty subset of the tool's registered actions
- `access` must be one of:
  - `allowed`
  - `approval_required`
- if `access` is `approval_required`, `approvers` must be non-empty

Response shape:

```json
{
  "grantId": "g_123",
  "subject": "agent:*",
  "actions": ["repo.read"],
  "access": "allowed",
  "approvers": [],
  "scope": {},
  "createdAt": "2026-03-10T12:01:00Z"
}
```

`POST /tools/{tool}/grants` returns that grant object directly.

## Demo Guarantees

This RFC defines the demo guarantee as:
- hosted REST registration is canonical
- MCP `stdio` and `streamable_http` are the supported execution transports
- action metadata is explicit enough to expose useful model-facing tools
- grants and approval policy are configured through the hosted API

## Non-Goals For This Contract

Explicitly deferred:
- registry sync or remote tool mirroring
- opaque `toolId` separate from `name`
- per-action output schema enforcement
- secret-management standardization beyond opaque `credentialRef` and transport
  placeholders
