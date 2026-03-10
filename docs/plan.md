# Shelley Workspace: Design & Implementation Plan

Turning Shelley into a multiplayer workspace runtime that implements the
Agentic Workspace Protocol.

**Audience:** A developer who has just cloned both repositories and has
not yet read either codebase or the workspace protocol spec.

**Repositories:**
- `github.com/boldsoftware/shelley` — a coding agent (Go + TypeScript/React)
- `github.com/niquola/agentic-workspace` — a protocol spec + reference implementation (TypeScript/Bun)

---

## Part I — Context

### What Shelley Is Today

Shelley is a single-user coding agent. You run `go run ./cmd/shelley serve`,
open a browser, and talk to an AI that can edit code, run bash commands, search
files, browse the web, and spawn sub-agents. The stack:

- **Go backend** (`server/`): HTTP server, conversation management, system
  prompt generation, git integration, notification dispatch (Discord, email,
  ntfy). Uses `net/http`, `github.com/coder/websocket` for terminal WebSocket.
- **SQLite database** (`db/`): Conversations, messages (user/agent/tool/system
  types), usage tracking, custom models, notification channels. Schema managed
  via embedded SQL migrations. Connection pool with 1 writer + 3 readers.
- **Agentic loop** (`loop/`): Core turn processor. Sends conversation history
  to an LLM, receives streaming response, executes tool calls, records
  messages, repeats until the model ends its turn. Supports interruptions
  (queued user messages injected between tool results). Handles prompt caching,
  retry on transient errors, missing tool result repair.
- **LLM abstraction** (`llm/`): Unified `Service` interface with
  implementations for Anthropic (`ant/`), OpenAI (`oai/`), and Gemini (`gem/`).
  Streaming responses, token counting, image handling.
- **Tool system** (`claudetool/`): Bash execution, file patching (with
  simplified schema for weaker models), keyword search (grep + LLM relevance),
  browser automation, sub-agents, LLM one-shot calls, output iframes. Each
  conversation gets its own `ToolSet`.
- **Web UI** (`ui/`): React (TSX) with esbuild. `App.tsx` manages
  conversation list and routing. `ConversationDrawer.tsx` is the sidebar.
  `ChatInterface.tsx` is the main chat view. `MessageInput.tsx` handles
  prompting. Messages stream via SSE (`/api/conversation/{id}/stream`).
  Prompts go via POST (`/api/conversation/{id}/chat`). Tool results render
  as specialized components (`BashTool`, `PatchTool`, `KeywordSearchTool`,
  etc.).
- **Other infrastructure**: Pub/sub system (`subpub/`) for real-time
  broadcasting to SSE subscribers. Conversation distillation (`server/distill.go`)
  that compresses long conversations into operational summaries. Skills system
  (`skills/`) that discovers `SKILL.md` files. Git state tracking per turn.
  Notification dispatcher with pluggable channels.

The key architectural fact: Shelley talks directly to LLM APIs. There is no
intermediate agent process. The Go server _is_ the agent — it runs the loop,
calls the tools, streams the results.

### What the Agentic Workspace Protocol Is

The Agentic Workspace Protocol (defined in `agent-workspace.md`) specifies a
multiplayer environment where humans and AI agents collaborate on shared
resources.

**Core concepts:**

A **workspace** is an isolated environment with a filesystem, an identity
(email-like address), participants (humans and agents), tools with access
policies, and versioned state (commit/rollback/clone). Workspaces are
declaratively defined and runtime-agnostic.

A **topic** is a named conversation thread within a workspace. Each topic has
exactly one agent instance with its own conversation context. Multiple humans
can observe and send prompts to the same topic. Topics are the unit of
parallelism — two topics run two independent agents on the same filesystem.

**ACP (Agent Client Protocol)** is the wire protocol between a client and an
agent. It is JSON-RPC 2.0 over stdio (newline-delimited JSON). The lifecycle:
`initialize` → `session/new` → `session/prompt` (with `session/update`
notifications streaming back) → turn ends with a `StopReason`. The client can
send `session/cancel` to interrupt. The agent can call back to the client for
permission (`session/request_permission`) or file access (`fs/read_text_file`,
`fs/write_text_file`).

**The reference implementation** has three pieces:
- `wsmanager.ts` — REST API that launches workspace containers (Docker)
- `wmlet.ts` — runs inside each container; manages topics, spawns ACP agent
  subprocesses, bridges WebSocket clients to ACP
- `cli.ts` — terminal client that connects via WebSocket

The important architectural pattern: **wmlet** is an ACP **client**. It spawns
an ACP-compatible agent (e.g., `claude-agent-acp`) as a subprocess, communicates
with it over stdin/stdout using JSON-RPC, and multiplexes multiple human
WebSocket connections onto that single ACP pipe. Humans do not speak ACP
directly — they speak wmlet's simpler WebSocket protocol.

### Concurrency Model

Within a topic, prompts are serial. One prompt at a time; the agent processes
it, the turn ends, the next prompt can be sent. This is inherent to LLMs — a
conversation is a single sequence of messages. If the agent is busy and a
second prompt arrives, it should be queued (not dropped, as the reference
implementation currently does).

All connected humans see all output from the topic. Reads are broadcast, writes
are serialized.

Parallelism comes from running multiple topics. Two topics = two agent
processes = two independent conversation contexts operating on the same
workspace filesystem concurrently.

---

## Part II — What We're Building

### The Goal

Turn Shelley into the workspace runtime. Shelley replaces wmlet. It manages
topics, spawns ACP agent subprocesses, bridges human clients (including its
own web UI) to those agents, persists state in SQLite, and exposes the
workspace protocol's REST and WebSocket APIs.

Shelley's own web UI is one client. Nikolai's reference implementation CLI
(`cli.ts`) is another. Any ACP-aware client (Zed, JetBrains, a custom tool)
that speaks the workspace's WebSocket protocol can connect. They all see the
same topics, the same conversation history, the same streaming output.

### End-User Experiences

**Experience 1: Solo developer, local machine.**
You run Shelley on your laptop. It serves the web UI on `localhost:9000`. You
create topics, each backed by an ACP agent. It works like Shelley does today,
except conversations are called "topics" and you can have multiple running in
parallel with different models. The agent per topic is `shelley-acp` (a new
Go binary that wraps Shelley's loop and speaks ACP). This is the default.

**Experience 2: Two developers, shared server.**
You run Shelley on a cloud VM. You open `https://vm.example.com:9000` in your
browser. Nikolai runs `ws connect vm.example.com:9000 debug-timeout` from
his terminal using the reference implementation's CLI. You're both in the same
workspace. You create a topic "debug" — Nikolai sees it appear and can join.
When you send a prompt, both of you see the agent's streaming response. When
Nikolai sends a prompt, you see it in your UI too.

**Experience 3: Containerized, managed by wsmanager.**
The workspace manager launches a Docker container running Shelley. It hands
out `ws://host:52001/acp/` as the connection endpoint. Multiple clients
connect. This is the full protocol-compliant deployment.

**Experience 4: Mixed agents.**
You create two topics: "architecture" with `shelley-acp` (using Claude Opus)
and "testing" with `claude-agent-acp` (Nikolai's reference agent). Both
topics share the workspace filesystem. You work in "architecture" while
Nikolai works in "testing". You can peek at each other's topics. The agent
is a property of the topic, not the user.

### What a Valid Implementation Requires

To implement the workspace protocol, Shelley must:

1. Expose the workspace protocol's REST API (topics, files, commits, tools)
2. Expose a WebSocket endpoint per topic that any client can connect to
3. Spawn ACP agent subprocesses per topic and bridge them to WebSocket clients
4. Persist conversation history so new clients can catch up
5. Attribute prompts to participants
6. Queue prompts when the agent is busy (not drop them)
7. Support the workspace lifecycle (create, suspend, resume, clone)

### Design Ethos: Test Against the Real Protocol, Not Your Assumptions

This project integrates two independent codebases via a protocol spec. The
highest-risk failure mode is not a bug in any single component — it's a
misunderstanding of the protocol that propagates silently through layers of
code until you discover it late, when everything is built on top of a broken
assumption.

The countermeasure: **use Nikolai's actual reference implementation as the
test oracle from day one.** Don't reimplement his CLI behavior in Go tests.
Don't mock the ACP wire format. Run his real `cli.ts` against your code. Run
his real `wsmanager.ts` against your Docker image. If his tools work against
your code, you're protocol-compliant. If they don't, you've found a bug or a
spec ambiguity — and you want to find it in the first week, not the sixth.

Concretely, this means:

- Nikolai's `agentic-workspace` repo must be cloned alongside Shelley's.
  Tests reference it by path.
- Bun must be installed in the dev and CI environment (for running `cli.ts`
  and `wsmanager.ts`). This is a real dependency, not optional.
- Every implementation phase has a **validation gate** — a concrete test that
  must pass before moving to the next phase. Most gates involve running code
  from the other repository against the code you just wrote.

The gates are ordered so that each phase validates the narrowest possible
assumption. Phase 0 validates "does my binary speak valid ACP?" — before any
server code exists. Phase 1 validates "does my WebSocket endpoint speak
wmlet's protocol?" — before any UI code exists. Each gate is a firewall
against building on broken foundations.

A second principle: **start each phase with the simplest possible thing that
exercises the interface, then fill in the real implementation.** Phase 0
begins with a trivial echo agent that hardcodes responses — get the ACP
framing right before wiring in Shelley's loop. Phase 1 begins with a
WebSocket endpoint that talks to a trivial agent — get the bridge right
before handling persistence. Separate "does the protocol work" from "does the
feature work" so you're never debugging both at once.

A **cumulative smoke test script** (`test/smoke.sh`) grows with each phase.
It runs every prior gate plus the current one. It runs in CI on every commit.
It catches regressions immediately. The script is specified in full in Part VI.

---

## Part III — Architecture

### Concept Mapping

| Workspace Protocol | Shelley | Notes |
|---|---|---|
| Workspace | Shelley server instance | One process = one workspace. The `--workspace-dir` flag is the workspace filesystem. |
| Topic | Conversation + ACPBridge + WSHub | A conversation with multiplayer capabilities added. |
| Topic name | Conversation slug | Slugs are already unique, human-readable identifiers. |
| Topic agent | ACP subprocess | Spawned per topic, communicates via stdio JSON-RPC. |
| Topic ACP endpoint | WebSocket at `/ws/acp/{topic}` | Wmlet's protocol, not raw ACP. |
| Workspace files | `--workspace-dir` filesystem | All topics' agents share this directory. |
| Workspace state | SQLite database + filesystem | Commits snapshot both. |
| Human participant | WebSocket client or SSE client | Web UI uses SSE; external clients use WebSocket. |

### System Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                    Shelley Workspace Server                      │
│                         (Go binary)                             │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    HTTP Server                             │  │
│  │                                                           │  │
│  │  Shelley UI routes (existing):                            │  │
│  │    GET  /api/conversations                                │  │
│  │    GET  /api/conversation/{id}/stream  (SSE)              │  │
│  │    POST /api/conversation/{id}/chat                       │  │
│  │    ...etc                                                 │  │
│  │                                                           │  │
│  │  Workspace protocol routes (new):                         │  │
│  │    GET  /ws/topics                                        │  │
│  │    POST /ws/topics                                        │  │
│  │    GET  /ws/topics/{name}                                 │  │
│  │    DEL  /ws/topics/{name}                                 │  │
│  │    GET  /ws/files/{path}                                  │  │
│  │    PUT  /ws/files/{path}                                  │  │
│  │    POST /ws/commits                                       │  │
│  │    GET  /ws/health                                        │  │
│  │                                                           │  │
│  │  WebSocket endpoints (new):                               │  │
│  │    /ws/acp/{topic}  — workspace protocol WebSocket        │  │
│  │                                                           │  │
│  │  Static assets:                                           │  │
│  │    / — Shelley web UI (existing, extended)                │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                  Topic Manager (new)                       │  │
│  │                                                           │  │
│  │  topics map[string]*Topic                                 │  │
│  │                                                           │  │
│  │  Each Topic:                                              │  │
│  │    ├── ACP Bridge (spawns agent subprocess, stdio pipe)   │  │
│  │    ├── WebSocket Hub (connected clients, broadcast)       │  │
│  │    ├── Prompt Queue (serialized, with attribution)        │  │
│  │    ├── Message Log (persisted to SQLite)                  │  │
│  │    └── Busy state, presence tracking                      │  │
│  └───────────────────────┬───────────────────────────────────┘  │
│                          │                                      │
│            ┌─────────────┼─────────────┐                        │
│            │ stdio       │ stdio       │ stdio                  │
│            ▼             ▼             ▼                         │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐           │
│  │ shelley-acp  │ │ shelley-acp  │ │ claude-agent  │           │
│  │ topic:debug  │ │ topic:tests  │ │ -acp          │           │
│  │ model:opus   │ │ model:flash  │ │ topic:review  │           │
│  └──────────────┘ └──────────────┘ └──────────────┘           │
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────────────┐ │
│  │ SQLite   │  │ SubPub   │  │ Notification Dispatcher      │ │
│  │ (db/)    │  │          │  │ (Discord, email, ntfy, hook) │ │
│  └──────────┘  └──────────┘  └──────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
         ▲              ▲              ▲
         │ SSE          │ WebSocket    │ WebSocket
         │              │              │
    Shelley UI     Nikolai's CLI   Any ACP client
    (browser)      (cli.ts)        (Zed, JetBrains)
```

### Key Design Decisions

**Decision 1: One Shelley instance is one workspace.**

There is no separate "workspace" object inside Shelley. The Shelley process
*is* the workspace runtime, the same way wmlet is the workspace runtime inside
its container in the reference implementation. The Shelley process owns the
workspace filesystem (`--workspace-dir`), serves the workspace's REST and
WebSocket APIs, manages topics, spawns agents, and persists state. The host
environment — your laptop, a cloud VM, a Docker container — is the workspace's
physical substrate. If you want two workspaces, you run two Shelley instances
(or have a workspace manager like wsmanager launch two containers).

**Decision 2: A topic is a Shelley conversation with multiplayer capabilities
added.**

In Shelley today, a conversation is: an ID, a slug, a working directory, a
model, and a sequence of messages in SQLite. This is exactly what a topic
needs for persistence. A topic is a conversation that *also* has:
- An ACP agent subprocess (the `ACPBridge`) — the agent brain
- A WebSocket hub (`WSHub`) — connected external clients
- A prompt queue (`PromptQueue`) — serialized prompts with attribution
- A topic name (mapped to the conversation slug)

Not every conversation is a topic. In workspace mode, you can still have
legacy conversations where Shelley's loop talks directly to the LLM with no
ACP subprocess. These are the existing single-user Shelley experience. Topics
are conversations that participate in the workspace protocol.

This means the database doesn't need a radical redesign. A new `topics` table
links topic metadata (name, agent binary, status) to an existing conversation
ID. The conversation table, message table, and all existing queries work
unchanged — they don't know or care whether a conversation is a topic.

**Decision 3: Shelley serves both its native API and the workspace protocol API
on the same port.**

Shelley's existing routes (`/api/conversations`, `/api/conversation/{id}/stream`,
etc.) continue to work unchanged. New workspace protocol routes live under
`/ws/` prefix. The web UI uses the existing Shelley API for its rich feature
set (SSE streaming, context window tracking, usage data, display data). External
clients use the workspace protocol WebSocket API. Both ultimately read from and
write to the same underlying topic state.

**Decision 5: The default ACP agent is `shelley-acp`, a new Go binary.**

`shelley-acp` wraps Shelley's `loop.Loop`, `llm` package, `claudetool` package,
and `db` package behind an ACP-compliant JSON-RPC interface on stdin/stdout.
This is the flagship agent — multi-model, persistent, with Shelley's full tool
set. But the system supports any ACP agent — you can configure a topic to use
`claude-agent-acp`, `goose`, or any other ACP-compatible binary.

**Decision 6: The Shelley web UI connects via the existing Shelley API, not the
workspace WebSocket.**

The browser talks to Shelley's server over SSE and REST — the same protocol
as today. The server internally bridges this to the topic's ACP agent. This
preserves Shelley's rich UI features (context window meter, usage tracking,
display data, tool-specific rendering) that the simpler workspace WebSocket
protocol cannot carry. External clients connect via the WebSocket endpoint
and get the simpler workspace protocol messages.

**Decision 7: The workspace WebSocket protocol is identical to wmlet's.**

The message format on the WebSocket is the same as the reference
implementation's wmlet: `{"type":"prompt","data":"..."}` inbound,
`{"type":"text","data":"..."}` / `{"type":"tool_call",...}` /
`{"type":"done"}` outbound. This means Nikolai's `cli.ts` works against
Shelley with zero changes.

---

## Part IV — Implementation Plan

### Phase 0: `shelley-acp` — Shelley as an ACP Agent

**New files:**
- `cmd/shelley-acp/main.go` — entrypoint
- `cmd/shelley-acp/jsonrpc.go` — JSON-RPC 2.0 framing (read/write ndjson on stdio)
- `cmd/shelley-acp/handlers.go` — ACP method handlers
- `cmd/shelley-acp/emitter.go` — Loop event → `session/update` notification translator

**How it works:**

`shelley-acp` is a Go binary that reads JSON-RPC from stdin, writes JSON-RPC
to stdout, and internally runs Shelley's `loop.Loop` as the agent brain.

Startup:
1. Wait for `initialize` on stdin. Respond with protocol version and
   capabilities (`loadSession: true`).
2. Wait for `session/new` (or `session/load`). Create a Shelley conversation
   in a local SQLite database at `$WORKSPACE_DIR/.shelley/topics/$NAME.db`.
   Return session ID.
3. Enter prompt loop: wait for `session/prompt`, drive the Loop, emit
   `session/update` notifications as the loop works, return `StopReason`
   when the turn ends.

The emitter callback translates between Shelley's internal events and ACP
notifications:

| Shelley Loop Event | ACP Notification |
|---|---|
| LLM streams a text chunk | `session/update` → `agent_message_chunk` |
| Loop starts a tool call | `session/update` → `tool_call` (status: pending) |
| Tool call completes | `session/update` → `tool_call_update` (status: completed) |
| Loop ends turn | `session/prompt` response with `stopReason: end_turn` |
| LLM hits max tokens | `session/prompt` response with `stopReason: max_tokens` |

For `session/request_permission`: when Shelley's loop encounters a tool that
requires permission (not currently enforced, but the hook exists), `shelley-acp`
writes a `session/request_permission` JSON-RPC request to stdout and blocks
on stdin for the response. The ACP client (wmlet or Shelley's topic manager)
handles the human interaction.

For `fs/read_text_file` and `fs/write_text_file`: `shelley-acp` can call
these on the client, but in practice Shelley's tools access the filesystem
directly (they run in the same container). These are implemented for protocol
compliance but will rarely be used.

For `session/load`: read the conversation history from the SQLite database and
replay it as `session/update` notifications (`user_message_chunk` and
`agent_message_chunk`), then respond to the `session/load` request.

**Changes to existing code:** None. `shelley-acp` is a new consumer of
existing library packages (`loop`, `llm`, `claudetool`, `db`, `models`). It
does not modify them.

**Build:**
```makefile
shelley-acp:
	go build -o shelley-acp ./cmd/shelley-acp
```

#### Phase 0 Implementation Order

**Step 0a: Trivial echo agent.**
Build a `shelley-acp` that handles `initialize` and `session/new` correctly,
and on `session/prompt` responds with a single `agent_message_chunk` that
echoes the input ("I received: {text}") followed by a `session/prompt`
response with `stopReason: end_turn`. No Shelley loop, no LLM, no tools.
The goal is to get JSON-RPC framing, ndjson encoding, and the ACP lifecycle
right in isolation.

**Step 0a gate — run against wmlet + cli.ts:**
```bash
# Build the trivial agent
go build -o shelley-acp ./cmd/shelley-acp

# Patch Nikolai's wmlet to use it (or set env var)
cd ../agentic-workspace/reference-impl
ACP_AGENT=$(pwd)/../../shelley/shelley-acp bun run wmlet.ts &
WMLET_PID=$!
sleep 3

# Connect with Nikolai's real CLI
echo "hello world" | timeout 30 bun run cli.ts connect test-topic
# Expected: see "I received: hello world" in output

kill $WMLET_PID
```

If this passes, the JSON-RPC framing is correct, the ACP lifecycle works,
and wmlet can manage `shelley-acp` as a subprocess. If it fails, fix the
framing before writing any more code.

(Note: wmlet currently hardcodes the agent binary path. Either modify wmlet
to read from an environment variable, or symlink `shelley-acp` to the
expected path. The one-line change to wmlet is:
`const command = process.env.ACP_AGENT || \`${BIN_DIR}/claude-agent-acp\`;`)

**Step 0b: Wire in Shelley's Loop.**
Replace the echo handler with the real Shelley loop — `loop.Loop` with
`llm.Service` and `claudetool.ToolSet`. The emitter callback translates
loop events to ACP `session/update` notifications.

**Step 0b gate — full agent test via wmlet + cli.ts:**
```bash
# Same as 0a, but now with the real loop
# Send a prompt that exercises tools:
echo "list the files in the current directory" | timeout 60 bun run cli.ts connect test-topic
# Expected: see tool_call for bash, tool output, agent summary
```

**Step 0c: Unit test with PredictableService.**
Write a Go test in `cmd/shelley-acp/` that spawns `shelley-acp` as a
subprocess, sends JSON-RPC via its stdin, and verifies the response stream.
Use `loop.PredictableService` for deterministic behavior so this test runs
in CI without API keys.

```go
func TestACPLifecycle(t *testing.T) {
    cmd := exec.Command("./shelley-acp")
    cmd.Env = append(os.Environ(), "SHELLEY_PREDICTABLE=1")
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    cmd.Start()

    // Send initialize, verify response
    sendJSONRPC(stdin, "initialize", initParams)
    resp := readJSONRPC(stdout)
    assert(resp.Result.ProtocolVersion == 1)

    // Send session/new, verify session ID
    sendJSONRPC(stdin, "session/new", newSessionParams)
    resp = readJSONRPC(stdout)
    sessionID := resp.Result.SessionID
    assert(sessionID != "")

    // Send prompt, collect session/update notifications until done
    sendJSONRPC(stdin, "session/prompt", promptParams(sessionID, "hello"))
    updates := collectUpdates(stdout) // reads until prompt response
    assert(len(updates) > 0)
    assert(updates[len(updates)-1].StopReason == "end_turn")
}
```

---

### Phase 1: Topic Manager — the ACP Client Bridge

**New files:**
- `server/topic.go` — `TopicManager` struct and topic lifecycle
- `server/acpbridge.go` — ACP client bridge (spawn process, stdio JSON-RPC)
- `server/ws_handler.go` — WebSocket handler for workspace protocol clients

**`server/topic.go` — TopicManager:**

```go
type TopicConfig struct {
    Name    string // topic name
    Agent   string // agent binary path (default: "shelley-acp")
    Model   string // model ID to pass to agent
    WorkDir string // workspace directory
}

type Topic struct {
    Name         string
    Config       TopicConfig
    Bridge       *ACPBridge           // ACP connection to agent subprocess
    Conversation *ConversationManager // Shelley conversation (for persistence)
    WSHub        *WSHub               // connected WebSocket clients
    PromptQueue  *PromptQueue         // serialized prompt queue with attribution
    CreatedAt    time.Time
}

type TopicManager struct {
    mu     sync.Mutex
    topics map[string]*Topic
    db     *db.DB
    server *Server
    logger *slog.Logger
    workDir string
}
```

The `TopicManager` is owned by `Server`. It manages the lifecycle of topics:

- `CreateTopic(config TopicConfig)` — creates a Shelley conversation in the
  database, spawns the ACP agent subprocess via `ACPBridge`, initializes the
  WebSocket hub.
- `GetTopic(name string)` — returns the topic, or nil.
- `ListTopics()` — returns all topics with status.
- `DeleteTopic(name string)` — kills the agent process, archives the
  conversation, closes WebSocket connections.

**`server/acpbridge.go` — ACP Client Bridge:**

This is a Go port of wmlet's ACP client logic. It:

1. Spawns an ACP agent as a subprocess (`exec.Cmd` with piped stdin/stdout)
2. Sends `initialize` JSON-RPC, waits for response
3. Sends `session/new`, receives session ID
4. Provides `Prompt(text string, sender string)` method that sends
   `session/prompt` and processes `session/update` notifications via a
   callback
5. Routes `session/request_permission` requests from the agent to the
   WebSocket hub for human approval

```go
type ACPBridge struct {
    cmd       *exec.Cmd
    stdin     io.WriteCloser
    stdout    *bufio.Scanner
    sessionID string
    mu        sync.Mutex
    busy      bool
    onUpdate  func(update ACPUpdate) // called for each session/update
    logger    *slog.Logger
}

type ACPUpdate struct {
    Type       string // "agent_message_chunk", "tool_call", "tool_call_update"
    Text       string // for message chunks
    ToolCallID string
    Title      string
    Kind       string
    Status     string
    Content    []ACPContent
}
```

The `onUpdate` callback is wired to two destinations:
1. The WebSocket hub (broadcast to all connected workspace protocol clients)
2. The Shelley `ConversationManager` (persist the message to SQLite, notify
   SSE subscribers)

**`server/ws_handler.go` — WebSocket Handler:**

Handles the `/ws/acp/{topic}` endpoint. This is the workspace protocol's
client-facing WebSocket — the same protocol as wmlet's.

```go
func (s *Server) handleWorkspaceWS(w http.ResponseWriter, r *http.Request) {
    topicName := r.PathValue("topic")

    conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
        InsecureSkipVerify: true, // CORS for dev; tighten in production
    })
    // ...

    topic := s.topicManager.GetTopic(topicName)
    if topic == nil {
        // Auto-create topic on first connection (per protocol spec)
        topic, err = s.topicManager.CreateTopic(TopicConfig{
            Name:    topicName,
            Agent:   s.defaultAgent,
            WorkDir: s.workDir,
        })
    }

    clientID := uuid.NewString()
    topic.WSHub.Add(clientID, conn)
    defer topic.WSHub.Remove(clientID)

    // Send connection confirmation
    wsjson.Write(ctx, conn, WSMessage{Type: "connected", Topic: topicName, SessionID: topic.Bridge.sessionID})

    // Send conversation history for catch-up
    s.sendTopicHistory(ctx, conn, topic)

    // Read loop
    for {
        var msg WSMessage
        err := wsjson.Read(ctx, conn, &msg)
        if err != nil { break }

        switch msg.Type {
        case "prompt":
            topic.PromptQueue.Enqueue(msg.Data, clientID)
        }
    }
}
```

**WSHub** is a set of WebSocket connections for a topic:

```go
type WSHub struct {
    mu      sync.Mutex
    clients map[string]*websocket.Conn
}

func (h *WSHub) Broadcast(msg WSMessage) {
    h.mu.Lock()
    defer h.mu.Unlock()
    data, _ := json.Marshal(msg)
    for id, conn := range h.clients {
        err := conn.Write(ctx, websocket.MessageText, data)
        if err != nil {
            conn.Close(websocket.StatusGoingAway, "")
            delete(h.clients, id)
        }
    }
}
```

**PromptQueue** serializes prompts with attribution:

```go
type QueuedPrompt struct {
    Text     string
    SenderID string
    EnqueuedAt time.Time
}

type PromptQueue struct {
    mu    sync.Mutex
    queue []QueuedPrompt
    notify chan struct{}
}
```

A goroutine per topic drains the queue: wait for the agent to be idle, dequeue
the next prompt, send it to the ACP bridge, wait for the turn to end, repeat.

**Changes to existing code:**

- `server/server.go`: Add `topicManager *TopicManager` field to `Server`.
  Initialize in `NewServer`. Register new routes in `RegisterRoutes`.
- `server/server.go` `RegisterRoutes`: Add workspace protocol routes under `/ws/`.

**New routes added to `RegisterRoutes`:**

```go
// Workspace protocol routes
mux.HandleFunc("/ws/acp/", s.handleWorkspaceWS) // WebSocket per topic
mux.HandleFunc("GET /ws/topics", s.handleListTopics)
mux.HandleFunc("POST /ws/topics", s.handleCreateTopic)
mux.HandleFunc("GET /ws/topics/{name}", s.handleGetTopic)
mux.HandleFunc("DELETE /ws/topics/{name}", s.handleDeleteTopic)
mux.HandleFunc("GET /ws/health", s.handleWorkspaceHealth)
mux.HandleFunc("GET /ws/files/{path...}", s.handleReadFile)
mux.HandleFunc("PUT /ws/files/{path...}", s.handleWriteFile)
```

#### Phase 1 Validation Gates

**Gate 1a — Nikolai's CLI connects and receives a response:**

Before writing any persistence, any Shelley API integration, any UI code —
verify that the WebSocket endpoint works with an external client.

```bash
# Start Shelley in workspace mode
./shelley serve --workspace-dir /tmp/ws --port 9000 --agent ./shelley-acp &
sleep 3

# Use Nikolai's actual CLI to connect
cd ../agentic-workspace/reference-impl
echo "what is 2+2?" | timeout 30 bun run cli.ts connect localhost:9000 math-topic
# Expected: see agent response, tool calls, "done"
```

This validates: WebSocket upgrade works, topic auto-creation works, ACP
agent spawns correctly, the bridge translates session/update to WebSocket
messages, the WebSocket message format matches what cli.ts expects.

**Gate 1b — Two simultaneous CLI clients, same topic:**

```bash
# Terminal 1: connect and send a prompt
bun run cli.ts connect localhost:9000 shared-topic
> explain quicksort

# Terminal 2: connect to the same topic
bun run cli.ts connect localhost:9000 shared-topic
# Expected: Terminal 2 sees the streaming response from Terminal 1's prompt

# Terminal 2: send a prompt while agent is busy (or after it finishes)
> now explain mergesort
# Expected: Terminal 1 sees the streaming response from Terminal 2's prompt
```

This validates: multiple WebSocket clients per topic, broadcast works,
prompt queue works (not dropped), attribution is present (or at least no crash).

**Gate 1c — Two topics in parallel:**

```bash
# Terminal 1:
bun run cli.ts connect localhost:9000 topic-a
> list files in /tmp

# Terminal 2 (simultaneously):
bun run cli.ts connect localhost:9000 topic-b
> what time is it?

# Expected: both topics respond independently, no cross-contamination
```

This validates: topic isolation, separate ACP agent processes, no shared state
leaking between topics.

**Gate 1d — Topic REST API works:**

```bash
# List topics
curl -s http://localhost:9000/ws/topics | jq .
# Expected: shows the topics created above

# Create a topic explicitly
curl -s -X POST http://localhost:9000/ws/topics -d '{"name":"explicit-topic"}'
# Expected: 201, topic info with ACP endpoint

# Health check
curl -s http://localhost:9000/ws/health | jq .
# Expected: mode "workspace", list of topics
```

---

### Phase 2: Wire Shelley's Existing API to Topics

**Goal:** Shelley's web UI works with topics. When you open Shelley's UI, the
conversation drawer shows topics. When you select a topic, you see the
conversation backed by that topic's agent. When you type a message, it goes
through the topic's ACP bridge.

**Changes to `server/convo.go` — ConversationManager:**

Add an optional `ACPBridge` field:

```go
type ConversationManager struct {
    // ... existing fields ...

    // If set, this conversation is backed by an ACP agent subprocess.
    // Prompts go through the bridge instead of the local Loop.
    acpBridge *ACPBridge
    topicName string // empty for legacy (non-workspace) conversations
}
```

Modify `AcceptUserMessage`:

```go
func (cm *ConversationManager) AcceptUserMessage(ctx context.Context, service llm.Service, modelID string, message llm.Message) (bool, error) {
    // ... existing hydration logic ...

    if cm.acpBridge != nil {
        // Topic-backed conversation: send through ACP bridge
        text := extractText(message)
        cm.acpBridge.Prompt(text, cm.userEmail)
        return isFirst, nil
    }

    // Legacy conversation: use local Loop (existing code, unchanged)
    // ... existing ensureLoop, QueueUserMessage logic ...
}
```

When the ACP bridge receives `session/update` notifications, it calls back
into the `ConversationManager` to record the message and publish to SSE
subscribers:

```go
bridge.onUpdate = func(update ACPUpdate) {
    switch update.Type {
    case "agent_message_chunk":
        // Record to DB as agent message, publish to SubPub
        cm.recordAgentChunk(ctx, update.Text)
        cm.subpub.Publish(seqID, StreamResponse{Messages: [...]})
    case "tool_call":
        cm.recordToolCall(ctx, update)
        cm.subpub.Publish(seqID, StreamResponse{Messages: [...]})
    case "done":
        cm.SetAgentWorking(false)
    }
}
```

This means the SSE handler (`handleStreamConversation`) works unchanged — it
subscribes to the `SubPub`, which is fed by the ACP bridge callbacks. The web
UI works unchanged — it connects via SSE, receives messages, renders them.

#### Phase 2 Validation Gates

**Gate 2a — Shelley's existing UI works with a topic conversation, zero UI
changes:**

```bash
# Start Shelley in workspace mode
./shelley serve --workspace-dir /tmp/ws --port 9000 --agent ./shelley-acp &

# Create a topic via REST
curl -s -X POST http://localhost:9000/ws/topics \
  -H 'Content-Type: application/json' \
  -d '{"name":"test-topic"}'

# Open http://localhost:9000 in a browser
# Navigate to the conversation backing "test-topic"
# Send a message through the web UI
# Expected: message goes through ACP bridge, agent responds, SSE streams it
```

The UI doesn't know it's talking to a topic. It just sees a conversation.
If this works, the bridge between Shelley's API and the ACP agent is correct.

**Gate 2b — Mixed-client multiplayer (the core integration test):**

This is the most important validation in the entire project. Run it before
touching any UI code in Phase 3.

```bash
# Start Shelley in workspace mode
./shelley serve --workspace-dir /tmp/ws --port 9000 --agent ./shelley-acp &

# Create a shared topic
curl -s -X POST http://localhost:9000/ws/topics \
  -H 'Content-Type: application/json' \
  -d '{"name":"collab"}'

# Client A: Shelley web UI in browser, viewing the "collab" conversation

# Client B: Nikolai's CLI
cd ../agentic-workspace/reference-impl
bun run cli.ts connect localhost:9000 collab

# From Client B (CLI): send a prompt
> list the contents of /tmp/ws

# Verify: Client A (browser) sees the agent's streaming response via SSE
# Verify: Client B (CLI) sees the same response via WebSocket

# From Client A (browser): send a prompt via the chat input
# Verify: Client B (CLI) sees the response
# Verify: Client A (browser) sees the response
```

This validates the entire thesis: two different clients, two different
protocols (SSE vs WebSocket), same topic, same agent, bidirectional
message flow. If this works, multiplayer works.

**Gate 2c — Conversation persistence and catch-up:**

```bash
# Send several prompts to a topic via CLI
echo "what is 1+1?" | bun run cli.ts connect localhost:9000 persist-test
echo "what is 2+2?" | bun run cli.ts connect localhost:9000 persist-test

# Open the Shelley UI, navigate to "persist-test" conversation
# Expected: full conversation history is visible, including prior prompts
#   and agent responses (loaded from SQLite, not just live-streamed)
```

This validates that the ACP bridge is recording messages to the database
and the SSE handler is replaying history correctly.

**Changes to `server/handlers.go`:**

Modify `handleChatConversation` to pass sender identity:

```go
func (s *Server) handleChatConversation(w http.ResponseWriter, r *http.Request, conversationID string) {
    // ... existing code ...
    // Add sender identification from header or session
    senderEmail := r.Header.Get("X-Workspace-Sender")
    if senderEmail == "" {
        senderEmail = "local-user"
    }
    // ... pass to AcceptUserMessage or ACPBridge ...
}
```

**Changes to `server/server.go`:**

Modify `getOrCreateConversationManager` to handle topic-backed conversations:

```go
func (s *Server) getOrCreateConversationManager(ctx context.Context, conversationID string) (*ConversationManager, error) {
    // ... existing singleflight logic ...

    cm := NewConversationManager(conversationID, s.db, s.logger, s.toolSetConfig, recordFunc, stateChangeFunc)

    // Check if this conversation is a topic
    if topic := s.topicManager.GetTopicByConversationID(conversationID); topic != nil {
        cm.acpBridge = topic.Bridge
        cm.topicName = topic.Name
    }

    return cm, nil
}
```

---

### Phase 3: UI Changes

**Goal:** Shelley's web UI shows topics as a first-class concept. Users can
create topics with specific agents/models, see which topics are active, see
who's connected, and switch between them.

#### 3a: New Types

**`ui/src/types.ts` — add workspace types:**

```typescript
export interface TopicInfo {
  name: string;
  agent: string;           // agent binary name
  model: string;           // model ID
  conversation_id: string; // backing Shelley conversation
  clients: number;         // connected WebSocket clients
  busy: boolean;
  created_at: string;
}

export interface WorkspaceInfo {
  mode: "standalone" | "workspace";
  topics: TopicInfo[];
  workspace_dir: string;
}

export interface ParticipantPresence {
  client_id: string;
  sender: string;          // email or display name
  topic: string;           // which topic they're viewing
  connected_at: string;
}
```

#### 3b: API Service Extensions

**`ui/src/services/api.ts` — add workspace methods:**

```typescript
async getWorkspaceInfo(): Promise<WorkspaceInfo> {
  const response = await fetch(`${this.baseUrl}/../ws/health`);
  return response.json();
}

async listTopics(): Promise<TopicInfo[]> {
  const response = await fetch(`${this.baseUrl}/../ws/topics`);
  return response.json();
}

async createTopic(name: string, agent?: string, model?: string): Promise<TopicInfo> {
  const response = await fetch(`${this.baseUrl}/../ws/topics`, {
    method: "POST",
    headers: this.postHeaders,
    body: JSON.stringify({ name, agent, model }),
  });
  return response.json();
}

async deleteTopic(name: string): Promise<void> {
  await fetch(`${this.baseUrl}/../ws/topics/${name}`, { method: "DELETE" });
}
```

#### 3c: ConversationDrawer Changes

**`ui/src/components/ConversationDrawer.tsx`:**

The drawer currently shows a flat list of conversations, optionally grouped by
`cwd` or `git_repo`. In workspace mode, it adds a "Topics" section at the top:

```tsx
// New state
const [workspaceMode, setWorkspaceMode] = useState(false);
const [topics, setTopics] = useState<TopicInfo[]>([]);

// On mount, check if we're in workspace mode
useEffect(() => {
  api.getWorkspaceInfo().then(info => {
    setWorkspaceMode(info.mode === "workspace");
    if (info.mode === "workspace") {
      setTopics(info.topics);
    }
  }).catch(() => setWorkspaceMode(false));
}, []);

// In render, add topics section above conversations:
{workspaceMode && (
  <div className="topics-section">
    <div className="section-header">
      <span>Topics</span>
      <button onClick={handleCreateTopic} className="btn-icon" title="New topic">+</button>
    </div>
    {topics.map(topic => (
      <TopicItem
        key={topic.name}
        topic={topic}
        isActive={currentConversationId === topic.conversation_id}
        onClick={() => onSelectConversation({ conversation_id: topic.conversation_id, slug: topic.name })}
      />
    ))}
  </div>
)}
```

New component `TopicItem` shows: topic name, agent icon (Shelley vs Claude vs
other), model name, busy indicator, connected client count.

#### 3d: ChatInterface Changes

**`ui/src/components/ChatInterface.tsx`:**

When viewing a topic-backed conversation, the header shows:
- Topic name (instead of conversation slug)
- Agent type and model
- Connected participants count
- "Topic" badge to distinguish from legacy conversations

```tsx
// In the header area:
{currentConversation?.topic_name && (
  <div className="topic-header">
    <span className="topic-badge">Topic</span>
    <span className="topic-name">{currentConversation.topic_name}</span>
    <span className="topic-agent">{currentConversation.topic_agent}</span>
    {currentConversation.topic_clients > 1 && (
      <span className="topic-clients">
        {currentConversation.topic_clients} connected
      </span>
    )}
  </div>
)}
```

#### 3e: MessageInput Changes

**`ui/src/components/MessageInput.tsx`:**

When creating a new topic (not sending to an existing one), the input shows
a topic creation form:

```tsx
// New "Create Topic" mode when in workspace mode with no conversation selected:
{workspaceMode && !conversationId && (
  <div className="new-topic-form">
    <input placeholder="Topic name" value={topicName} onChange={...} />
    <ModelPicker value={model} onChange={...} />
    <select value={agent} onChange={...}>
      <option value="shelley-acp">Shelley</option>
      <option value="claude-agent-acp">Claude Code</option>
    </select>
    <button onClick={handleCreateTopicAndSend}>Create & Send</button>
  </div>
)}
```

#### 3f: Message Attribution

**`ui/src/components/Message.tsx`:**

When a message has a sender attribution (from the workspace protocol), show it:

```tsx
{message.sender && message.sender !== "local-user" && (
  <span className="message-sender">{message.sender}</span>
)}
```

This requires the backend to include sender information in the message's
`user_data` JSON field when recording prompts that come from workspace clients.

#### Phase 3 Validation Gates

**Gate 3a — UI topics section renders (Playwright):**

Shelley already has Playwright e2e infrastructure in `ui/e2e/`. Add a test:

```typescript
test('workspace mode shows topics in drawer', async ({ page }) => {
  // Create a topic via API
  await fetch('http://localhost:9000/ws/topics', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: 'e2e-topic' }),
  });

  await page.goto('http://localhost:9000');
  // Open drawer
  await page.click('[data-testid="open-drawer"]');
  // Verify topics section exists
  await expect(page.locator('.topics-section')).toBeVisible();
  // Verify our topic appears
  await expect(page.locator('text=e2e-topic')).toBeVisible();
});
```

**Gate 3b — End-to-end multiplayer with real UI + real CLI (the full stack
test):**

```bash
# Start Shelley in workspace mode
./shelley serve --workspace-dir /tmp/ws --port 9000 --agent ./shelley-acp &

# In one terminal: Nikolai's CLI connected to a topic
bun run cli.ts connect localhost:9000 full-stack-test &

# Playwright test that opens the browser, navigates to the same topic,
# sends a prompt, and verifies the CLI also received the response.
# The Playwright test can check the CLI's stdout via subprocess.
```

This is the capstone test for the UI phase — the full multiplayer experience
running in a real browser alongside a real external CLI client.

---

### Phase 4: Workspace Protocol REST API

**New file: `server/workspace_handlers.go`**

Implements the REST endpoints that the workspace protocol defines. These are
thin wrappers around `TopicManager`:

```go
func (s *Server) handleListTopics(w http.ResponseWriter, r *http.Request) {
    topics := s.topicManager.ListTopics()
    json.NewEncoder(w).Encode(topics)
}

func (s *Server) handleCreateTopic(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name  string `json:"name"`
        Agent string `json:"agent"`
        Model string `json:"model"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    if req.Agent == "" { req.Agent = s.defaultAgent }
    if req.Model == "" { req.Model = s.defaultModel }

    topic, err := s.topicManager.CreateTopic(TopicConfig{
        Name:    req.Name,
        Agent:   req.Agent,
        Model:   req.Model,
        WorkDir: s.workDir,
    })
    // ... respond with topic info ...
}

func (s *Server) handleWorkspaceHealth(w http.ResponseWriter, r *http.Request) {
    topics := s.topicManager.ListTopics()
    json.NewEncoder(w).Encode(map[string]any{
        "status":        "ok",
        "mode":          "workspace",
        "topics":        topics,
        "workspace_dir": s.workDir,
    })
}
```

File access handlers delegate to the workspace filesystem:

```go
func (s *Server) handleReadWorkspaceFile(w http.ResponseWriter, r *http.Request) {
    path := filepath.Join(s.workDir, r.PathValue("path"))
    // Validate path is within workspace, serve file
}

func (s *Server) handleWriteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
    path := filepath.Join(s.workDir, r.PathValue("path"))
    // Validate path, write body to file
}
```

---

### Phase 5: State Versioning (Commits)

**New file: `server/workspace_state.go`**

```go
type WorkspaceState struct {
    commitsDir string
    workDir    string
    db         *db.DB
    logger     *slog.Logger
}

func (ws *WorkspaceState) Commit(label string) (string, error) {
    commitID := generateCommitID()
    commitDir := filepath.Join(ws.commitsDir, commitID)
    os.MkdirAll(commitDir, 0755)

    // 1. Checkpoint SQLite
    ws.db.Checkpoint()

    // 2. Copy database
    copyFile(ws.db.Path(), filepath.Join(commitDir, "shelley.db"))

    // 3. Snapshot workspace files (excluding .shelley/)
    tarWorkspace(ws.workDir, filepath.Join(commitDir, "workspace.tar.gz"))

    // 4. Record metadata
    ws.db.RecordCommit(commitID, label, time.Now())
    return commitID, nil
}

func (ws *WorkspaceState) Rollback(commitID string) error {
    // Kill all agent processes
    // Restore database and filesystem from snapshot
    // Restart topics
}
```

REST handlers:

```go
mux.HandleFunc("GET /ws/commits", s.handleListCommits)
mux.HandleFunc("POST /ws/commits", s.handleCreateCommit)
mux.HandleFunc("POST /ws/commits/{id}/rollback", s.handleRollback)
```

---

### Phase 6: Server Mode Flag

**Changes to `cmd/shelley/main.go`:**

Add a `--workspace` flag that enables workspace mode:

```go
workspace := fs.String("workspace-dir", "", "Enable workspace mode with this directory")
agentBin := fs.String("agent", "shelley-acp", "Default ACP agent binary")
```

When `--workspace-dir` is set:
- `TopicManager` is initialized
- Workspace protocol routes are registered
- The UI receives `mode: "workspace"` in its init data
- Legacy standalone conversation creation still works (for backward
  compatibility), but the UI defaults to topic-based interaction

When `--workspace-dir` is NOT set:
- Shelley works exactly as it does today
- No `TopicManager`, no workspace routes, no WebSocket endpoints
- Complete backward compatibility

**Changes to `server/server.go` `NewServer`:**

```go
func NewServer(..., workspaceDir string, defaultAgent string) *Server {
    s := &Server{
        // ... existing fields ...
        workDir:      workspaceDir,
        defaultAgent: defaultAgent,
    }

    if workspaceDir != "" {
        s.topicManager = NewTopicManager(s.db, s.logger, workspaceDir, defaultAgent)
    }

    return s
}
```

---

### Phase 7: Dockerfile

```dockerfile
FROM golang:1.23 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /shelley ./cmd/shelley
RUN CGO_ENABLED=1 go build -o /shelley-acp ./cmd/shelley-acp

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y git ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /shelley /usr/local/bin/
COPY --from=builder /shelley-acp /usr/local/bin/

RUN mkdir -p /workspace && git -C /workspace init
VOLUME /workspace

ENV WORKSPACE_DIR=/workspace
EXPOSE 31337

CMD ["shelley", "serve", \
     "--port", "31337", \
     "--workspace-dir", "/workspace", \
     "--agent", "shelley-acp"]
```

This image can be launched by Nikolai's `wsmanager.ts` (change the image name
from `agrp-wmlet` to the Shelley image). The `wsmanager` waits for `/ws/health`
to return OK, then hands out the WebSocket endpoint to clients.

#### Phase 7 Validation Gate

**Gate 7 — Nikolai's wsmanager launches the Shelley container:**

```bash
# Build the Shelley workspace image
docker build -t shelley-workspace -f Dockerfile.workspace .

# Start Nikolai's wsmanager, pointed at the Shelley image
cd ../agentic-workspace/reference-impl
WMLET_IMAGE=shelley-workspace bun run wsmanager.ts &
sleep 2

# Create a workspace via wsmanager
curl -s -X POST http://localhost:31337/workspaces \
  -H 'Content-Type: application/json' \
  -d '{"name":"integration-test","topics":["topic-a"]}'

# Connect with Nikolai's CLI via wsmanager
bun run cli.ts connect integration-test topic-a
> hello from wsmanager
# Expected: full agent response, routed through wsmanager → container → Shelley → shelley-acp
```

This validates the full protocol-compliant deployment path: Nikolai's
orchestrator launches a Shelley container, manages its lifecycle, and
external clients connect through the standard workspace protocol. If this
works, the implementation is a valid workspace runtime.

---

## Part V — File Inventory

### New Files

| File | Language | Purpose |
|---|---|---|
| `cmd/shelley-acp/main.go` | Go | ACP agent entrypoint (stdin/stdout JSON-RPC) |
| `cmd/shelley-acp/jsonrpc.go` | Go | ndjson JSON-RPC 2.0 framing |
| `cmd/shelley-acp/handlers.go` | Go | initialize, session/new, session/prompt, session/load, session/cancel |
| `cmd/shelley-acp/emitter.go` | Go | Loop events → session/update notifications |
| `server/topic.go` | Go | TopicManager, Topic, TopicConfig |
| `server/acpbridge.go` | Go | ACP client bridge (spawn subprocess, stdio pipe) |
| `server/ws_handler.go` | Go | WebSocket handler for workspace protocol clients |
| `server/ws_hub.go` | Go | WebSocket connection hub per topic, broadcast |
| `server/prompt_queue.go` | Go | Serialized prompt queue with attribution |
| `server/workspace_handlers.go` | Go | REST handlers for /ws/ workspace endpoints |
| `server/workspace_state.go` | Go | Commit/rollback via SQLite + filesystem snapshots |
| `db/schema/NNN_topics.sql` | SQL | Topics table (name, agent, model, conversation_id, status) |
| `db/query/topics.sql` | SQL | sqlc queries for topic CRUD |
| `Dockerfile.workspace` | Docker | Container image for workspace deployment |
| `test/smoke.sh` | Bash | Cumulative validation script (runs all gates, grows per phase) |

### Modified Files

| File | Change |
|---|---|
| `cmd/shelley/main.go` | Add `--workspace-dir` and `--agent` flags; conditional TopicManager init |
| `server/server.go` | Add `topicManager`, `workDir`, `defaultAgent` fields; register /ws/ routes; modify `getOrCreateConversationManager` to check for topic backing |
| `server/convo.go` | Add `acpBridge` and `topicName` fields to `ConversationManager`; modify `AcceptUserMessage` to route through ACP bridge when present |
| `server/handlers.go` | Pass sender identity in chat handler; include topic metadata in conversation responses |
| `go.mod` | No new dependencies expected (`github.com/coder/websocket` already present) |
| `ui/src/types.ts` | Add `TopicInfo`, `WorkspaceInfo`, `ParticipantPresence` types |
| `ui/src/services/api.ts` | Add `getWorkspaceInfo`, `listTopics`, `createTopic`, `deleteTopic` |
| `ui/src/App.tsx` | Pass workspace mode state; add topic creation flow |
| `ui/src/components/ConversationDrawer.tsx` | Add "Topics" section; TopicItem component |
| `ui/src/components/ChatInterface.tsx` | Topic header with agent/model/participants info |
| `ui/src/components/MessageInput.tsx` | Topic creation form in workspace mode |
| `ui/src/components/Message.tsx` | Sender attribution display |

### Unchanged

Everything in `loop/`, `llm/`, `claudetool/`, `models/`, `skills/`, `subpub/`,
`slug/`, `gitstate/`, `client/` — no modifications needed. These packages
are consumed as libraries by both the existing server and the new
`shelley-acp` binary.

---

## Part VI — Cumulative Smoke Test

The validation gates above are embedded in each phase. They should also be
collected into a single script that runs in CI on every commit. This script
grows with each phase — earlier gates are never removed, so regressions in
any layer are caught immediately.

**`test/smoke.sh`:**

```bash
#!/bin/bash
set -euo pipefail

SHELLEY_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORKSPACE_DIR="$(pwd)/agentic-workspace/reference-impl"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR; kill 0 2>/dev/null" EXIT

cd "$SHELLEY_DIR"

echo "=== Building ==="
go build -o "$TMPDIR/shelley-acp" ./cmd/shelley-acp
go build -o "$TMPDIR/shelley" ./cmd/shelley

# ------------------------------------------------------------------
# Phase 0 gate: shelley-acp speaks valid ACP
# ------------------------------------------------------------------
echo "=== Phase 0: ACP protocol compliance ==="

# 0a: JSON-RPC lifecycle (trivial check via stdin/stdout)
echo '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"test","version":"0.1"},"clientCapabilities":{}}}' \
  | timeout 10 "$TMPDIR/shelley-acp" 2>/dev/null \
  | head -1 \
  | grep -q '"protocolVersion"' \
  && echo "  ✓ ACP initialize" || { echo "  ✗ ACP initialize"; exit 1; }

# 0b: Full lifecycle via wmlet + cli.ts
cd "$WORKSPACE_DIR"
ACP_AGENT="$TMPDIR/shelley-acp" bun run wmlet.ts &
WMLET_PID=$!
sleep 3
echo "hello" | timeout 30 bun run cli.ts connect test-topic 2>&1 | grep -q "text" \
  && echo "  ✓ shelley-acp via wmlet + cli.ts" || { echo "  ✗ wmlet integration"; exit 1; }
kill $WMLET_PID 2>/dev/null; wait $WMLET_PID 2>/dev/null || true

# ------------------------------------------------------------------
# Phase 1 gate: Shelley's WebSocket endpoint works with cli.ts
# ------------------------------------------------------------------
echo "=== Phase 1: WebSocket endpoint ==="

"$TMPDIR/shelley" serve --workspace-dir "$TMPDIR/workspace" --port 0 --port-file "$TMPDIR/port" --agent "$TMPDIR/shelley-acp" &
SHELLEY_PID=$!
sleep 3
PORT=$(cat "$TMPDIR/port")

# 1a: CLI connects and gets a response
cd "$WORKSPACE_DIR"
echo "say hi" | timeout 30 bun run cli.ts connect localhost:$PORT ws-test 2>&1 | grep -q "text" \
  && echo "  ✓ cli.ts connects to Shelley" || { echo "  ✗ cli.ts connection"; exit 1; }

# 1d: REST API
curl -sf "http://localhost:$PORT/ws/topics" | grep -q "ws-test" \
  && echo "  ✓ REST /ws/topics" || { echo "  ✗ REST topics"; exit 1; }
curl -sf "http://localhost:$PORT/ws/health" | grep -q "workspace" \
  && echo "  ✓ REST /ws/health" || { echo "  ✗ REST health"; exit 1; }

# ------------------------------------------------------------------
# Phase 2 gate: Shelley API sees topic conversations
# ------------------------------------------------------------------
echo "=== Phase 2: Shelley API integration ==="

curl -sf "http://localhost:$PORT/api/conversations" | grep -q "ws-test" \
  && echo "  ✓ Topic visible as Shelley conversation" || { echo "  ✗ Shelley API"; exit 1; }

# ------------------------------------------------------------------
# Phase 4 gate: Workspace protocol REST API
# ------------------------------------------------------------------
echo "=== Phase 4: Workspace REST API ==="

curl -sf -X POST "http://localhost:$PORT/ws/topics" \
  -H 'Content-Type: application/json' \
  -d '{"name":"rest-created"}' | grep -q "rest-created" \
  && echo "  ✓ POST /ws/topics" || { echo "  ✗ POST topics"; exit 1; }

curl -sf "http://localhost:$PORT/ws/topics/rest-created" | grep -q "rest-created" \
  && echo "  ✓ GET /ws/topics/:name" || { echo "  ✗ GET topic"; exit 1; }

kill $SHELLEY_PID 2>/dev/null; wait $SHELLEY_PID 2>/dev/null || true

# ------------------------------------------------------------------
# Phase 7 gate: Docker image works with wsmanager
# ------------------------------------------------------------------
echo "=== Phase 7: Docker + wsmanager ==="

if command -v docker &>/dev/null; then
  cd "$SHELLEY_DIR"
  docker build -t shelley-workspace-test -f Dockerfile.workspace . -q

  cd "$WORKSPACE_DIR"
  WMLET_IMAGE=shelley-workspace-test bun run wsmanager.ts &
  WSM_PID=$!
  sleep 2

  curl -sf -X POST http://localhost:31337/workspaces \
    -H 'Content-Type: application/json' \
    -d '{"name":"docker-test"}' | grep -q "docker-test" \
    && echo "  ✓ wsmanager creates Shelley workspace" || { echo "  ✗ wsmanager"; exit 1; }

  kill $WSM_PID 2>/dev/null; wait $WSM_PID 2>/dev/null || true
  docker rm -f agrp-ws-docker-test 2>/dev/null || true
else
  echo "  ⊘ Docker not available, skipping"
fi

echo ""
echo "=== All gates passed ==="
```

This script is the project's single source of truth for "does it work." Run
it after every meaningful change. Extend it as new phases are completed. Never
delete earlier gates — they catch regressions.

### Unit Tests (in addition to the smoke script)

Unit tests validate internal correctness. The smoke script validates external
protocol compliance. Both are needed.

- `cmd/shelley-acp/*_test.go`: ACP lifecycle with `PredictableService`.
  Subprocess test that sends JSON-RPC, verifies response format.
- `server/acpbridge_test.go`: Mock subprocess that echoes expected ACP
  responses. Verify the bridge translates correctly.
- `server/prompt_queue_test.go`: Enqueue/dequeue ordering, attribution,
  blocking behavior when agent is busy.
- `server/ws_hub_test.go`: Broadcast to N connections, cleanup on write error,
  concurrent send safety.

---

## Part VII — Migration & Backward Compatibility

Shelley without `--workspace-dir` behaves exactly as it does today. No
existing functionality is changed or removed. The workspace mode is additive.

When `--workspace-dir` is set, existing Shelley conversations still work —
they just don't have ACP bridges or topic metadata. Users can create new
topics alongside legacy conversations. Over time, the UI can default to
topic-based interaction in workspace mode while preserving legacy conversation
access.

The database schema change (new `topics` table) is additive — no existing
tables or columns are modified. Migration is a standard `CREATE TABLE IF NOT
EXISTS`.

---

## Part VIII — Build Order

Each phase produces a testable increment. No phase is complete until its
validation gate passes. The gates use Nikolai's actual reference
implementation code — not mocks, not approximations.

1. **Phase 0** (`shelley-acp`): Build trivial echo agent first (Step 0a).
   Run against wmlet + cli.ts. Fix framing. Then wire in real Shelley loop
   (Step 0b). Run against wmlet + cli.ts again. Write unit test (Step 0c).
   **Gate: cli.ts gets a full agent response through wmlet + shelley-acp.**

2. **Phase 1** (Topic Manager + ACP Bridge + WebSocket): Build the WebSocket
   endpoint and topic manager. No UI work. Validate entirely with cli.ts:
   single client, two clients same topic, two topics in parallel, REST API.
   **Gate: two instances of cli.ts in the same topic see each other's prompts
   and responses.**

3. **Phase 2** (Wire Shelley API to topics): Bridge ACP updates to SubPub.
   No UI changes — test by opening the existing Shelley UI and navigating to
   a topic-backed conversation. Then run the mixed-client test: browser +
   cli.ts on the same topic, bidirectional message flow.
   **Gate: Shelley web UI and Nikolai's CLI both see messages from each other
   in the same topic.**

4. **Phase 3** (UI changes): Topics in the drawer, topic creation form,
   participant count, sender attribution. Playwright e2e tests plus the
   full-stack mixed-client test with real browser + real CLI.
   **Gate: Playwright test creates a topic, sends a prompt, verifies response
   renders with topic metadata.**

5. **Phase 4** (Workspace REST API): Full protocol-compliant REST endpoints.
   Validate with curl + the smoke script.
   **Gate: all REST endpoints return expected shapes.**

6. **Phase 5** (State versioning): Commit and rollback via SQLite + filesystem
   snapshots.
   **Gate: commit, modify, rollback, verify state restored.**

7. **Phase 6** (Server mode flag): `--workspace-dir` flag, backward compat.
   **Gate: Shelley without flag works identically to current behavior. All
   existing Shelley tests pass unchanged.**

8. **Phase 7** (Dockerfile): Container image.
   **Gate: Nikolai's wsmanager.ts creates a workspace using the Shelley
   Docker image. cli.ts connects through wsmanager and gets a response.**

The cumulative smoke script (`test/smoke.sh`) runs all prior gates on every
commit. Phase 0's gate is never removed — it runs alongside Phase 7's gate,
catching regressions at every layer.

