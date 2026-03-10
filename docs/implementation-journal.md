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
