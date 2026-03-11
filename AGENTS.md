# Workspace Notes

## Scope
This repository is a scratch workspace for evaluating how `shelley` might interoperate with the Agentic Workspace Protocol.

Treat the draft plan in [`docs/plan.md`](/home/jmandel/hobby/workspace-protocol/docs/plan.md) as WIP background only. It appears to be the same design draft that was originally referred to as `tmp.md`. Do not execute it as a plan without re-validating against the cloned repositories. Use checked-out code as the source of truth.

The root directory was initialized as a local git repo on March 9, 2026 to hold notes plus the two upstream clones. No remote is configured yet.

## Layout
- [`docs/plan.md`](/home/jmandel/hobby/workspace-protocol/docs/plan.md): draft design/implementation notes; current location of the WIP plan file.
- [`shelley/`](/home/jmandel/hobby/workspace-protocol/shelley): cloned from `github.com/boldsoftware/shelley`.
- [`agentic-workspace/`](/home/jmandel/hobby/workspace-protocol/agentic-workspace): cloned from `github.com/niquola/agentic-workspace`.

The two subdirectories above are independent git repositories, not submodules.

## Checked-Out Revisions
- `shelley`: `main` at `1f1ad4dc287e56a43f6a39e02d85b195f222d552`
- `agentic-workspace`: `main` at `95cd4aad97ecd5507c5b521480711f8b2de30345`

## Shelley Quick Map
- [`shelley/cmd/shelley/main.go`](/home/jmandel/hobby/workspace-protocol/shelley/cmd/shelley/main.go): CLI entrypoint. Main commands are `serve`, `client`, `unpack-template`, `version`.
- [`shelley/server/server.go`](/home/jmandel/hobby/workspace-protocol/shelley/server/server.go) and [`shelley/server/handlers.go`](/home/jmandel/hobby/workspace-protocol/shelley/server/handlers.go): current HTTP API surface.
- [`shelley/server/convo.go`](/home/jmandel/hobby/workspace-protocol/shelley/server/convo.go): per-conversation runtime manager; hydration, loop creation, system prompt creation.
- [`shelley/loop/loop.go`](/home/jmandel/hobby/workspace-protocol/shelley/loop/loop.go): conversation/turn loop; queued user messages are processed serially here.
- [`shelley/db/db.go`](/home/jmandel/hobby/workspace-protocol/shelley/db/db.go): SQLite pool + embedded migrations.
- [`shelley/claudetool/`](/home/jmandel/hobby/workspace-protocol/shelley/claudetool): tool implementations.
- [`shelley/ui/`](/home/jmandel/hobby/workspace-protocol/shelley/ui): React + TypeScript + esbuild + pnpm frontend.

### Shelley Current Behavior
- Shelley is currently a single-user, multi-conversation coding agent with SSE streaming and a WebSocket terminal endpoint.
- Conversation routes live under `/api/...`, for example:
  - `/api/conversations`
  - `/api/conversations/new`
  - `/api/conversation/{id}`
  - `/api/conversation/{id}/stream`
  - `/api/conversation/{id}/chat`
  - `/api/conversation/{id}/cancel`
- There is no ACP adapter or workspace/topic runtime in the current codebase.
- There is a `require-header` server option that can force a header on `/api/*` requests, which may matter later for multi-user work.

### Shelley Mental Model
- SQLite `conversations` are durable; in-memory `ConversationManager` instances are lazy runtime wrappers around active conversations.
- Each active conversation gets its own `loop.Loop`, `ToolSet`, mutable working directory, and realtime pub/sub stream.
- Shelley is not a thin wrapper around an external agent process. The Go server is the agent harness, persistence layer, tool runner, and streaming server.

### Shelley Data Model
- `conversations` now carry more than the earliest schema doc suggests, including later-added fields like `cwd`, `archived`, `parent_conversation_id`, and `model`.
- `messages` persist `llm_data`, `user_data`, `usage_data`, `display_data`, and `excluded_from_context`.
- Subagents are regular child conversations linked by `parent_conversation_id`.
- Message handling code distinguishes `user`, `agent`, `tool`, `system`, `error`, and `gitinfo` behaviors, even if the earliest SQL file only shows the original set.

### Shelley Request Lifecycle
- UI sends `POST /api/conversations/new` or `POST /api/conversation/{id}/chat`.
- Server resolves the requested model, gets or creates a `ConversationManager`, and records the user message to SQLite immediately so it appears in the UI before the turn completes.
- `ConversationManager.AcceptUserMessage()` queues onto the long-lived loop and flips conversation state to `working=true`.
- `ConversationManager.ensureLoop()` rereads canonical history from SQLite, partitions system prompt versus replayable history, constructs a per-conversation `ToolSet`, and starts `loop.Go()` in a goroutine.
- `loop.Go()` serially drains queued user messages, calls the selected `llm.Service`, executes tool calls, records tool/agent messages, and repeats until end of turn.
- `recordMessage()` persists each new message, updates `updated_at`, and publishes only the incremental delta to subscribers.

### Shelley Streaming Model
- Main chat streaming is SSE on `/api/conversation/{id}/stream`.
- SSE supports resume with `last_sequence_id`; the UI tracks the highest seen sequence number and reconnects from there.
- Shelley multiplexes more than message deltas over the same stream:
  - conversation metadata changes
  - conversation-list updates for other conversations
  - per-conversation working-state updates
  - notification events
- The in-memory primitive is [`shelley/subpub/subpub.go`](/home/jmandel/hobby/workspace-protocol/shelley/subpub/subpub.go), which has indexed `Publish()` plus unconditional `Broadcast()`.

### Shelley UI Behavior
- [`shelley/ui/src/App.tsx`](/home/jmandel/hobby/workspace-protocol/shelley/ui/src/App.tsx) owns the conversation list, URL slug routing, current selection, and cross-conversation updates.
- [`shelley/ui/src/components/ChatInterface.tsx`](/home/jmandel/hobby/workspace-protocol/shelley/ui/src/components/ChatInterface.tsx) owns conversation loading, SSE connection management, reconnect logic, send/cancel actions, and live tool rendering.
- The UI keeps an in-memory LRU cache in [`shelley/ui/src/services/conversationCache.ts`](/home/jmandel/hobby/workspace-protocol/shelley/ui/src/services/conversationCache.ts) and uses sequence IDs for fast resume after reconnect/backgrounding.
- Real-time tool rendering and persisted-message rendering are split; new tools must be registered in both `ChatInterface.tsx` and `Message.tsx`.
- Text input starting with `!` does not go through the chat API; it opens an ephemeral terminal panel in the UI.

### Shelley Tool Runtime
- `ToolSet` is assembled per conversation in [`shelley/claudetool/toolset.go`](/home/jmandel/hobby/workspace-protocol/shelley/claudetool/toolset.go).
- Core tools are `bash`, `patch`, `keyword_search`, `change_dir`, and `output_iframe`.
- Optional tools include browser automation, `subagent`, and `llm_one_shot`.
- Tool availability and schema shape depend on context:
  - patch schema is simplified for weaker models
  - subagent availability is depth-limited
  - browser tools are opt-in
- `change_dir` is the persistent directory switch; bash state does not persist between tool calls.

### Shelley Prompt Construction
- System prompts are generated lazily on first conversation hydration, then stored as `system` messages in the conversation itself.
- [`shelley/server/system_prompt.go`](/home/jmandel/hobby/workspace-protocol/shelley/server/system_prompt.go) collects git info, working-directory guidance files, user-level guidance files, local skills, and environment facts.
- Root/workdir guidance includes `AGENTS.md` discovery, so this repository's own root [`AGENTS.md`](/home/jmandel/hobby/workspace-protocol/AGENTS.md) is relevant if Shelley is pointed at this repo.
- Subagent conversations use a separate minimal subagent system prompt.

### Shelley Special Features
- Subagents are real child conversations with their own history and runtime, not just synthetic tool output.
- Distillation creates a fresh conversation seeded with an LLM-generated operational handoff from an older conversation.
- There is a second realtime channel besides SSE: `/api/exec-ws` provides a PTY-backed terminal WebSocket for the UI terminal panel.
- Background routines handle conversation-manager cleanup, notification fanout, and optional auto-upgrade/restart.

### Shelley Commands Worth Remembering
- `cd shelley && make`
- `cd shelley && make serve`
- `cd shelley && make test-go`
- `cd shelley/ui && pnpm run type-check`
- `cd shelley/ui && pnpm run test:e2e`

### Shelley Docs to Trust Carefully
- [`shelley/ARCHITECTURE.md`](/home/jmandel/hobby/workspace-protocol/shelley/ARCHITECTURE.md) is partly stale. It still mentions Vue/Jest, but the actual UI is React and Playwright according to [`shelley/ui/package.json`](/home/jmandel/hobby/workspace-protocol/shelley/ui/package.json) and the source tree.
- [`shelley/AGENTS.md`](/home/jmandel/hobby/workspace-protocol/shelley/AGENTS.md) is worth reading before editing Shelley itself.

## Agentic Workspace Quick Map
- [`agentic-workspace/agent-workspace.md`](/home/jmandel/hobby/workspace-protocol/agentic-workspace/agent-workspace.md): protocol draft/spec.
- [`agentic-workspace/reference-impl/wsmanager.ts`](/home/jmandel/hobby/workspace-protocol/agentic-workspace/reference-impl/wsmanager.ts): host-side REST manager that launches Docker containers.
- [`agentic-workspace/reference-impl/wmlet.ts`](/home/jmandel/hobby/workspace-protocol/agentic-workspace/reference-impl/wmlet.ts): per-workspace Bun server; manages topics and bridges WebSocket clients to ACP agents.
- [`agentic-workspace/reference-impl/cli.ts`](/home/jmandel/hobby/workspace-protocol/agentic-workspace/reference-impl/cli.ts): CLI for creating, listing, deleting, inspecting, and connecting.
- [`agentic-workspace/reference-impl/Dockerfile`](/home/jmandel/hobby/workspace-protocol/agentic-workspace/reference-impl/Dockerfile): Bun image that installs Node, git, ACP deps, and initializes `/workspace` as a git repo.

### Agentic Workspace Current Behavior
- The reference implementation is Bun + Docker based.
- `wsmanager.ts` exposes simple REST routes such as `/workspaces` and `/health`.
- `wmlet.ts` exposes:
  - `GET /topics`
  - `POST /topics`
  - `GET /topics/:name`
  - `DELETE /topics/:name`
  - `GET /health`
  - `WS /acp/:topic`
- Each topic spawns a separate `claude-agent-acp` subprocess with its own ACP session and shared workspace filesystem.
- `wmlet.ts` currently does not queue concurrent prompts. If a topic is busy, it sends `agent is busy, wait...` and drops the prompt.

### Agentic Workspace Commands Worth Remembering
- `cd agentic-workspace/reference-impl && docker build -t agrp-wmlet .`
- `cd agentic-workspace/reference-impl && bun run wsmanager.ts`
- `cd agentic-workspace/reference-impl && bun run cli.ts health`
- `cd agentic-workspace/reference-impl && bun run cli.ts create my-task general debug`
- `cd agentic-workspace/reference-impl && bun run cli.ts connect my-task debug`

## High-Signal Observations
- [`docs/plan.md`](/home/jmandel/hobby/workspace-protocol/docs/plan.md) broadly matches the direction of travel, but it is a design draft, not an accurate snapshot of either codebase.
- The protocol spec in [`agentic-workspace/agent-workspace.md`](/home/jmandel/hobby/workspace-protocol/agentic-workspace/agent-workspace.md) is broader than the current reference implementation. The spec describes namespaced `/apis/v1/...` endpoints, suspend/resume/clone, commits, and file APIs; the Bun reference implementation does not implement that full surface yet.
- Shelley has much richer persistence, UI, and tooling than the reference implementation, but it currently speaks its own REST/SSE protocol rather than ACP/WebSocket topic APIs.
- Shelley already has several pieces that look useful for future workspace work:
  - serial prompt queuing within one agent context
  - persisted conversation history
  - multi-viewer realtime updates
  - child conversations via subagents
  - mutable per-conversation working directories
- Shelley still fundamentally models `conversation` as the top-level unit. A real workspace/topic design would need an explicit workspace abstraction above the current conversation model rather than just renaming fields.
- [`agentic-workspace/reference-impl/test.ts`](/home/jmandel/hobby/workspace-protocol/agentic-workspace/reference-impl/test.ts) looks stale relative to current `wmlet.ts` message shapes. It refers to `input`/`output` and `history`, while current `wmlet.ts` emits `prompt`, `text`, `tool_call`, `tool_update`, `done`, etc.
- If future work starts on integration, validate against the actual Bun CLI and wmlet behavior first. Do not implement from spec prose or the draft plan alone.

## Likely Integration Hotspots
- [`shelley/server/server.go`](/home/jmandel/hobby/workspace-protocol/shelley/server/server.go): likely anchor point for any future workspace/topic REST or WebSocket surface.
- [`shelley/server/convo.go`](/home/jmandel/hobby/workspace-protocol/shelley/server/convo.go): closest existing analogue to a per-topic runtime.
- [`shelley/loop/loop.go`](/home/jmandel/hobby/workspace-protocol/shelley/loop/loop.go): already provides the "one prompt at a time within one context" behavior that topics will need.
- [`shelley/db/schema/`](/home/jmandel/hobby/workspace-protocol/shelley/db/schema): likely needs new first-class workspace/topic/participant tables instead of overloading `conversations`.
- [`shelley/ui/src/App.tsx`](/home/jmandel/hobby/workspace-protocol/shelley/ui/src/App.tsx) and [`shelley/ui/src/components/ConversationDrawer.tsx`](/home/jmandel/hobby/workspace-protocol/shelley/ui/src/components/ConversationDrawer.tsx): current navigation is conversation-first, so a workspace/topic UX would require deliberate reshaping here.

## Next-Session Bias
- Start by re-reading the current code entrypoints above.
- Use the cloned repos, not GitHub pages, unless a newer upstream revision is intentionally required.
- If editing Shelley UI or tool rendering, also read [`shelley/ui/src/components/AGENTS.md`](/home/jmandel/hobby/workspace-protocol/shelley/ui/src/components/AGENTS.md).

## VM Setup Notes
- This exe.dev VM did not initially have the JS/browser/runtime dependencies needed by this repo.
- Installed during setup:
  - `bun` for `shelleymanager/web` builds
  - Java 21 (`openjdk-21-jre-headless`) for the `fhir-validator` local tool; Java 17+ is required
  - Node.js/npm so `npx` is available in the VM
  - `bubblewrap` for `shelleymanager` `bwrap` runtime mode
  - Google Chrome (`google-chrome`) for Shelley browser tools
- Ubuntu's `chromium-browser` package in this VM is only a snap shim and is not a usable browser here.
- `snapd` can start, but this VM does not fully support snap packages because squashfs mounting is unavailable, so prefer non-snap packages.
- `bun` was installed under `~/.bun/bin` and also copied to `/usr/local/bin/bun` so sandboxed `bwrap` runtimes can resolve it via mounted system paths.
