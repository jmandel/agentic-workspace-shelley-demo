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
- This RFC captures the design distinction that came up in discussion:
  - Shelley can reasonably remain a single-workspace runtime
  - the protocol gap is the missing handoff between a manager API that creates workspaces and a runtime API that serves one workspace
