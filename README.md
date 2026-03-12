# Workspace Protocol

An implementation spike exploring what it takes to run the [Agentic Workspace Protocol](agentic-workspace/agent-workspace.md) on top of a real agent runtime ([Shelley](https://github.com/boldsoftware/shelley)).

The base protocol spec describes an idealized multiplayer agent environment — workspaces, topics, tools, access control. This repo is where that spec meets reality: a production-grade Go agent with SQLite persistence, serial prompt processing, and a rich tool runtime. The RFCs in `agentic-workspace/rfcs/` document every place where the base spec needed to be extended, constrained, or rethought to work with a real system.

## What's here

```
agentic-workspace/         Protocol spec + RFCs (submodule)
  agent-workspace.md       Base protocol draft
  rfcs/                    Extensions discovered during implementation
  reference-impl/cli.ts    CLI client (Bun/TypeScript)

shelley/                   Agent runtime (submodule, Go)
  server/                  Topic adapter, tool runtime, WS wire protocol

shelleymanager/            Workspace manager (Go, this repo)
  manager/                 Lifecycle, tool registration, event broadcasting
  web/                     React dashboard (Vite + Zustand + wouter)

test/smoke.sh              End-to-end integration test
docs/                      Implementation journal, demo script
```

## The base spec and what we changed

The [base protocol](agentic-workspace/agent-workspace.md) defines workspaces, topics, tools, and grants at a conceptual level. It does not specify wire formats, prompt lifecycle, or what happens when multiple humans interact with a running agent. Building a working system required seven RFCs that extend the base spec:

### RFC 0002 — [Topic Realtime Wire Contract](agentic-workspace/rfcs/0002-topic-realtime-wire-contract.md)

The base spec says topics have ACP endpoints but doesn't define the message format. This RFC settles the WebSocket wire protocol: event types (`text`, `tool_call`, `tool_update`, `done`, `error`, ...), the `eventId`/`timestamp` envelope, session-scoped replay on reconnect, and the constraint that every prompt must end with exactly one `done` event.

**Key tension with real systems:** Shelley records whole messages to SQLite, not token streams. The first adapter emitted `text` events at message boundaries rather than per-token. This is protocol-compliant but means clients see text arrive in chunks rather than streaming character by character. The RFC deliberately leaves token-level streaming as a transport optimization rather than a protocol requirement.

### RFC 0003 — [Workspace Tool API Payloads](agentic-workspace/rfcs/0003-workspace-tool-api-payloads.md)

The base spec describes tool registration and grants conceptually. This RFC defines the exact REST payloads for `POST /tools`, `GET /tools`, grant CRUD, and transport configuration (`stdio`, `streamable_http`).

**Key change from base spec:** The base spec treats tools as self-describing (each tool lists its actions inline). For MCP tools, this is redundant — the MCP server already knows its own capabilities. We made `tools` optional in the registration payload. When omitted, the manager discovers available tools at registration time by connecting to the MCP server and calling `tools/list`. This avoids requiring callers to duplicate information the MCP server already publishes, while still giving the runtime the schema it needs.

**Vocabulary drift:** The base spec uses `actions` for nested tool operations. The RFCs renamed this to `tools` to align with MCP's own vocabulary. The Go internals still use `actionDefs` in many places — a known inconsistency.

### RFC 0004 — [Approval Workflow Semantics](agentic-workspace/rfcs/0004-approval-workflow-semantics.md)

The base spec says tools can require approval but doesn't specify how. This RFC defines the approval lifecycle: `approval_request` and `approval_decision` events over the topic WebSocket, first-valid-response-wins resolution, timeout via `expiresAt`, and audit recording.

**Scoped down from base spec:** The base spec implies rich delegation chains and role-based approval routing. The RFC explicitly defers multi-step approval, quorum voting, and offline approval REST endpoints — implementing only the single-approver, synchronous path that a real demo needs.

### RFC 0005 — [Local Tool Catalog](agentic-workspace/rfcs/0005-local-tool-catalog.md)

Not in the base spec at all. Real workspaces need trusted local tools (a FHIR validator JAR, a linter, a database CLI) that are runtime-provided capabilities rather than user-registered MCP servers. This RFC introduces `GET /apis/v1/local-tools` as a manager-published catalog and `runtime.localTools` in workspace configuration to select which tools are available. The packaging mechanism (bind mounts, copied files, container layers) is intentionally left to the runtime.

### RFC 0006 — [Topic Prompt Queue Management](agentic-workspace/rfcs/0006-topic-prompt-queue-management.md)

The base spec's topic model assumes one prompt, one response. Real multiplayer use immediately breaks this: what happens when Alice submits a prompt while the agent is still working on Bob's? This RFC makes queued prompts first-class objects with stable identity, ownership, status tracking (`queued` → `started` → `completed`), and mutation (edit, reorder, cancel). The `queue_snapshot` event on connect gives clients the full picture.

**Real system constraint:** Shelley's `loop.Loop` processes prompts serially. The queue is a protocol-level construct layered on top of this runtime behavior, not a replacement for it. Prompt ordering in the queue is exactly the execution order.

### RFC 0007 — [Active Turn Injection and Interruption](agentic-workspace/rfcs/0007-active-turn-injection-and-interruption.md)

The base spec treats agent turns as atomic — submit a prompt, wait for completion. Real use demands mid-turn interaction. This RFC introduces three composable primitives:

- **`prompt`** — queue new work (from RFC 0006)
- **`inject`** — deliver a message into the active turn without stopping it
- **`interrupt`** — cancel the active turn

"Stop and redirect" is composed as `interrupt` + `prompt { position: 0 }` rather than a single operation. Injection delivery happens at well-defined safe points (after tool completion, before the next LLM call). Interruption propagates via Go context cancellation through the tool and LLM call chain.

**Implementation reality:** Injection is the hardest primitive. The message must be persisted to SQLite before the inject can be acknowledged, because a crash between acknowledgment and persistence would lose the message. The runtime delivers injected messages by appending them to the conversation's pending message queue and waiting for the loop to drain them at the next safe point.

### RFC 0009 — [Manager Lifecycle Notifications](agentic-workspace/rfcs/0009-manager-lifecycle-notifications.md)

Not in the base spec. The manager needs its own event stream for workspace/topic CRUD so dashboards can react without polling. This RFC adds a read-only WebSocket at `/acp/{namespace}/events` with replay-on-connect and live tail — following the same session model as RFC 0002 but scoped to the manager layer above individual workspaces.

## Architecture

```
                    ┌─────────────────────┐
                    │   Shelley Manager    │
                    │   (Go, this repo)    │
                    │                      │
   Browser/CLI ◄──►│  REST API            │
                    │  Events WebSocket    │
                    │  Tool registration   │
                    │  MCP discovery       │
                    │                      │
                    └──────────┬───────────┘
                               │ launch + proxy
                    ┌──────────▼───────────┐
                    │   Shelley Runtime    │
                    │   (Go, submodule)    │
                    │                      │
   Topic WS ◄─────►│  Topic adapter       │
                    │  Prompt queue        │
                    │  Inject / interrupt  │
                    │  Tool execution      │
                    │  SQLite persistence  │
                    │  LLM loop            │
                    └─────────────────────┘
```

The manager handles workspace lifecycle, tool registration (including MCP discovery at registration time), and event broadcasting. Each workspace gets its own Shelley runtime process. The manager proxies topic WebSocket connections and REST API calls to the appropriate runtime.

## Getting started

### Prerequisites

- Go 1.22+
- [Bun](https://bun.sh) (for the web UI build and CLI client)
- `bubblewrap` (`bwrap`) for sandboxed runtime — or set `RUNTIME_MODE=direct` to skip sandboxing

### Clone

```bash
git clone --recurse-submodules https://github.com/jmandel/workspace-protocol.git
cd workspace-protocol
```

If you already cloned without `--recurse-submodules`:

```bash
git submodule update --init --recursive
```

When updating an existing checkout, do not stop at `git pull`. Update the
submodule worktrees too:

```bash
git pull --ff-only
git submodule update --init --recursive
git submodule status
```

If `git status` shows `M agentic-workspace` or `M shelley` immediately after
pulling, the submodule checkout is not at the commit pinned by the parent repo
yet.

### Run with mock LLM (no API key needed)

```bash
cd shelleymanager
make dev-predictable
```

This builds the Shelley runtime, the web UI, and starts the manager on `http://localhost:31337`. The predictable mode uses canned responses instead of calling an LLM — useful for testing the protocol machinery without spending API credits.

### Run with real Claude

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cd shelleymanager
make dev-claude    # Claude Sonnet 4.6
# or
make dev-haiku     # Claude Haiku 4.5 (faster, cheaper)
```

### Use the dashboard

Open `http://localhost:31337/app/` in a browser. From there you can create workspaces, open topics, send prompts, and watch the agent work.

### Use the CLI

```bash
cd agentic-workspace/reference-impl
bun run cli.ts connect <workspace> <topic>
```

### Run the smoke test

```bash
bash test/smoke.sh
```

## Known spec/implementation gaps

- **`actions` vs `tools` vocabulary**: The base spec, RFCs, and Go internals use different terms for the same concept. The wire format uses `tools` but internal code still says `actionDefs`.
- **`user` event type**: Emitted by the runtime for injected messages but not defined in RFC 0002 (the wire contract). RFC 0007 introduces injection but doesn't formally extend 0002's event table.
- **`queue_cleared` event**: Handled by the CLI but not defined in any RFC.
- **Ownership enforcement**: RFC 0006 specifies that queue mutations are scoped to the submitter. The implementation accepts but ignores the sender identity — any client can modify any queued prompt.
- **MCP tool discovery drops fields**: `outputSchema` and `annotations` from MCP `tools/list` responses are not captured during registration-time discovery.

## License

This integration work is unlicensed. The submodules carry their own licenses:
- Shelley: see `shelley/LICENSE`
- Agentic Workspace Protocol: MIT (see `agentic-workspace/LICENSE`)
