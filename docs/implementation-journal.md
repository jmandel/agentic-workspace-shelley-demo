# Implementation Journal

## 2026-03-10

### Scope chosen
- Re-read `docs/plan.md` against the checked-out `shelley` and `agentic-workspace` repos before coding.
- Did not start with the full workspace-spec surface from the draft. The Bun reference implementation and CLI only require a much smaller real interface first: `GET /health`, `GET|POST /topics`, `GET|DELETE /topics/{name}`, and `WS /acp/{topic}`.
- Chose to build that compatibility slice inside Shelley first, backed by the existing conversation runtime, instead of inventing new workspace/topic tables immediately.

### Plan correction
- The updated `docs/plan.md` corrected an earlier misunderstanding about ACP's role.
- Current direction:
  - Shelley does **not** need an internal ACP subprocess or `shelley-acp` binary to implement topics.
  - A topic is Shelley conversation state plus multiplayer fanout and prompt queueing, with Shelley's own `loop.Loop` running the agent directly.
  - The important interoperability target is the reference implementation's topic WebSocket message shape, not ACP stdio.
- This correction matches the code path already taken better than the older draft did.

### What I validated from code
- Shelley already has the key runtime primitive needed for topics: one queued prompt stream per conversation via `ConversationManager` and `loop.Loop`.
- Conversation history is already durable in SQLite and live updates are already broadcast through `subpub`.
- The Bun reference server (`reference-impl/wmlet.ts`) does not queue prompts today; Shelley already does. That makes Shelley a good host for the first compatibility layer.
- The Bun CLI (`reference-impl/cli.ts`) expects websocket messages shaped like `connected`, `system`, `text`, `tool_call`, `tool_update`, `done`, and `error`.

### Implementation in progress
- Added a wmlet-style adapter in Shelley server code.
- Mapping used for now:
  - topic name -> top-level conversation slug
  - topic session id -> conversation id
  - topic busy -> active conversation `agentWorking`
  - topic client count -> new in-memory websocket count per slug
- Added websocket prompt handling that emits `queued prompt` when the topic is already busy, then lets Shelley queue the prompt internally.
- Added `/ws/*` aliases so the draft plan's documented route family works without dropping the root wmlet-compatible routes.
- Added a minimal `/workspaces` and `/workspaces/{name}` discovery facade so the checked-out `agentic-workspace` CLI can discover the running Shelley process as a single workspace.
- Added `/ws/topic/{name}` as the native topic WebSocket route described by the updated plan.

### Surprises / deviations
- Archiving in Shelley keeps the conversation row, while wmlet's topic deletion semantics allow reconnecting with the same topic name later. To keep topic names reusable without losing history, I restored archived conversations on re-create / reconnect instead of creating a brand new conversation.
- Shelley records whole assistant/tool messages, not token chunks. The first adapter therefore emits websocket `text` events at message boundaries rather than token-by-token streaming. That is enough for Bun CLI compatibility but not full ACP-like granularity.
- Shelley also batches queued user prompts into the next LLM request if they arrive before that request is dispatched. So `queued prompt` is true in the sense of "accepted and not dropped", but not yet true in the stricter wmlet sense of "guaranteed separate next turn". This needs a deeper runtime change if strict per-prompt turn boundaries are required.
- Joining an existing topic over the wmlet-compatible websocket does not replay full history yet. The current adapter subscribes to new events only. Persistence exists underneath; history replay is still a follow-up item.
- The draft plan's validation commands are stale relative to the checked-out `agentic-workspace` repo:
- Some compatibility shims remain because the checked-out external client still expects them:
  - `/workspaces` discovery
  - `/acp/{topic}` websocket path
  - These are interop layers for today's Bun CLI, not the updated plan's core route shape.
- The earlier draft plan's validation commands were stale relative to the checked-out `agentic-workspace` repo:
  - `cli.ts connect ...` does not connect directly to wmlet; it first does manager discovery via `/workspaces/{name}`.
  - piping a prompt into `cli.ts connect` before the websocket is connected loses the input; the client currently drops pre-connection stdin.
  - the plan documents `/ws/...` routes, but current `wmlet.ts` serves `/topics`, `/health`, and `/acp/...` at the root.
  - the older draft smoke script used `serve --workspace-dir` and `--agent` flags that do not exist in the checked-out Shelley CLI yet.
  - `go build ./cmd/shelley` also requires generated template tarballs (`make templates`); the draft smoke script does not include that prerequisite.

### Current status
- Server adapter code is added.
- Focused tests for topic lifecycle, websocket queueing, and message-shape translation are added next to Shelley server tests.
- Verification completed for Shelley server package.
- Added `test/smoke.sh` at the workspace root to exercise the currently implemented cross-repo validation path end to end.

### Verification
- `corepack pnpm install --frozen-lockfile` in `shelley/ui/`
- `corepack pnpm run build` in `shelley/ui/`
- `go test ./server -run 'TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./server` in `shelley/`
- `bun install` in `agentic-workspace/reference-impl/`
- `go build -o /tmp/shelley-workspace-test ./cmd/shelley` in `shelley/` after `make templates`
- Live Shelley process:
  - `WORKSPACE_NAME=test-workspace /tmp/shelley-workspace-test -db /tmp/shelley-workspace-test.db -predictable-only -default-model predictable serve -port 0 -port-file /tmp/shelley-workspace-port`
- External validation against the checked-out Bun client:
  - `curl http://localhost:$PORT/workspaces`
  - `curl http://localhost:$PORT/ws/health`
  - `WS_MANAGER=http://localhost:$PORT bun run cli.ts connect test-workspace cli-topic`
    - sent `echo: hi` after the `connected` message
    - observed `thinking...`, `hi`, and `done`
  - `WS_MANAGER=http://localhost:$PORT bun run cli.ts topics test-workspace`
  - `curl http://localhost:$PORT/ws/topics`
  - `curl http://localhost:$PORT/api/conversations`
- `./test/smoke.sh` from the workspace root
  - passed after fixing one bash `coproc` bug in the script itself

### Smoke script scope
- The new `test/smoke.sh` is intentionally a subset of the draft Part VI script.
- It now also aligns with the updated plan's native `/ws/topic/{name}` route through Go test coverage, while the live Bun CLI smoke still uses the discovery path the checked-out client actually implements.
- It validates the implementation that actually exists now:
  - Shelley server package tests
  - full Shelley binary build prerequisites (`ui` build + `make templates`)
  - predictable-mode Shelley server startup
  - `/workspaces` discovery facade
  - current checked-out Bun `cli.ts` connect/topics flow
  - `/ws/topics`
  - `/api/conversations`
- It does not attempt the draft's Phase 0 ACP-agent binary gate, nonexistent `serve --workspace-dir` / `--agent` flags, or Docker/wsmanager deployment yet, because those parts are not implemented in the checked-out Shelley tree.

### Environment notes
- `pnpm` was not on `PATH` in this environment, but `corepack` was present. Used `corepack pnpm ...` rather than changing global tooling.

### 2026-03-10 update — topic runtime refactor
- Refactored the workspace websocket path onto explicit topic runtime pieces:
  - `server/topic.go`
  - `server/prompt_queue.go`
  - `server/ws_hub.go`
- The websocket handler now attaches clients to a shared per-topic `WSHub` and enqueues prompts onto a per-topic `PromptQueue` instead of creating a fresh `subpub` subscription and direct `AcceptUserMessage` path per connection.
- Topic busy state now includes queued work, not just `ConversationManager.agentWorking`.
- Archiving a topic now tears down the topic runtime and disconnects attached websocket clients via the hub.

### Queueing behavior change
- The refactor intentionally tightened prompt handling relative to the first adapter slice.
- Before this refactor, two prompts arriving close together could still batch into one next LLM request because Shelley accepted them directly into the conversation loop.
- After the new `PromptQueue` drainer, queued prompts are now processed as separate turns in order.
- Updated the websocket queue test to assert the stronger behavior: two prompts produce two LLM requests, preserving order.

### Phase 2 bridge
- Re-read the updated Phase 2 plan and implemented the feasible slice that matches the checked-out code today:
  - `POST /api/conversation/{id}/chat` now routes through the topic prompt queue when the conversation is already backed by an active topic runtime in `TopicManager`.
  - This gives Shelley's existing REST chat path and external websocket clients one shared serialized prompt path for active topics.
- Added a mixed-path test:
  - create topic
  - connect websocket client
  - send prompt through Shelley REST chat endpoint
  - verify websocket client receives the turn and the predictable model saw the prompt

### New plan drift found while re-reading
- The newer draft plan is ahead of the checked-out code in several places:
  - it assumes a future `topics` table that does not exist yet
  - it assumes a `--workspace-dir` mode flag that does not exist yet in the checked-out Shelley CLI
  - it assumes `/ws/tools` and later workspace REST surfaces that are not implemented yet
- Because there is still no first-class persisted topic table, the new REST chat bridge only applies when a topic runtime already exists in memory.
- Practical consequence:
  - browser + CLI sharing works for topics created or joined through the current workspace routes
  - opening an old topic-like conversation after a cold restart does not yet auto-recreate topic runtime purely from durable metadata
- I treated this as a design gap to note, not a reason to force the draft's future schema into the current phase.

### Validation update
- `go test ./server -run 'TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./server` in `shelley/`
- `corepack pnpm install --frozen-lockfile` in `shelley/ui/`
- `corepack pnpm run type-check` in `shelley/ui/`
- `./test/smoke.sh` from the workspace root
  - extended to prove the Phase 2 bridge in a live cross-repo path:
    - Bun `cli.ts` stays connected over websocket
    - Shelley receives `POST /api/conversation/{id}/chat`
    - Bun CLI receives that turn on the same topic

### 2026-03-10 update — durable topic identity
- Added a real `topics` table:
  - migration: `db/schema/017-topics.sql`
  - queries: `db/query/topics.sql`
- Topic rows now persist `topic_name -> conversation_id` separately from conversation slug overloading.
- Workspace topic list and lookup now read from persisted topic rows instead of treating every top-level slugged conversation as a topic.

### Runtime recovery and rename coherence
- Added persisted-topic recovery for the Shelley API bridge:
  - `POST /api/conversation/{id}/chat` now recreates the topic runtime from the `topics` table after a fresh server instance, then routes through the shared prompt queue.
- Added topic rename synchronization:
  - existing conversation rename updates both `conversations.slug` and `topics.topic_name`
  - active in-memory topic runtime maps are updated to the new topic name as well
- This prevents the current Shelley UI rename feature from silently breaking workspace topic routing.

### Legacy/backfill note
- There is still one narrow migration compromise:
  - older topic-like conversations created before the `topics` table existed are not bulk-backfilled automatically
  - they are lazily backfilled if accessed through the workspace topic routes by name
- Reason:
  - bulk-importing every top-level slugged conversation as a topic would wrongly classify legacy non-workspace conversations
- Result:
  - new topics now have durable identity immediately
  - historical pre-table topics need one workspace-route touch before restart-safe recovery exists

### Additional tests added
- workspace topics no longer list arbitrary legacy slugged conversations
- topic-backed API chat restores runtime from persisted topic metadata after a fresh server instance
- renaming a topic-backed conversation preserves workspace topic routing

### Validation update — durable topic checkpoint
- `go test ./server -run 'TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root

### 2026-03-10 update — Phase 2 mixed-stream proof
- Added explicit mixed-client SSE coverage in Go tests.
- New proof point:
  - websocket client sends a topic prompt on `/ws/topic/{name}`
  - Shelley's native `/api/conversation/{id}/stream` SSE channel for the same conversation receives the agent response
- This is the closest current automated check to the Phase 2 "browser + external client share a topic" behavior without adding Playwright/browser automation yet.

### Validation update — mixed-stream checkpoint
- `go test ./server -run 'TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./server` in `shelley/`

### 2026-03-10 update — workspace file endpoints
- Added workspace file routes rooted at the Shelley server process working directory for now:
  - `GET /ws/files`
  - `GET /ws/files/{path...}`
  - `PUT /ws/files/{path...}`
  - `DELETE /ws/files/{path...}`
- Implemented:
  - file read
  - file write with parent directory creation
  - file/directory delete
  - directory listing for root or any directory path
- Added path-safety checks so `..` segments are rejected instead of silently normalized.

### File endpoint design compromise
- The draft plan assumes a future `--workspace-dir` flag. The checked-out Shelley CLI still does not have that flag.
- For this checkpoint, workspace file APIs use the server process cwd as `workspaceRoot`.
- This keeps the implementation real and testable now, while leaving a clean seam to switch to `--workspace-dir` later.

### Validation update — file endpoint checkpoint
- `go test ./server -run 'TestWorkspaceFiles|TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root
  - now includes `PUT/GET/DELETE /ws/files/...` plus directory listing

### 2026-03-10 update — workspace tool metadata APIs
- Added persisted workspace tool and grant tables:
  - `db/schema/018-workspace-tools.sql`
  - `db/query/workspace_tools.sql`
- Added `/ws/tools` REST handlers for:
  - listing tools
  - creating tools
  - fetching one tool with its grants
  - deleting tools
  - creating grants
  - deleting grants
- Added focused server tests covering tool lifecycle and duplicate-name rejection.

### Tool API scope note
- This checkpoint is metadata and policy storage only.
- Connected tools are now durable and queryable through the workspace REST surface, but they are not yet injected into Shelley's live per-topic tool sets.
- Approval workflow and audit logging are also still future work; the current implementation stops at persisted tool/grant CRUD.

### Validation update — workspace tool metadata checkpoint
- `go test ./server -run 'TestWorkspaceTools|TestWorkspaceFiles|TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root
  - now includes `POST/GET/DELETE /ws/tools`

### 2026-03-10 update — live workspace tool injection
- Topic turns now refresh dynamic `workspace_*` tools from the persisted workspace tool/grant tables before each next prompt is handed to Shelley's loop.
- Added `loop.Loop.SetTools()` and `ConversationManager.SetExtraTools()` so active topic conversations can pick up tool changes on the next turn without restarting the conversation runtime.
- Current visibility rules:
  - a workspace tool is only exposed to the LLM when the topic has at least one matching non-denied grant
  - supported grant subjects today are `agent:*` and `agent:{topic}`
  - visible actions are limited to the granted subset of the tool's registered actions
- Execution is still intentionally narrow:
  - `workspace_*` tools now exist in the LLM tool list and enforce access mode at call time
  - actual external protocol execution is not implemented yet, so allowed calls currently return a clear "not implemented" tool error
  - `approval_required` also returns a clear not-yet-implemented error instead of silently allowing the call

### Plan adjustment surfaced by implementation
- The draft Phase 4 text suggests a `SetSystem()` update path alongside `SetTools()`.
- In the checked-out Shelley code, the actual system prompt text does not enumerate tool descriptions; tool descriptions live in system-message `display_data` for the UI and in the LLM request tool list itself.
- Practical result:
  - `SetTools()` was required for real next-turn behavior
  - `SetSystem()` is not required yet for correctness
  - new topic conversations do include workspace tools in their initial system-message display metadata because the extra tools are loaded before first hydrate
  - existing conversations' persisted system-message display metadata is still not rewritten when tools change

### Surprise caught during this slice
- My first pass moved topic tool refresh ahead of the point where the topic marked itself busy for a turn.
- That created a short regression where a second websocket prompt could miss the `queued prompt` system message.
- Fixed by marking the turn busy before refreshing workspace tools, then aborting the turn cleanly if refresh fails.

### Additional tests added
- active topic picks up a newly granted workspace tool on the next turn and drops it again after deletion
- topic-scoped grants (`agent:{topic}`) only expose the tool to the matching topic
- invalid grant access values are rejected at the REST layer

### Validation update — live workspace tool injection checkpoint
- `go test ./server -run 'TestWorkspaceTools|TestWorkspaceFiles|TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root

### 2026-03-10 update — workspace tool audit log
- Added a durable `workspace_tool_log` table in `db/schema/019-workspace-tool-log.sql`.
- Workspace tool calls now write audit entries capturing:
  - tool id
  - topic name
  - action
  - subject (`agent:{topic}`)
  - access decision
  - truncated input summary
- `GET /ws/tools/{tool}` now includes recent log entries in `.log`.

### Runtime behavior added in this slice
- Added a predictable-model test trigger:
  - prompt format `workspace_tool: <tool> <action>`
  - if the current request tool list includes `workspace_<tool>`, the predictable model emits a real tool call
- This made it possible to test the full path:
  - topic prompt
  - dynamic `workspace_*` tool exposure
  - tool execution entrypoint
  - access decision
  - audit log persistence
- Current decision logging behavior:
  - `allowed` grants log `allowed`
  - `approval_required` currently logs `denied` because approval flow is not implemented yet
  - missing/denied access also logs `denied` if the runtime path is reached

### Scope boundary
- The cumulative smoke script still only exercises workspace tool metadata endpoints live (`POST/GET/DELETE /ws/tools`).
- The new audit behavior is covered by Shelley server tests instead, because the checked-out Bun CLI has no direct built-in way to trigger a named `workspace_*` tool call.

### Additional tests added
- allowed workspace tool call produces a persisted audit log entry visible on `GET /ws/tools/{tool}`
- approval-required workspace tool call logs a denied decision

### Validation update — workspace tool audit log checkpoint
- `go test ./server -run 'TestWorkspaceTools|TestWorkspaceFiles|TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root

### 2026-03-10 update — `--workspace-dir`
- Implemented `serve --workspace-dir <path>` in Shelley.
- The configured workspace root now drives three things together instead of only the file API:
  - `/ws/files/...`
  - default per-conversation tool working directory
  - initial `cwd` for new topic-backed conversations created through workspace routes
- This closes one of the larger remaining drifts from the updated plan. Workspace mode no longer has to piggyback on the server process cwd to define its filesystem root.

### Validation added for workspace root
- Added server test coverage proving a workspace-created topic conversation persists its `cwd` as the configured workspace root.
- Extended the smoke script to launch Shelley with `serve --workspace-dir "$TMPDIR/workspace"` and verify file writes land in that host directory.

### Remaining limitation after this slice
- Existing non-topic Shelley conversations are still just Shelley conversations; there is still no first-class workspace table or multi-workspace server mode.
- `--workspace-dir` makes the single running Shelley process's workspace root explicit, but does not yet add cloning/snapshot/rollback semantics.

### Validation update — workspace root checkpoint
- `go test ./server -run 'TestWorkspaceTools|TestWorkspaceFiles|TestWorkspace|TestEmitWorkspace'` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root
  - now launches Shelley with `--workspace-dir`

### 2026-03-10 update — MCP-backed workspace tools
- Wired real MCP execution into the existing `workspace_<tool>` runtime wrapper using the official Go MCP SDK.
- Supported transports in this slice:
  - `stdio`
  - `streamable_http`
- The persisted workspace tool record is still the source of truth for:
  - which wrapper tool name Shelley exposes
  - which actions are visible to a given topic
  - approval and audit policy
- Runtime behavior added:
  - allowed and approved `protocol:"mcp"` workspace tools now connect to the configured MCP server and call the selected action as the remote MCP tool name
  - wrapper `input` is passed through as MCP tool arguments and must currently be a JSON object
  - MCP text content is returned to Shelley as normal `llm.ContentTypeText`
  - MCP structured content is preserved by converting it to JSON text content instead of dropping it
  - non-text MCP content is not rendered specially yet; it is funneled through the same typed conversion path and serialized to JSON text for now

### Notes from this slice
- This is deliberately execution-first, not discovery-first.
- Shelley does not yet sync or infer the remote MCP server's tool catalog.
- The `actions` array stored in the workspace tool record must still be populated manually, and those action names must match remote MCP tool names.
- MCP sessions are currently created fresh per tool call.
  - This keeps the runtime simple and made the first transport integration straightforward.
  - It also means there is no session pooling, warm connection reuse, or long-lived server push handling yet beyond what the Go SDK transport establishes for a single call.

### Tests added
- stdio MCP workspace tool executes successfully and returns text content
- streamable HTTP MCP workspace tool executes successfully and returns text plus structured-content fallback
- invalid non-object wrapper input is rejected before calling MCP
- streamable HTTP MCP workspace tool is exercised through a real Shelley topic turn, not just by calling the runtime tool directly

### Small fixture improvement during MCP testing
- Extended the predictable model test fixture with `workspace_tool_json: <tool> <action> <json>` so end-to-end topic tests can drive a workspace tool call with structured wrapper input.
- Also fixed the shared `sendTopicAPIChat` test helper to JSON-encode request bodies instead of interpolating raw strings, which avoids breaking tests when prompts contain quotes.

### Approval-path note
- Added websocket approval coverage for an MCP-backed workspace tool.
- One runtime gap showed up while doing this:
  - after an approved websocket-originated MCP call, the topic websocket reliably shows the follow-on `tool_call` and final `done`
  - the persisted tool result is also present in the conversation history
  - but this path did not emit a translated `tool_update` before `done` in my test run
- I kept the passing test coverage on the stable behavior above and left the missing `tool_update` translation as follow-up work rather than baking in a failing assertion.

### Validation update — MCP workspace tools checkpoint
- `go test ./server -run 'TestWorkspaceToolMCP|TestWorkspaceToolCallsAreLogged|TestWorkspaceToolApproval' -v` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root
  Interfaces I’d Strengthen First

  - REST resource model:
      - Add first-class workspaceId, topicId, and probably stable toolId semantics that are distinct from names/slugs.
      - Version the public API explicitly. Right now /api/*, /ws/*, /workspaces, and /acp/* are mixed for compatibility.
      - Return richer metadata on public resources: protocol version, capabilities, created/updated timestamps, status/health, and maybe deprecation markers for
        compatibility routes.
  - Topic/WebSocket protocol:
      - Add an envelope with eventId, topicId, turnId, timestamp, and protocolVersion.
      - Add resumability/ordering primitives. Right now the WSS stream is not like Shelley’s SSE stream; there is no real sequence/resume contract.
      - Make prompt lifecycle explicit: accepted, queued, started, completed, cancelled. Today queued prompt is just a system message, which is too ad hoc for a real
        protocol.
      - Keep content typed from the start. Right now we mostly flatten tool results and MCP structured payloads into text. Even if we ignore images for now, the wire format
        should already support typed content parts.
  - Tool API:
      - Separate “tool definition”, “workspace connection/binding”, and “grant/policy”. We currently blur those together in one record.
      - Formalize action descriptors. actions: []string is enough for the draft spec, but real model interoperability wants name, description, inputSchema, and eventually
        output typing.
      - Make transport config a typed union instead of opaque config. mcp plus transport-specific settings should be modeled, validated, and inspectable.
      - Add tool status/health fields. A registered tool should expose whether Shelley can currently reach it.
      - Approval should have a first-class approvalId, not just reuse toolCallId.
  - Identity and policy:
      - subject: "agent:*" and approver emails are useful placeholders, but not a durable security model. We need real principals, actor types, and auth tied to both REST
        and websocket connections before multi-user use.
      - scope: any is flexible, but too underspecified. If it stays open-ended, we need a convention for how scope is interpreted and audited.


### 2026-03-10 update — workspace tool API shape and websocket tool updates
- Fixed the previously noted missing `tool_update` on the workspace websocket approval path.
- Root cause:
  - Shelley persists tool results as `user` messages when the underlying LLM message role is `user`, even when the message content is a tool result.
  - The workspace websocket adapter had only been translating persisted `tool` messages into `tool_update`.
  - Result: the tool result was present in conversation history, but the workspace websocket client never saw the corresponding `tool_update`.
- Fix:
  - The adapter now translates tool-result content from both persisted `user` and persisted `tool` message types.
  - Re-tightened the approval-path MCP websocket test to require the `tool_update` message again.

### Hosted `/ws/tools` registration changes
- Kept the hosted Shelley tool registry as the source of truth; no remote MCP catalog mirroring was added.
- Extended `POST /ws/tools` to accept either:
  - the existing spec-compatible `actions: ["read", "write"]`
  - or richer action objects carrying `name`, `description`, and `inputSchema`
- The richer action form is normalized into the existing `workspace_tools.actions` JSON column, so this slice did not need a new DB migration.
- `GET /ws/tools` and `GET /ws/tools/{tool}` now continue to expose `actions` as simple names, and also expose `actionDefs` when richer metadata is available.
- Shelley runtime tool exposure now uses that richer metadata when present:
  - per-action descriptions are included in the `workspace_<tool>` description shown to the model
  - the wrapper input schema narrows by visible action and carries through per-action `inputSchema`
- Validation added for invalid action schemas so clearly non-object `inputSchema` payloads are rejected at registration time.

### Compatibility note
- The protocol/spec draft still shows `actions` as a string array.
- Shelley now accepts that form unchanged and treats richer action objects as a backward-compatible extension for better runtime schema fidelity.

### Validation update — tool registration + websocket translation checkpoint
- `go test ./server -run 'TestWorkspaceToolMCP|TestWorkspaceTools|TestEmitWorkspace' -v` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root

### 2026-03-10 update — RFC directory
- Added `docs/rfcs/` as a place for narrower design decisions that should not be buried in the plan or the running journal.
- Added the first RFC:
  - `0001-workspace-manager-runtime-handoff.md`
- The RFC was then narrowed after re-reading the draft spec:
  - it no longer proposes root-scoped runtime routes as an equal public protocol form
  - it now focuses specifically on clarifying the manager response as a handoff object
  - it stays aligned with the draft's canonical namespaced `/apis/v1/...` resource model

### 2026-03-10 update — workspace route cleanup
- Cleaned up Shelley’s workspace route wiring so the code now treats `/ws/*` as the canonical workspace runtime REST surface.
- Kept the following compatibility routes, but moved them into an explicit compatibility registration path instead of leaving them mixed into the main workspace route block:
  - root wmlet-style runtime routes like `/topics` and `/health`
  - ACP websocket aliases under `/acp/*`
  - the single-workspace `/workspaces` manager shim used by the checked-out Bun CLI
- Updated the workspace manager discovery response so `api` now points at the canonical `/ws` base instead of the server root.
  - This lets the Bun CLI consume canonical `/ws/topics` through discovery without changing the CLI itself.
- Switched Shelley’s own workspace tests and helpers to use `/ws/topics` by default.
  - Coverage for the legacy root `/topics` path remains in a focused compatibility test instead of leaking aliases through the rest of the suite.

### Validation update — workspace route cleanup checkpoint
- `go test ./server -run 'TestWorkspace|TestEmitWorkspace' -v` in `shelley/`
- `go test ./db ./server` in `shelley/`
- `./test/smoke.sh` from the workspace root

### 2026-03-10 update — follow-on protocol RFCs
- Added three more RFC drafts to pin down the areas where Shelley implementation work is already forcing protocol choices:
  - `0002-topic-realtime-wire-contract.md`
  - `0003-workspace-tool-api-payloads.md`
  - `0004-approval-workflow-semantics.md`
- These are intentionally grounded in three inputs together:
  - the draft protocol prose in `agentic-workspace/agent-workspace.md`
  - the Bun reference implementation behavior in `reference-impl/`
  - Shelley's own implementation pressure and surprises while building a compatible server
- Deliberately did not write commits/files RFCs yet; those remain future-facing and less informed by the current interoperability slice.

### 2026-03-10 update — RFC 0001 reframed around control plane vs runtime plane
- Rewrote `0001-workspace-manager-runtime-handoff.md` to focus on the actual protocol crux:
  - the Workspace Manager should create and locate workspaces
  - it should not be required to host or proxy the runtime APIs of those workspaces
- The revised proposal now recommends:
  - Manager lifecycle routes remain the control plane
  - Manager create/get responses return a handoff object with stable workspace identity plus runtime REST and ACP endpoints
  - runtime REST routes are defined relative to the returned runtime API base instead of being implicitly tied to the Manager host/path space
- This is a better fit for likely Shelley deployment as well as VM/container-style managers that simply provision a runtime and hand back its address.

### 2026-03-10 update — RFC trust model cleanup
- Tightened the RFCs around authentication and approval identity after reviewing the control-plane/runtime split more critically.
- `0001-workspace-manager-runtime-handoff.md` now says the split is compatible with security only if runtime requests carry identity the runtime can verify:
  - shared issuer / direct token validation
  - Manager-minted runtime token
  - or trusted gateway forwarding identity
- `0002-topic-realtime-wire-contract.md` and `0004-approval-workflow-semantics.md` no longer let clients self-assert `approver` inside `approval_response`.
  - the client now sends only the decision payload
  - the runtime binds that decision to the authenticated caller and emits the approver identity only in server-originated events / audit records

### 2026-03-10 update — RFC 0001 deferred
- Removed `0001-workspace-manager-runtime-handoff.md` from the active RFC set.
- Reason:
  - the manager/runtime handoff question is real, but it is not on the current critical path for Shelley interoperability work
  - the more immediate protocol pressure is in topic realtime behavior, tool payloads, and approval semantics
- Kept the current implementation bias:
  - do not force the manager to proxy all runtime traffic
  - but do not spend more spec surface on that question until it becomes blocking

### 2026-03-10 update — `shelleymanager` proxy slice
- Added a new `shelleymanager/` Go module that acts as a public control plane plus reverse proxy for isolated single-workspace Shelley runtimes.
- Current manager behavior:
  - creates one Shelley runtime per workspace
  - keeps the runtime on a private loopback address
  - exposes public manager-style routes and proxies them to the runtime's internal `/ws/*` surface
- Implemented public routes:
  - manager health: `/health`
  - compatibility manager: `/workspaces`, `/workspaces/{name}`
  - canonical manager shape: `/apis/v1/namespaces/{ns}/workspaces...`
  - public topic websocket: `/acp/{ns}/{workspace}/topics/{topic}`
- Implemented proxy mapping:
  - public `/apis/v1/namespaces/{ns}/workspaces/{name}/topics...` -> runtime `/ws/topics...`
  - public `/apis/v1/namespaces/{ns}/workspaces/{name}/tools...` -> runtime `/ws/tools...`
  - public `/apis/v1/namespaces/{ns}/workspaces/{name}/files...` -> runtime `/ws/files...`
  - public websocket `/acp/{ns}/{workspace}/topics/{topic}` -> runtime `/ws/topic/{topic}`
- Kept the Bun CLI-compatible discovery shape on the manager:
  - `GET /workspaces/{name}` returns `api` rooted at `/workspaces/{name}`
  - `acp` rooted at `/workspaces/{name}/acp`
  - manager proxies those compatibility paths to the same runtime underneath

### Launch isolation options
- The manager runtime launcher is configurable rather than Docker-hardcoded.
- Initial launch modes:
  - `process`
  - `docker`
  - `bwrap`
- Validation today is strongest for:
  - `process` mode end-to-end in smoke
  - `docker` / `bwrap` command construction in unit tests
- Design intent:
  - the manager owns public ingress
  - the launcher owns how a Shelley runtime is isolated and started
  - the proxy layer does not care whether isolation is a subprocess, container, or bubblewrap sandbox

### Bug caught during manager bring-up
- The first live manager create attempt failed while precreating topics.
- Root cause:
  - JSON decode fallback from `topics: [{"name":"..."}]` first tried `[]string`
  - Go left a zero-value string element behind even on unmarshal error
  - the fallback object path appended to that slice instead of resetting it
  - result: the manager tried to create an empty topic name before the real one
- Fixed by clearing the partially filled slice before decoding object-form topics.
- Added a regression test for object-form topic decoding.

### Validation update — `shelleymanager`
- `cd shelleymanager && go test ./...`
- `./test/smoke.sh`
  - now builds both `shelley` and `shelleymanager`
  - starts `shelleymanager`
  - creates a workspace via `POST /apis/v1/namespaces/{ns}/workspaces`
  - verifies proxied files and tools through the public manager routes
  - runs the Bun CLI through manager `/workspaces` discovery and public proxied websocket traffic

### 2026-03-10 update — tightened `bwrap` isolation
- Reworked the `bwrap` launcher after looking at it from the filesystem-isolation angle.
- The first cut only put Shelley in a new mount namespace while still exposing the host root directly; that was not good enough.
- Current `bwrap` shape:
  - one per-workspace host root is created under manager state
  - that root is mounted into the sandbox at `/sandbox`
  - Shelley runs from a copied binary inside `/sandbox/bin/shelley`
  - workspace files, DB, temp dir, and home dir all live under `/sandbox`
  - host system directories like `/usr`, `/bin`, `/lib*`, and `/etc` are mounted read-only only as needed for execution
  - `/tmp` inside the sandbox is bound to the workspace-local temp dir under manager state
- Result:
  - writes inside the workspace root work
  - writes to `/tmp` stay inside the workspace-local sandbox temp
  - the host filesystem outside the mounted workspace/state root is not used as a writable surface by the runtime

### Validation update — live `bwrap` smoke
- `SMOKE_RUNTIME_MODE=bwrap ./test/smoke.sh`
  - passed end to end
  - manager created a Shelley workspace in `bwrap` mode
  - Bun CLI connected and chatted through the manager websocket proxy
  - a predictable `bash:` turn wrote `bwrap-inside.txt` inside the mounted workspace
  - the same turn wrote `/tmp/<name>` only inside the sandbox-local temp dir
  - confirmed the corresponding host `/tmp/<name>` path was untouched

### 2026-03-10 update — demo-ready RFC contracts
- Rewrote the three active protocol RFCs from exploratory drafts into concrete demo contracts:
  - `0002-topic-realtime-wire-contract.md`
  - `0003-workspace-tool-api-payloads.md`
  - `0004-approval-workflow-semantics.md`
- Main decisions now pinned:
  - topic websocket replay is defined around a live `sessionId` plus `since=<eventId>` catchup
  - a no-`since` connect must replay the active turn and unresolved approvals
  - the hosted tool API is the canonical source of truth for the demo
  - tool payloads are fixed around hosted MCP registrations with `stdio` and `streamable_http`
  - approval is websocket-first, first-valid-response-wins, and identity comes from auth context rather than client-declared approver fields
- Explicitly deferred for the demo:
  - durable replay across runtime restart
  - registry sync / remote tool mirroring
  - REST approval endpoints for offline approvers

### 2026-03-10 update — concrete demo narrative
- Wrote `docs/demo-run-of-show.md` as a specific presenter runbook instead of a generic outline.
- Chosen story:
  - workspace: `bp-ig-fix`
  - namespace: `acme`
  - repo/template: `acme-rpm-ig`
  - topic: `bp-panel-validator`
  - browser participant: Priya Shah
  - CLI participant: Marco Ruiz
- The narrative now uses one believable FHIR standards task end to end:
  - Shelley runs a local `fhir-validator` tool against `input/fsh/BloodPressurePanel.fsh`
  - the validator reports a concrete missing-slicing error on `Observation.component`
  - a late CLI joiner catches up and uses MCP stdio `hl7-jira`
  - Shelley adds explicit FSH slicing declarations, re-runs validation, then pauses on an approval-required `publish-preview`
- Also tightened `0003-workspace-tool-api-payloads.md` so the nested `tools` array is described as MCP-native tool definition objects rather than an ad hoc hosted action schema.
- Practical goal:
  - the demo document is now specific enough to drive fixture building, UI polish, and presenter rehearsal without inventing the story live on stage.

### 2026-03-10 update — shared tools mount for isolated runtimes
- Added first-class manager support for an optional shared host tools directory.
- New launcher behavior:
  - `shelleymanager` accepts `-tools-dir <host-path>`
  - `process` runtimes inherit `WORKSPACE_TOOLS_DIR=<host-path>`
  - `docker` runtimes mount that host path read-only at `/tools` and inherit `WORKSPACE_TOOLS_DIR=/tools`
  - `bwrap` runtimes `--ro-bind` that host path at `/tools` and inherit `WORKSPACE_TOOLS_DIR=/tools`
- Purpose:
  - avoid baking demo tools into workspace creation time
  - give isolated workspaces a stable shared location for items like:
    - FHIR Validator JAR wrapper
    - Bun binary
    - HL7 Jira MCP fixture app
- Validation:
  - `cd shelleymanager && go test ./...`
  - `./test/smoke.sh`
- This surfaced one remaining demo gap clearly:
  - the manager/runtime filesystem path is now straightforward, but Shelley still only executes workspace tools through the `mcp` protocol
  - the demo's `fhir-validator` story therefore still needs a local executable tool path, likely an `exec`-style workspace tool protocol, rather than only more launcher work

### 2026-03-10 update — stdio MCP command resolution from shared tools
- Tightened Shelley's stdio MCP execution path so bare command names resolve from `$WORKSPACE_TOOLS_DIR/bin` before falling back to ambient `PATH`.
- Practical effect:
  - if a workspace runtime has a shared tools mount exposing `/tools/bin/bun`, an MCP stdio registration can use `"command":"bun"` and Shelley will find it
  - this reduces coupling between hosted MCP registrations and global host installs
- Kept the fallback behavior:
  - absolute command paths still work
  - slash-containing relative paths are still passed through as-is
  - normal `PATH` lookup still works when the shared tools dir does not contain the command
- Validation:
  - `go test ./server -run 'TestWorkspaceToolMCP'` in `shelley/`
  - `go test ./server` in `shelley/`
  - `./test/smoke.sh`
- Design consequence for the demo:
  - MCP tools now have a better runtime story with shared tool bundles or system installs
  - the remaining modeling question is specifically about trusted local non-MCP tools such as `fhir-validator` and `ig-publisher`, not about MCP stdio transport itself

### 2026-03-10 update — simplified live demo tool story
- Updated `docs/demo-run-of-show.md` to make the tool model explicit.
- The mainline demo now intentionally shows only two paths:
  - trusted local runtime tools reachable through bash, using `fhir-validator`
  - managed MCP workspace tools, using `hl7-jira`
- I removed approval from the core run-of-show and moved it to an optional follow-on demo.
- Reason:
  - mixing late join, local tools, MCP tools, and approval in one short live story made the architecture harder to explain
  - the simpler story better matches the current implementation boundary:
    - trusted local bundles are a manager/runtime concern
    - MCP tools are the first-class workspace tool API story
- I also made the narration clearer that stdio MCP runs inside the bubblewrapped runtime, typically via `npx`, rather than as a host-side subprocess outside isolation.

### 2026-03-10 update — RFC for local tool catalog
- Added `docs/rfcs/0005-local-tool-catalog.md`.
- Decision captured there:
  - trusted local runtime tools should not be modeled as opaque hidden manager strings
  - the manager publishes a local tool catalog at `GET /apis/v1/local-tools`
  - workspace configuration selects from that catalog with `runtime.localTools: [...]`
  - workspace detail should expose the resolved local tool metadata back to clients
- This keeps the demo simple without introducing a packaging standard:
  - `fhir-validator` comes from the manager-published local tool catalog
  - `hl7-jira` still comes from the workspace tools API as MCP
- Updated `docs/demo-run-of-show.md` accordingly so the demo now explicitly starts with the manager catalog before workspace creation.

### 2026-03-10 update — demo manager flow and SDK-backed Bun MCP fixture
- Implemented the demo-critical manager flow:
  - manager home page at `/`
  - manager topic page at `/app/{namespace}/{workspace}/{topic}`
  - published local tool catalog at `GET /apis/v1/local-tools`
  - workspace create path that accepts `runtime.localTools`
  - demo workspace seeding for `acme-rpm-ig`
- Selected local tools are now treated as manager-controlled runtime capabilities:
  - manager resolves requested `runtime.localTools` against its published catalog
  - launcher mounts only those selected tool roots into the runtime
  - manager writes `.shelley/AGENTS.md` describing the mounted bash-visible commands so Shelley knows they exist
- Added a concrete demo fixture for `fhir-validator` under `test/fixtures/local-tools/`:
  - command name: `fhir-validator`
  - behavior: reports the missing `Observation.component` slicing metadata until the profile is fixed
  - this is intentionally a local runtime tool reachable via bash, not a managed workspace tool registration
- Replaced the hand-rolled HL7 Jira MCP fixture with a real Bun MCP server using the official JavaScript SDK:
  - script: `shelleymanager/manager/testdata/hl7-jira-mcp.js`
  - server library: `@modelcontextprotocol/sdk/server/*`
  - backing store: `bun:sqlite`
  - data shape: a tiny SQLite database created in `.demo/hl7-community-search.sqlite` on first run
  - current fixture issues include `FHIR-53953`, `FHIR-53960`, `FHIR-51091`, and `FHIR-31709`
- The manager UI now does the real demo setup path:
  - create workspace with `runtime.localTools`
  - write `.demo/hl7-jira-mcp.js` into the workspace through the files API
  - register `hl7-jira` through `POST /tools`
  - grant `jira.search` through `POST /tools/{tool}/grants`
- Tightened topic catchup for the late-join demo:
  - websocket topic connect now replays translated persisted topic messages
  - persisted tool results are translated into both `tool_update` and `text`
  - reason: the checked-out Bun CLI only prints `text` and would otherwise hide successful tool output during replay

### Validation update — full demo path
- Focused Shelley MCP fixture validation:
  - `go test ./server -run TestWorkspaceToolMCPStdioBunFixtureFromWorkspace -v`
- Full Shelley server validation:
  - `go test ./server`
- Full manager validation:
  - `cd shelleymanager && go test ./...`
- Full end-to-end demo smoke in process mode:
  - `./test/smoke.sh`
- Full end-to-end demo smoke in `bwrap` mode:
  - `SMOKE_RUNTIME_MODE=bwrap ./test/smoke.sh`
- The smoke path now proves all of the intended demo beats:
  - local tool catalog discovery
  - workspace creation with `runtime.localTools`
  - served manager web pages
  - late CLI join replay
  - `fhir-validator` local tool execution through bash
  - `hl7-jira` MCP stdio execution through Bun inside the workspace runtime
  - manager proxying for both REST and topic websocket traffic

### Remaining edge / design note
- The current demo keeps `fhir-validator` as a mounted wrapper script rather than a real validator JAR plus explicit JRE dependency model.
- In `bwrap` mode, host system runtime binaries such as `bun` and `java` are visible because the launcher mounts standard host runtime directories like `/usr` and `/bin` read-only into the sandbox.
- That is good enough for the current demo, but if local tool catalog entries later need explicit transitive runtime dependencies, the catalog model will need to grow beyond `commands + guidance + requirements`.

### 2026-03-10 update — RFC corrections from implementation
- Updated `0002-topic-realtime-wire-contract.md` to better match the demo path we actually proved:
  - bounded catchup-on-connect is required
  - exact `since=<eventId>` resumability is deferred
  - replay may come from a translated durable transcript, not only an in-memory event buffer
  - runtimes may emit a compatibility `text` event for human-readable tool results
- Updated `0003-workspace-tool-api-payloads.md` to clarify the demo-proven stdio MCP shape:
  - `transport.command` runs inside the workspace runtime
  - relative `cwd` resolves from the workspace root
  - workspace-local Bun scripts are a valid stdio MCP registration pattern
  - hosted `tools` metadata is authoritative for the workspace and does not require automatic remote MCP tool mirroring
- Updated `0005-local-tool-catalog.md` to capture the runtime dependency lesson from the demo:
  - catalog entries may expose descriptive `requirements`
  - clients select only the local tool itself, not its transitive dependencies
  - managers may satisfy those dependencies through either selected mounts or base runtime binaries

### 2026-03-10 update — live demo sync fixes
- Fixed a real topic-wire gap in Shelley:
  - websocket translation had been dropping normal user messages
  - result: prompts sent from the lightweight topic page or Shelley's native UI did not show up in the other client
  - `server/workspace_adapter.go` now emits `{"type":"user"}` for persisted user text so the topic websocket is no longer assistant-only
- Added regression coverage in Shelley for both directions of the shared-topic demo:
  - API chat now has an assertion that websocket clients receive the user prompt
  - websocket replay on connect now has an assertion that earlier user prompts are replayed too
- Tightened the manager demo UI to behave like a real shared client instead of a fake local echo:
  - the simple topic page now waits for server-originated `user` events rather than appending a local optimistic echo
  - message rendering now uses DOM nodes plus `textContent` instead of `innerHTML`
  - reason: keep the demo clients consistent with one another and avoid writing a trivial XSS sink into the page
- Fixed two demo-entry bugs that were making manual testing noisy:
  - the manager home page now bootstraps from escaped HTML attributes instead of fragile inline JS string quoting
  - the Jira MCP fixture script is now served from `/demo-assets/hl7-jira-mcp.js` instead of being embedded into the page
- Verified live against the running demo:
  - websocket-originated prompts persist into Shelley and appear in the native Shelley UI
  - runtime API / native Shelley UI prompts emit `user` events onto the manager topic websocket
  - the manager `Open Shelley UI` link now resolves to the correct Shelley conversation route `/c/{topic}`

### 2026-03-10 update — browser websocket + manager UI lifecycle controls
- Found and fixed the real reason the lightweight topic page stayed blank in Chromium while the CLI worked:
  - the manager websocket proxy was forwarding the browser `Origin` header for the public manager host straight through to the private Shelley runtime
  - Shelley's websocket accept path treated that as cross-origin and returned `403`
  - the manager now rewrites proxied runtime `Origin` to the runtime origin and preserves the original browser origin in `X-Forwarded-Origin`
- Added coverage at the manager layer for the exact browser-origin failure mode:
  - `manager_test.go` now dials the manager ACP websocket with an explicit browser-style `Origin` header and asserts that the proxied runtime accepts it
  - the smoke path now includes an actual headless Chromium check that the topic web UI reaches `Connected`
- Tightened the lightweight topic page UX:
  - it now shows `Connecting...` first
  - failed websocket setup becomes a visible `Realtime connection failed` message instead of silently collapsing to a blank page plus `Disconnected`
  - sending a prompt while the websocket is not open now yields an explicit visible error
- Added the missing manager UI lifecycle controls requested during manual testing:
  - home page workspace cards now list all topics
  - each workspace card can create a new topic through the real manager API
  - each listed topic can be deleted from the home page
  - each workspace can be deleted from the home page
  - the topic page itself now also exposes `Delete Topic`
- Confirmed the manager can be launched on a fixed port for the manual demo:
  - `shelleymanager -listen 127.0.0.1:31337 ...`
  - the random-port mode is optional, not required

### 2026-03-10 update — new RFC for collaborative queued prompts
- Added `docs/rfcs/0006-topic-prompt-queue-management.md`.
- Reason:
  - the live demo clarified that simple serialized prompt queuing is better than hard mutex or prompt dropping, but still not good enough if queued work is invisible and not cancellable
  - we want submitters to be able to inspect and remove their own queued prompts before execution starts
- The RFC takes the position that:
  - each submitted prompt becomes an explicit queue entry with stable `promptId`
  - queue state should be visible over both websocket and REST
  - reconnecting clients should receive a `queue_snapshot`
  - callers should be able to cancel or clear their own queued prompts while they remain queued

### 2026-03-10 update — queue RFC implemented in Shelley and the Bun CLI
- Shelley topic runtimes now expose prompt queue state as a real public surface instead of an internal FIFO plus the old `queued prompt` system text.
- Runtime-side changes:
  - queued prompts now have stable `promptId`
  - topics emit `prompt_status` lifecycle events for `accepted`, `queued`, `started`, `completed`, `cancelled`, and `failed`
  - websocket connect now emits `queue_snapshot`
  - queued prompts can be removed before start through:
    - websocket `cancel_prompt`
    - `DELETE /topics/{topic}/queue/{promptId}`
    - `POST /topics/{topic}/queue:clear-mine`
  - queue ownership currently uses a lightweight requester identity placeholder:
    - websocket `client_id` query parameter
    - REST `X-Workspace-Client-ID` header
  - this is intentionally a prototype auth context, not the final security model
- Bun CLI changes:
  - `connect` now uses a stable per-process participant id and sends explicit `promptId`
  - CLI prints queue lifecycle and queue snapshots
  - added `queue` and `clear-queue` commands
  - interactive `/queue`, `/cancel <promptId|last>`, `/clear`, and `/whoami` are now available while connected
- Lightweight manager topic page changes:
  - browser websocket connections now also carry a stable participant id
  - browser prompt sends now include explicit `promptId`
  - queue events no longer fall through as raw unknown-event output

### Validation update — queue implementation
- Shelley focused queue tests:
  - `go test ./server -run 'TestWorkspaceTopicWSQueuesPrompt|TestWorkspaceTopicQueueRESTAndCancellation|TestWorkspaceTopicAPIChatUsesTopicQueue|TestWorkspaceTopicWSReplaysRecentMessagesOnConnect' -v`
- Full Shelley server suite:
  - `go test ./server`
- Full manager suite:
  - `cd shelleymanager && go test ./...`
- Full smoke in process mode:
  - `./test/smoke.sh`
- Full smoke in `bwrap` mode:
  - `SMOKE_RUNTIME_MODE=bwrap ./test/smoke.sh`
- New smoke coverage now explicitly proves:
  - second prompt becomes queued behind an active turn
  - `GET /topics/{topic}/queue` shows active and queued prompt ids
  - `ws queue` surfaces that queue state in the Bun CLI
  - `DELETE /queue/{promptId}` removes the queued prompt through the public manager path

### 2026-03-10 update — root repo now uses real submodules
- Created/published GitHub targets under the `jmandel` account:
  - `jmandel/shelley` already existed as a fork
  - created `jmandel/agentic-workspace` as a fork
  - created `jmandel/workspace-protocol` for this root project
- Pushed branch `workspace-protocol-queue-demo` to:
  - `jmandel/shelley`
  - `jmandel/agentic-workspace`
- Converted the root repo to proper submodules:
  - `shelley` now points at `https://github.com/jmandel/shelley.git`
  - `agentic-workspace` now points at `https://github.com/jmandel/agentic-workspace.git`
- Added a root `.gitignore` for local demo artifacts so the root repo only tracks the real project state.

### Remaining demo follow-up
- Extend the predictable model so demo prompts can deterministically trigger:
  - local `bash` flows that exercise mounted FHIR tooling
  - MCP tool calls such as the HL7 Jira fixture
- The goal is to make the live demo show real tool traffic without depending on a non-deterministic model.

### 2026-03-10 update — protocol RFCs moved into `agentic-workspace`
- The canonical RFC set now lives in `agentic-workspace/rfcs/` instead of this
  root scratch repo.
- Rationale:
  - the RFCs are protocol-facing, not Shelley-manager implementation notes
  - keeping them next to `agent-workspace.md` makes the spec easier to evolve in
    one place
- The root `docs/rfcs/README.md` now just points at the protocol repo copy.
