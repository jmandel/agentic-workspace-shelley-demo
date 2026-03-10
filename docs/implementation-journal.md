# Implementation Journal

## 2026-03-10

### Scope chosen
- Re-read `docs/plan.md` against the checked-out `shelley` and `agentic-workspace` repos before coding.
- Did not start with the full workspace-spec surface from the draft. The Bun reference implementation and CLI only require a much smaller real interface first: `GET /health`, `GET|POST /topics`, `GET|DELETE /topics/{name}`, and `WS /acp/{topic}`.
- Chose to build that compatibility slice inside Shelley first, backed by the existing conversation runtime, instead of inventing new workspace/topic tables immediately.

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

### Surprises / deviations
- Archiving in Shelley keeps the conversation row, while wmlet's topic deletion semantics allow reconnecting with the same topic name later. To keep topic names reusable without losing history, I restored archived conversations on re-create / reconnect instead of creating a brand new conversation.
- Shelley records whole assistant/tool messages, not token chunks. The first adapter therefore emits websocket `text` events at message boundaries rather than token-by-token streaming. That is enough for Bun CLI compatibility but not full ACP-like granularity.
- Shelley also batches queued user prompts into the next LLM request if they arrive before that request is dispatched. So `queued prompt` is true in the sense of "accepted and not dropped", but not yet true in the stricter wmlet sense of "guaranteed separate next turn". This needs a deeper runtime change if strict per-prompt turn boundaries are required.
- Joining an existing topic over the wmlet-compatible websocket does not replay full history yet. The current adapter subscribes to new events only. Persistence exists underneath; history replay is still a follow-up item.
- The draft plan's validation commands are stale relative to the checked-out `agentic-workspace` repo:
  - `cli.ts connect ...` does not connect directly to wmlet; it first does manager discovery via `/workspaces/{name}`.
  - piping a prompt into `cli.ts connect` before the websocket is connected loses the input; the client currently drops pre-connection stdin.
  - the plan documents `/ws/...` routes, but current `wmlet.ts` serves `/topics`, `/health`, and `/acp/...` at the root.
  - the draft smoke script uses `serve --workspace-dir` and `--agent` flags that do not exist in the checked-out Shelley CLI yet.
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
