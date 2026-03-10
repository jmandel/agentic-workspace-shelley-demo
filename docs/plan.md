# Shelley Workspace: Design & Implementation Plan

Turning Shelley into a multiplayer workspace runtime that implements the
Agentic Workspace Protocol.

**Audience:** A developer who has just cloned both repositories and has
not yet read either codebase or the workspace protocol spec.

**Repositories:**
- `github.com/boldsoftware/shelley` — a coding agent (Go + TypeScript/React)
- `github.com/niquola/agentic-workspace` — a protocol spec + reference
  implementation (TypeScript/Bun)

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
- **Model manager** (`models/`): Discovers available models from API keys and
  gateway config. Supports custom user-defined models via the database.
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
  broadcasting to SSE subscribers. Conversation distillation
  (`server/distill.go`) compresses long conversations into operational
  summaries. Skills system (`skills/`) discovers `SKILL.md` files. Git state
  tracking per turn. Notification dispatcher with pluggable channels.

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

### The Two Protocol Interfaces

The workspace protocol defines two distinct interfaces that a workspace
runtime must expose:

**1. A REST API** for workspace and topic management. This is specified in
the spec with concrete endpoints:

```
Topics:
  GET    /topics              — list topics
  POST   /topics              — create a topic
  GET    /topics/{topic}      — get topic details
  DELETE /topics/{topic}      — archive a topic

Files:
  GET    /files/{path}        — read file content
  PUT    /files/{path}        — write file content
  DELETE /files/{path}        — delete file

State:
  GET    /commits             — list commits
  POST   /commits             — create a commit (snapshot)
  POST   /commits/{id}/rollback — rollback to commit

Tools:
  GET    /tools               — list connected tools
  POST   /tools               — connect a tool
  POST   /tools/{tool}/grants — add a grant

Health:
  GET    /health              — health check
```

**2. A WebSocket endpoint per topic** where humans connect to participate.
The spec labels these URLs but does not formally specify the wire protocol.
The reference implementation's CLI (`cli.ts`) and workspace runtime
(`wmlet.ts`) define it by example. This is the complete protocol, extracted
from the reference implementation:

**Client → Server (one message type):**

```json
{"type": "prompt", "data": "<user's message text>"}
```

**Server → Client (seven message types):**

```json
// Connection established
{"type": "connected", "topic": "<name>", "sessionId": "<id>"}

// System status (agent starting, busy, exited, etc.)
{"type": "system", "data": "<status text>"}

// Agent text chunk (streaming — many per turn)
{"type": "text", "data": "<partial text>"}

// Agent started a tool invocation
{"type": "tool_call", "toolCallId": "<id>", "title": "<desc>",
 "kind": "<type>", "status": "pending"}

// Tool progress or completion
{"type": "tool_update", "toolCallId": "<id>", "status": "<status>",
 "title": "<desc>"}

// Agent's turn is complete
{"type": "done"}

// Error
{"type": "error", "data": "<error text>"}
```

This is the entire topic wire protocol. One inbound message type, seven
outbound. No handshake beyond the WebSocket upgrade. No session negotiation.
Connect, receive `connected`, send `prompt`, receive streaming messages,
receive `done`. We treat this as the de facto standard because the reference
implementation's CLI is our primary interoperability target.

### Concurrency Model

Within a topic, prompts are serial. One prompt at a time; the agent processes
it, streams responses, the turn ends, the next prompt can be sent. This
follows from the LLM itself — a conversation is a single sequence of messages.

All connected humans see all output from the topic. Reads are broadcast,
writes are serialized.

Parallelism comes from multiple topics. Two topics = two independent agent
instances operating on the same workspace filesystem concurrently.

---

## Part II — What We're Building

### The Goal

Turn Shelley into a workspace runtime. Shelley manages topics, runs agents
(using its existing loop directly), bridges multiple human clients to those
agents via WebSocket, persists state in SQLite, and exposes the workspace
protocol's REST and WebSocket APIs.

Shelley's own web UI is one client. Nikolai's reference implementation CLI
is another. Any client that speaks the topic WebSocket protocol can connect.
They all see the same topics, the same history, the same streaming output.

### End-User Experiences

**Experience 1: Solo developer, local machine.**
You run Shelley on your laptop. It serves the web UI on `localhost:9000`. You
create topics, each running Shelley's agent loop with whatever model you
choose. It works like Shelley does today, except conversations are called
"topics" and you can have multiple running in parallel with different models.

**Experience 2: Two developers, shared server.**
You run Shelley on a cloud VM. You open `https://vm.example.com:9000` in your
browser. Nikolai runs his CLI from his machine, connecting to the same
endpoint. You're both in the same workspace. You create a topic "debug" —
Nikolai sees it and joins. When you send a prompt, both of you see the agent's
streaming response. When Nikolai sends a prompt, you see it in your UI too.

**Experience 3: Containerized, managed by wsmanager.**
Nikolai's workspace manager launches a Docker container running Shelley. It
hands out the connection endpoint. Multiple clients connect. This is the full
protocol-compliant deployment.

### Design Ethos: Test Against the Real Protocol, Not Your Assumptions

This project integrates two independent codebases via a protocol. The
highest-risk failure mode is a misunderstanding of the protocol that
propagates through layers of code until you discover it late.

The countermeasure: **use Nikolai's actual reference implementation as the
test oracle from day one.** Don't reimplement his CLI behavior in Go tests.
Run his real `cli.ts` against your code. If his tools work against your code,
you're protocol-compliant. If they don't, you've found a bug — and you want
to find it in the first week, not the sixth.

Concretely:

- Nikolai's `agentic-workspace` repo must be cloned alongside Shelley's.
  Tests reference it by path.
- Bun must be installed in the dev and CI environment (for running `cli.ts`).
- Every implementation phase has a **validation gate** — a concrete test that
  must pass before moving to the next phase. Most gates involve running code
  from the other repository against the code you just wrote.

Start each phase with the simplest possible thing that exercises the
interface, then fill in the real implementation. Separate "does the protocol
work" from "does the feature work" so you're never debugging both at once.

A **cumulative smoke test** (`test/smoke.sh`) grows with each phase. It runs
every prior gate plus the current one. It runs in CI on every commit.

---

## Part III — Architecture

### Concept Mapping

| Workspace Protocol | Shelley | Notes |
|---|---|---|
| Workspace | Shelley server instance | One process = one workspace. `--workspace-dir` is the workspace filesystem. |
| Topic | Conversation + TopicHub | A conversation with multiplayer broadcasting added. |
| Topic name | Conversation slug | Slugs are already unique, human-readable identifiers. |
| Topic agent | Shelley's `loop.Loop` | One loop instance per topic, running in a goroutine. No subprocess. |
| Topic WebSocket endpoint | `/ws/topic/{name}` | Speaks the topic wire protocol defined above. |
| Workspace files | `--workspace-dir` filesystem | All topics' agents share this directory. |
| Workspace state | SQLite database + filesystem | Commits snapshot both. |
| Human participant | WebSocket client or SSE client | Web UI uses SSE; external clients use WebSocket. |

### System Diagram

```
┌────────────────────────────────────────────────────────────────┐
│                   Shelley Workspace Server                      │
│                        (Go binary)                              │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                    HTTP Server                            │  │
│  │                                                           │  │
│  │  Shelley UI routes (existing, unchanged):                 │  │
│  │    GET  /api/conversations                                │  │
│  │    GET  /api/conversation/{id}/stream  (SSE)              │  │
│  │    POST /api/conversation/{id}/chat                       │  │
│  │    ...etc                                                 │  │
│  │                                                           │  │
│  │  Workspace protocol routes (new):                         │  │
│  │    GET/POST        /ws/topics                             │  │
│  │    GET/DELETE      /ws/topics/{name}                      │  │
│  │    GET/PUT/DELETE  /ws/files/{path}                       │  │
│  │    GET/POST        /ws/commits                            │  │
│  │    GET             /ws/health                             │  │
│  │                                                           │  │
│  │  WebSocket (new):                                         │  │
│  │    /ws/topic/{name}  — topic wire protocol                │  │
│  │                                                           │  │
│  │  Static assets (existing):                                │  │
│  │    / — Shelley web UI                                     │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                 Topic Manager (new)                        │  │
│  │                                                           │  │
│  │  topics map[string]*Topic                                 │  │
│  │                                                           │  │
│  │  Each Topic:                                              │  │
│  │    ├── Loop (Shelley's loop.Loop — runs the agent)        │  │
│  │    ├── ConversationManager (persistence, SSE broadcast)   │  │
│  │    ├── WSHub (connected WebSocket clients, WS broadcast)  │  │
│  │    ├── PromptQueue (serialized, with sender attribution)  │  │
│  │    └── Model, working dir, busy state, presence           │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────────────┐ │
│  │ SQLite   │  │ SubPub   │  │ Notification Dispatcher      │ │
│  │ (db/)    │  │          │  │ (Discord, email, ntfy, hook) │ │
│  └──────────┘  └──────────┘  └──────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
         ▲              ▲              ▲
         │ SSE          │ WebSocket    │ WebSocket
         │              │              │
    Shelley UI     Nikolai's CLI   Any conforming
    (browser)      (cli.ts)        client
```

### Key Design Decisions

**Decision 1: One Shelley instance is one workspace.**

There is no separate "workspace" object inside Shelley. The Shelley process
*is* the workspace runtime. It owns the workspace filesystem
(`--workspace-dir`), serves the workspace REST and WebSocket APIs, manages
topics, and persists state. If you want two workspaces, you run two Shelley
instances.

**Decision 2: A topic is a Shelley conversation with multiplayer added.**

A topic is a conversation that also has:
- A `loop.Loop` running in a goroutine (the agent)
- A `WSHub` (connected external WebSocket clients)
- A `PromptQueue` (serialized prompts with sender attribution)

Not every conversation is a topic. Legacy single-user conversations still
work unchanged. A new `topics` table links topic metadata (name, model,
status) to a conversation ID. Existing tables are untouched.

**Decision 3: Topics use Shelley's loop directly. No agent subprocess.**

The reference implementation spawns a separate process per topic and talks
to it over stdio. This adds complexity (process management, IPC, another
binary to build) without benefit for Shelley, because Shelley's loop is
already a Go library designed to be instantiated multiple times.

Each topic gets its own `loop.Loop` with its own `llm.Service`, `ToolSet`,
and conversation history. The loop runs in a goroutine. When a prompt arrives,
it's queued; a drainer goroutine feeds prompts to the loop one at a time.
As the loop streams LLM responses and executes tools, callbacks translate
events into both:
- WebSocket messages (broadcast to external clients via `WSHub`)
- SSE messages (broadcast to the Shelley web UI via `SubPub`)

**Decision 4: Shelley serves both its native API and the workspace protocol
on the same port.**

Existing Shelley routes (`/api/...`) are unchanged. New workspace protocol
routes live under `/ws/`. The web UI uses the Shelley API for its rich
features (SSE streaming, context window tracking, usage data, tool-specific
display data). External clients use the workspace WebSocket protocol. Both
read from and write to the same topic state.

**Decision 5: The WebSocket protocol matches the reference implementation
exactly.**

The seven message types defined in Part I. This means Nikolai's `cli.ts`
works against Shelley with zero changes.

**Decision 6: The Shelley web UI connects via SSE (existing API), not via
the workspace WebSocket.**

The workspace WebSocket protocol is simple — streaming text chunks and tool
call metadata. Shelley's SSE API is richer — full message objects with usage
data, display data, context window size, conversation list updates, and
heartbeats. The web UI needs this richness. External clients get the simpler
protocol. Both are fed from the same underlying topic state.

---

## Part IV — Implementation Plan

### Phase 1: WebSocket Endpoint + Topic Manager

**Goal:** Nikolai's CLI can connect to Shelley and interact with an agent.
No Shelley UI changes. Validate everything with the reference implementation's
actual CLI.

**New files:**
- `server/topic.go` — `TopicManager`, `Topic`, `TopicConfig`
- `server/ws_hub.go` — WebSocket connection hub per topic
- `server/ws_handler.go` — WebSocket handler speaking the topic wire protocol
- `server/prompt_queue.go` — Serialized prompt queue with attribution

**`server/topic.go`:**

```go
type TopicConfig struct {
    Name     string
    ModelID  string
    WorkDir  string
}

type Topic struct {
    Name           string
    Config         TopicConfig
    Conversation   *ConversationManager
    Loop           *loop.Loop
    LoopCancel     context.CancelFunc
    WSHub          *WSHub
    PromptQueue    *PromptQueue
    CreatedAt      time.Time
}

type TopicManager struct {
    mu      sync.Mutex
    topics  map[string]*Topic
    db      *db.DB
    server  *Server
    logger  *slog.Logger
    workDir string
}

func (tm *TopicManager) CreateTopic(cfg TopicConfig) (*Topic, error) {
    // 1. Create a Shelley conversation in the database
    // 2. Create a ConversationManager
    // 3. Create a loop.Loop with the configured model and tools
    // 4. Start the loop in a goroutine
    // 5. Wire callbacks: loop events → WSHub broadcast + SubPub publish
    // 6. Start the prompt queue drainer goroutine
    // 7. Register in the topics map
}
```

The key wiring — when the loop produces events, they fan out to two
destinations:

```go
// Called by the loop when the LLM streams a text chunk
func (t *Topic) onAgentChunk(text string) {
    // Broadcast to WebSocket clients (workspace protocol)
    t.WSHub.Broadcast(WSMessage{Type: "text", Data: text})
    // Record to DB and publish to SSE subscribers (Shelley UI)
    t.Conversation.RecordAndPublish(agentMessage)
}

// Called by the loop when a tool is invoked
func (t *Topic) onToolCall(id, title, kind string) {
    t.WSHub.Broadcast(WSMessage{
        Type: "tool_call", ToolCallID: id,
        Title: title, Kind: kind, Status: "pending",
    })
    t.Conversation.RecordAndPublish(toolMessage)
}

// Called when the loop's turn ends
func (t *Topic) onTurnComplete() {
    t.WSHub.Broadcast(WSMessage{Type: "done"})
    t.Conversation.SetAgentWorking(false)
}
```

**`server/ws_handler.go`:**

```go
func (s *Server) handleTopicWS(w http.ResponseWriter, r *http.Request) {
    topicName := r.PathValue("name")

    conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
        InsecureSkipVerify: true,
    })
    if err != nil { return }

    // Auto-create topic on first connection (per spec)
    topic := s.topicManager.GetOrCreateTopic(topicName)

    clientID := uuid.NewString()
    topic.WSHub.Add(clientID, conn)
    defer topic.WSHub.Remove(clientID)

    // Send connection confirmation
    wsjson.Write(ctx, conn, WSMessage{
        Type: "connected", Topic: topicName,
        SessionID: topic.Conversation.ID(),
    })

    // Read loop: receive prompts, enqueue them
    for {
        var msg WSMessage
        if err := wsjson.Read(ctx, conn, &msg); err != nil { break }
        if msg.Type == "prompt" {
            topic.PromptQueue.Enqueue(msg.Data, clientID)
        }
    }
}
```

**`server/prompt_queue.go`:**

```go
type QueuedPrompt struct {
    Text      string
    SenderID  string
    QueuedAt  time.Time
}

type PromptQueue struct {
    mu     sync.Mutex
    queue  []QueuedPrompt
    notify chan struct{}
}

// Drainer goroutine (one per topic):
func (t *Topic) drainPrompts(ctx context.Context) {
    for {
        prompt := t.PromptQueue.WaitForNext(ctx)
        if prompt == nil { return }

        t.WSHub.Broadcast(WSMessage{Type: "system", Data: "thinking..."})

        msg := llm.Message{
            Role: llm.MessageRoleUser,
            Content: []llm.Content{{
                Type: llm.ContentTypeText, Text: prompt.Text,
            }},
        }
        t.Loop.QueueUserMessage(msg)

        // Wait for the turn to complete
        t.WaitForTurnEnd(ctx)
    }
}
```

**Changes to existing code:**

- `server/server.go`: Add `topicManager *TopicManager` field. Register
  workspace routes in `RegisterRoutes`.

**New routes:**
```go
mux.HandleFunc("/ws/topic/", s.handleTopicWS)
mux.HandleFunc("GET /ws/topics", s.handleListTopics)
mux.HandleFunc("POST /ws/topics", s.handleCreateTopic)
mux.HandleFunc("GET /ws/topics/{name}", s.handleGetTopic)
mux.HandleFunc("DELETE /ws/topics/{name}", s.handleDeleteTopic)
mux.HandleFunc("GET /ws/health", s.handleWorkspaceHealth)
```

#### Phase 1 Implementation Order and Gates

**Step 1a: Hardcoded echo topic.**
Build the WebSocket endpoint and topic manager, but instead of running
Shelley's loop, the topic echoes back: "I received: {text}". Get the
WebSocket protocol right — the message types, the connection flow — without
involving the LLM.

**Gate 1a — Nikolai's CLI connects:**
```bash
cd ../agentic-workspace/reference-impl
echo "hello" | timeout 10 bun run cli.ts connect localhost:9000 test-topic
# Expected: "connected", echo response, "done"
```

If this passes, the wire protocol is correct. If it fails, fix the format
before doing anything else.

(Note: `cli.ts` currently expects wsmanager. For direct connection, either
adapt the CLI or write a thin wrapper. The meaningful part is the WebSocket
connection to `/ws/topic/{name}` or a path the CLI accepts.)

**Step 1b: Wire in Shelley's loop.**
Replace the echo with a real `loop.Loop`.

**Gate 1b — full agent interaction:**
```bash
echo "list the files in the current directory" | timeout 60 \
  bun run cli.ts connect localhost:9000 test-topic
# Expected: tool_call for bash, streaming output, agent summary, done
```

**Gate 1c — two clients, same topic:**
```bash
# Terminal 1: connect
bun run cli.ts connect localhost:9000 shared
# Terminal 2: connect to same topic
bun run cli.ts connect localhost:9000 shared
# Prompt from Terminal 1 → both see response
# Prompt from Terminal 2 → both see response
```

**Gate 1d — two topics in parallel:**
```bash
# Terminal 1: topic-a
# Terminal 2: topic-b (simultaneously)
# Independent responses, no cross-contamination
```

---

### Phase 2: Wire Shelley's Existing API to Topics

**Goal:** Shelley's web UI works with topic-backed conversations. No UI
changes — the existing UI sees topics as normal conversations.

**Changes to `server/convo.go`:**

```go
type ConversationManager struct {
    // ... existing fields ...
    topicName string // empty for legacy conversations
    topicHub  *WSHub // nil for legacy conversations
}
```

When a prompt comes via Shelley's REST API for a topic-backed conversation,
route it to the topic's `PromptQueue`:

**Changes to `server/handlers.go`:**

```go
func (s *Server) handleChatConversation(...) {
    // If this conversation is a topic, route through the prompt queue
    if topic := s.topicManager.GetTopicByConversationID(conversationID); topic != nil {
        topic.PromptQueue.Enqueue(text, senderID)
        return
    }
    // Otherwise, existing local loop path (unchanged)
}
```

The loop's event callbacks already publish to `SubPub` (wired in Phase 1).
The SSE handler subscribes to `SubPub`. So the web UI works unchanged.

#### Phase 2 Validation Gates

**Gate 2a — existing UI, zero changes:**
```bash
# Create a topic via REST
curl -s -X POST http://localhost:9000/ws/topics -d '{"name":"ui-test"}'
# Open browser, navigate to "ui-test" conversation
# Send a message → agent responds via SSE
```

**Gate 2b — mixed-client multiplayer (the core test):**
```bash
# Client A: Shelley web UI in browser, viewing "collab" topic
# Client B: Nikolai's CLI connected to same topic
# Prompt from B → A sees response
# Prompt from A → B sees response
```

This is the most important test in the project.

**Gate 2c — history catch-up:**
```bash
# Send prompts via CLI. Then open UI.
# Verify: full history visible (loaded from SQLite)
```

---

### Phase 3: UI Changes

**Goal:** Topics are first-class in the Shelley web UI.

#### 3a: New Types (`ui/src/types.ts`)

```typescript
export interface TopicInfo {
  name: string;
  model: string;
  conversation_id: string;
  clients: number;
  busy: boolean;
  created_at: string;
}

export interface WorkspaceInfo {
  mode: "standalone" | "workspace";
  topics: TopicInfo[];
  workspace_dir: string;
}
```

#### 3b: API Service (`ui/src/services/api.ts`)

```typescript
async getWorkspaceInfo(): Promise<WorkspaceInfo> { ... }
async listTopics(): Promise<TopicInfo[]> { ... }
async createTopic(name: string, model?: string): Promise<TopicInfo> { ... }
async deleteTopic(name: string): Promise<void> { ... }
```

#### 3c: ConversationDrawer — Topics Section

In workspace mode, add a "Topics" section above the conversation list.
`TopicItem` shows: name, model, busy indicator, connected clients count.

#### 3d: ChatInterface — Topic Header

When viewing a topic, show: topic badge, name, model, connected count.

#### 3e: MessageInput — Topic Creation

In workspace mode with no conversation selected, show a topic creation form
with name input and model picker.

#### 3f: Message — Sender Attribution

Show sender identity when a message has attribution from the workspace
protocol.

#### Phase 3 Gates

**Gate 3a — Playwright e2e:**
```typescript
test('workspace mode shows topics', async ({ page }) => {
  await page.goto('http://localhost:9000');
  await page.click('[data-testid="open-drawer"]');
  await expect(page.locator('.topics-section')).toBeVisible();
});
```

**Gate 3b — full stack mixed-client test:**
Playwright in browser + cli.ts subprocess, same topic, bidirectional.

---

### Phase 4: Workspace Tools

**Goal:** External tools (Gmail, GitHub, databases, custom APIs) can be
connected to the workspace via the REST API, governed by grants, and invoked
by the agent through prefixed tools in its tool set.

#### How It Works

The workspace protocol defines tools as capabilities connected by human
participants with credential bindings and per-action grants. The spec says:
"All tool calls go through the workspace runtime, which enforces policy,
checks grants, injects credentials, and logs every invocation."

In Shelley, this is implemented by dynamically adding `workspace_`-prefixed
tools to each topic's `ToolSet`. When Gmail and GitHub are connected to the
workspace, the LLM sees:

```
bash                       — Shelley's local tool (unchanged)
patch                      — Shelley's local tool (unchanged)
keyword_search             — Shelley's local tool (unchanged)
workspace_gmail            — workspace-managed, grants apply
workspace_github           — workspace-managed, grants apply
```

Each `workspace_*` tool has its own name, description, and input schema,
generated from the tool's registration. The LLM reasons about them
individually — it knows `workspace_gmail` can read, search, and send email,
and `workspace_github` can read repos and create PRs. They're first-class
tools in the prompt, not a generic dispatch mechanism.

When the loop executes a tool call:
- Tool name has no prefix → local tool, executes directly (unchanged)
- Tool name starts with `workspace_` → workspace layer handles it

#### The Workspace Tool Execution Path

```
Loop calls workspace_gmail with action "send"
  → Look up "gmail" in workspace tools table
  → Look up grants for (agent, gmail, send)
  → Grant says "approval_required", approvers: [alice@acme.com]
  → Broadcast approval request to WSHub + SubPub
  → Alice clicks approve in her client
  → Inject Alice's Gmail credentials
  → Execute the tool call (via MCP, HTTP, or whatever the tool's protocol is)
  → Log the invocation to audit table
  → Return result to the loop
```

#### New Database Tables

```sql
-- db/schema/NNN_workspace_tools.sql

CREATE TABLE IF NOT EXISTS workspace_tools (
    tool_id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,          -- "gmail", "github"
    description TEXT,
    protocol TEXT DEFAULT 'mcp',        -- "mcp", "http", etc.
    actions TEXT NOT NULL,              -- JSON array: ["read","send","search"]
    provider TEXT,                      -- who connected it: "alice@acme.com"
    credential_ref TEXT,               -- reference to credential store
    config TEXT,                        -- JSON: tool-specific configuration
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS workspace_grants (
    grant_id TEXT PRIMARY KEY,
    tool_id TEXT NOT NULL REFERENCES workspace_tools(tool_id),
    subject TEXT NOT NULL,             -- "agent:*", "agent:topic-debug", "role:contributor"
    actions TEXT NOT NULL,             -- JSON array: ["read","search"]
    access TEXT DEFAULT 'allowed',     -- "allowed", "approval_required", "denied"
    approvers TEXT,                    -- JSON array: ["alice@acme.com"]
    scope TEXT,                        -- JSON: {"from":"client@example.com"}
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS workspace_tool_log (
    log_id TEXT PRIMARY KEY,
    tool_id TEXT NOT NULL,
    topic_name TEXT,
    action TEXT NOT NULL,
    subject TEXT NOT NULL,             -- who initiated
    access_decision TEXT NOT NULL,     -- "allowed", "approved", "denied"
    approved_by TEXT,                  -- who approved (if approval_required)
    input_summary TEXT,               -- truncated input for audit
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

#### New Code

**`server/workspace_tools.go`:**

```go
type WorkspaceTool struct {
    ToolID      string
    Name        string
    Description string
    Protocol    string
    Actions     []string
    Provider    string
    Config      json.RawMessage
}

type WorkspaceGrant struct {
    GrantID   string
    ToolID    string
    Subject   string
    Actions   []string
    Access    string // "allowed", "approval_required", "denied"
    Approvers []string
    Scope     json.RawMessage
}

// BuildWorkspaceTools generates llm.Tool entries for all connected
// workspace tools, prefixed with "workspace_".
func (tm *TopicManager) BuildWorkspaceTools(topicName string) []*llm.Tool {
    tools, _ := tm.db.ListWorkspaceTools()
    var result []*llm.Tool

    for _, wt := range tools {
        tool := &llm.Tool{
            Name:        "workspace_" + wt.Name,
            Description: wt.Description,
            InputSchema: buildSchemaFromActions(wt.Actions),
            Run: func(ctx context.Context, input json.RawMessage) claudetool.Result {
                return tm.executeWorkspaceTool(ctx, topicName, wt, input)
            },
        }
        result = append(result, tool)
    }
    return result
}

func (tm *TopicManager) executeWorkspaceTool(
    ctx context.Context,
    topicName string,
    tool WorkspaceTool,
    input json.RawMessage,
) claudetool.Result {
    // 1. Parse the action from input
    action := parseAction(input)

    // 2. Look up grants
    grant, err := tm.db.FindGrant(tool.ToolID, "agent:"+topicName, action)
    if err != nil || grant == nil {
        return claudetool.Result{Error: fmt.Errorf("no grant for %s/%s", tool.Name, action)}
    }

    // 3. Check access
    switch grant.Access {
    case "denied":
        tm.logToolCall(tool, topicName, action, "denied", "")
        return claudetool.Result{Error: fmt.Errorf("access denied: %s/%s", tool.Name, action)}

    case "approval_required":
        // Broadcast approval request to connected clients
        approved, approver := tm.requestApproval(ctx, topicName, tool, action, grant.Approvers)
        if !approved {
            tm.logToolCall(tool, topicName, action, "denied", "")
            return claudetool.Result{Error: fmt.Errorf("approval denied: %s/%s", tool.Name, action)}
        }
        tm.logToolCall(tool, topicName, action, "approved", approver)

    case "allowed":
        tm.logToolCall(tool, topicName, action, "allowed", "")
    }

    // 4. Execute via the tool's protocol (MCP, HTTP, etc.)
    result := tm.callTool(ctx, tool, action, input)
    return result
}
```

**Approval routing through WebSocket:**

When an approval is needed, the workspace runtime sends a new message type
to connected clients:

```json
{"type": "approval_request", "toolCallId": "<id>",
 "tool": "gmail", "action": "send",
 "summary": "Send email to client@example.com",
 "approvers": ["alice@acme.com"]}
```

Clients respond with:

```json
{"type": "approval_response", "toolCallId": "<id>",
 "approved": true, "approver": "alice@acme.com"}
```

This extends the topic wire protocol with two new message types (one in
each direction). Nikolai's CLI would need to handle these to participate in
approval workflows — or it can ignore them, in which case approvals only
work through Shelley's UI or other clients that implement them.

**Integration with topic ToolSet construction:**

When creating a topic's `ToolSet` in `TopicManager.CreateTopic`, append
workspace tools:

```go
func (tm *TopicManager) CreateTopic(cfg TopicConfig) (*Topic, error) {
    // ... create conversation, loop, etc. ...

    // Build tool set: Shelley's local tools + workspace-managed tools
    localTools := claudetool.NewToolSet(ctx, toolSetConfig)
    workspaceTools := tm.BuildWorkspaceTools(cfg.Name)

    allTools := append(localTools.Tools(), workspaceTools...)
    // Pass allTools to the loop
}
```

#### REST API Handlers

**`server/workspace_handlers.go` — tool endpoints:**

```go
// POST /ws/tools — connect a tool to the workspace
func (s *Server) handleConnectTool(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name        string   `json:"name"`
        Description string   `json:"description"`
        Protocol    string   `json:"protocol"`
        Actions     []string `json:"actions"`
        Provider    string   `json:"provider"`
        Config      any      `json:"config"`
    }
    json.NewDecoder(r.Body).Decode(&req)
    tool, err := s.db.CreateWorkspaceTool(req)
    // ... rebuild tool sets for active topics ...
    json.NewEncoder(w).Encode(tool)
}

// POST /ws/tools/{tool}/grants — add a grant
func (s *Server) handleAddGrant(w http.ResponseWriter, r *http.Request) {
    toolName := r.PathValue("tool")
    var req struct {
        Subject   string   `json:"subject"`
        Actions   []string `json:"actions"`
        Access    string   `json:"access"`
        Approvers []string `json:"approvers"`
        Scope     any      `json:"scope"`
    }
    json.NewDecoder(r.Body).Decode(&req)
    grant, err := s.db.CreateGrant(toolName, req)
    json.NewEncoder(w).Encode(grant)
}

// GET /ws/tools — list connected tools
func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
    tools, _ := s.db.ListWorkspaceToolsWithGrants()
    json.NewEncoder(w).Encode(tools)
}

// DELETE /ws/tools/{tool} — disconnect a tool
// DELETE /ws/tools/{tool}/grants/{grant} — revoke a grant
```

When a tool is connected or a grant changes, active topics need their tool
sets rebuilt so the LLM sees the updated `workspace_*` tools on its next
turn.

#### Dynamic Tool Injection into Live Topics

Shelley's `loop.Loop` reads its tools list under a mutex at the start of
every turn (see `processLLMRequest` in `loop/loop.go`, line 202). It reads
the `l.tools` field fresh each time. However, the field is currently set
only at construction time — there's no public method to update it.

**Change to `loop/loop.go` — add a `SetTools` method:**

```go
// SetTools replaces the tool set. The new tools take effect on the
// next turn — they do not affect a turn already in progress.
func (l *Loop) SetTools(tools []*llm.Tool) {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.tools = tools
}
```

This is the only change needed in the `loop` package. One method, three
lines. The mutex is already there; the loop already reads `l.tools` under
it each turn.

**Rebuild flow when a workspace tool or grant changes:**

```
Human calls POST /ws/tools or POST /ws/tools/{tool}/grants
  → REST handler writes to database
  → REST handler calls topicManager.RebuildToolSets()
      → For each active topic:
          1. Query workspace tools table
          2. Generate workspace_* llm.Tool entries
          3. Combine with Shelley's local tools (bash, patch, etc.)
          4. Call topic.Loop.SetTools(combinedTools)
          5. Optionally update system prompt via topic.Loop.SetSystem(...)
  → Next turn the agent runs, it sees the new tools
```

If the agent is mid-turn when a tool is connected, the change doesn't take
effect until the current turn ends and the next one begins. This is correct
— you can't change the tools the LLM is already reasoning about mid-response.
The LLM sent its tool_use request based on the tools it saw at the start of
the turn; changing them mid-turn would create an inconsistency.

**System prompt update:** When workspace tools change, the system prompt
should also be updated to describe the new tools. Add a corresponding
`SetSystem` method to the Loop:

```go
func (l *Loop) SetSystem(system []llm.SystemContent) {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.system = system
}
```

The topic manager regenerates the system prompt with the updated tool
descriptions and calls `SetSystem`. Like tools, it takes effect on the next
turn.

#### Phase 4 Validation Gates

**Gate 4a — connect a tool and see it in the agent's tool set:**
```bash
# Connect a mock tool
curl -sf -X POST http://localhost:9000/ws/tools \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "test_api",
    "description": "A test API for validation",
    "protocol": "http",
    "actions": ["read", "write"],
    "provider": "dev@example.com"
  }'

# Add a grant
curl -sf -X POST http://localhost:9000/ws/tools/test_api/grants \
  -H 'Content-Type: application/json' \
  -d '{
    "subject": "agent:*",
    "actions": ["read"],
    "access": "allowed"
  }'

# Verify the tool appears in the list
curl -sf http://localhost:9000/ws/tools | jq '.[].name'
# Expected: "test_api"

# Send a prompt to a topic that would trigger the workspace tool
# Verify the LLM can see workspace_test_api in its available tools
```

**Gate 4b — approval workflow:**
```bash
# Add a grant that requires approval
curl -sf -X POST http://localhost:9000/ws/tools/test_api/grants \
  -H 'Content-Type: application/json' \
  -d '{
    "subject": "agent:*",
    "actions": ["write"],
    "access": "approval_required",
    "approvers": ["dev@example.com"]
  }'

# Connect via WebSocket, trigger a write action
# Verify: approval_request message received
# Respond with approval_response
# Verify: tool executes after approval
```

**Gate 4c — audit log:**
```bash
# After running tools, check the audit log
curl -sf http://localhost:9000/ws/tools/test_api | jq '.log'
# Expected: entries with tool calls, decisions, timestamps
```

---

### Phase 5: Full REST API (Files, Health)

Complete remaining workspace protocol REST endpoints: file access, health
with proper response shapes.

**New routes:**
```go
mux.HandleFunc("GET /ws/files/{path...}", s.handleReadWorkspaceFile)
mux.HandleFunc("PUT /ws/files/{path...}", s.handleWriteWorkspaceFile)
mux.HandleFunc("DELETE /ws/files/{path...}", s.handleDeleteWorkspaceFile)
```

**Gate:** curl validates file read/write/delete, health response shape.

---

### Phase 6: State Versioning

Commit/rollback via SQLite checkpoint + filesystem tarball.

```go
func (ws *WorkspaceState) Commit(label string) (string, error) { ... }
func (ws *WorkspaceState) Rollback(commitID string) error { ... }
```

**Gate:** commit, modify, rollback, verify state restored.

---

### Phase 7: Server Mode Flag

`--workspace-dir` flag. Without it, Shelley is unchanged. With it, workspace
features activate.

**Gate:** existing test suite passes without flag.

---

### Phase 8: Dockerfile

```dockerfile
FROM golang:1.23 AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -o /shelley ./cmd/shelley

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y git ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /shelley /usr/local/bin/
RUN mkdir -p /workspace && git -C /workspace init
VOLUME /workspace
EXPOSE 31337
CMD ["shelley", "serve", "--port", "31337", "--workspace-dir", "/workspace"]
```

**Gate — wsmanager launches Shelley:**
```bash
docker build -t shelley-workspace .
cd ../agentic-workspace/reference-impl
WMLET_IMAGE=shelley-workspace bun run wsmanager.ts &
curl -s -X POST http://localhost:31337/workspaces -d '{"name":"test"}'
bun run cli.ts connect test general
> hello
# Expected: full agent response
```

---

## Part V — File Inventory

### New Files

| File | Language | Purpose |
|---|---|---|
| `server/topic.go` | Go | TopicManager, Topic, TopicConfig, lifecycle |
| `server/ws_hub.go` | Go | WebSocket connection hub, broadcast |
| `server/ws_handler.go` | Go | WebSocket handler (topic wire protocol) |
| `server/prompt_queue.go` | Go | Serialized prompt queue with attribution |
| `server/workspace_tools.go` | Go | Workspace tool execution, grant checking, approval routing |
| `server/workspace_handlers.go` | Go | REST handlers for /ws/ endpoints (topics, tools, files) |
| `server/workspace_state.go` | Go | Commit/rollback |
| `db/schema/NNN_topics.sql` | SQL | Topics table |
| `db/schema/NNN_workspace_tools.sql` | SQL | workspace_tools, workspace_grants, workspace_tool_log tables |
| `db/query/topics.sql` | SQL | sqlc queries for topic CRUD |
| `db/query/workspace_tools.sql` | SQL | sqlc queries for tools, grants, audit log |
| `Dockerfile.workspace` | Docker | Container image |
| `test/smoke.sh` | Bash | Cumulative validation script |

### Modified Files

| File | Change |
|---|---|
| `cmd/shelley/main.go` | Add `--workspace-dir` flag; conditional TopicManager init |
| `server/server.go` | Add `topicManager` field; register /ws/ routes; check topic backing in conversation manager lookup |
| `server/convo.go` | Add `topicName`, `topicHub` fields to ConversationManager |
| `server/handlers.go` | Route chat to topic queue for topic-backed conversations |
| `claudetool/toolset.go` | Accept additional tools from workspace layer when constructing per-topic ToolSet |
| `loop/loop.go` | Add `SetTools` and `SetSystem` methods for dynamic tool injection into live topics |
| `ui/src/types.ts` | Add `TopicInfo`, `WorkspaceInfo` |
| `ui/src/services/api.ts` | Add workspace API methods |
| `ui/src/App.tsx` | Workspace mode state |
| `ui/src/components/ConversationDrawer.tsx` | Topics section |
| `ui/src/components/ChatInterface.tsx` | Topic header |
| `ui/src/components/MessageInput.tsx` | Topic creation form |
| `ui/src/components/Message.tsx` | Sender attribution |

### Unchanged

`llm/`, `claudetool/` (except ToolSet construction), `models/`, `skills/`,
`subpub/`, `slug/`, `gitstate/`, `client/` — no modifications. The `loop/`
package gets two small setter methods (`SetTools`, `SetSystem`) but is
otherwise unchanged.

---

## Part VI — Cumulative Smoke Test

**`test/smoke.sh`:**

```bash
#!/bin/bash
set -euo pipefail

SHELLEY_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REFIMPL_DIR="${REFIMPL_DIR:-$(cd "$SHELLEY_DIR/../agentic-workspace/reference-impl" && pwd)}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR; kill 0 2>/dev/null" EXIT

cd "$SHELLEY_DIR"
echo "=== Building ==="
go build -o "$TMPDIR/shelley" ./cmd/shelley

# Start Shelley in workspace mode
mkdir -p "$TMPDIR/workspace"
"$TMPDIR/shelley" serve --workspace-dir "$TMPDIR/workspace" \
  --port 0 --port-file "$TMPDIR/port" &
SHELLEY_PID=$!
sleep 3
PORT=$(cat "$TMPDIR/port")

# --- Phase 1: WebSocket protocol ---
echo "=== Phase 1: WebSocket ==="

cd "$REFIMPL_DIR"
echo "say hello" | timeout 30 bun run cli.ts connect localhost:$PORT test 2>&1 \
  | grep -q "text" \
  && echo "  ✓ cli.ts connects" || { echo "  ✗ cli.ts"; exit 1; }

curl -sf "http://localhost:$PORT/ws/topics" | grep -q "test" \
  && echo "  ✓ GET /ws/topics" || { echo "  ✗ topics"; exit 1; }

curl -sf "http://localhost:$PORT/ws/health" | grep -q "workspace" \
  && echo "  ✓ GET /ws/health" || { echo "  ✗ health"; exit 1; }

# --- Phase 2: Shelley API integration ---
echo "=== Phase 2: Shelley API ==="

curl -sf "http://localhost:$PORT/api/conversations" | grep -q "test" \
  && echo "  ✓ Topic as conversation" || { echo "  ✗ API"; exit 1; }

# --- Phase 4: Workspace tools ---
echo "=== Phase 4: Workspace tools ==="

curl -sf -X POST "http://localhost:$PORT/ws/tools" \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke_tool","description":"test","actions":["read"],"provider":"test"}' \
  | grep -q "smoke_tool" \
  && echo "  ✓ POST /ws/tools" || { echo "  ✗ POST tools"; exit 1; }

curl -sf "http://localhost:$PORT/ws/tools" | grep -q "smoke_tool" \
  && echo "  ✓ GET /ws/tools" || { echo "  ✗ GET tools"; exit 1; }

# --- Phase 5: REST API ---
echo "=== Phase 5: REST ==="

curl -sf -X POST "http://localhost:$PORT/ws/topics" \
  -H 'Content-Type: application/json' \
  -d '{"name":"rest-test"}' | grep -q "rest-test" \
  && echo "  ✓ POST /ws/topics" || { echo "  ✗ POST"; exit 1; }

kill $SHELLEY_PID 2>/dev/null; wait $SHELLEY_PID 2>/dev/null || true

# --- Phase 8: Docker ---
echo "=== Phase 8: Docker ==="

if command -v docker &>/dev/null; then
  cd "$SHELLEY_DIR"
  docker build -t shelley-ws-test -f Dockerfile.workspace . -q

  cd "$REFIMPL_DIR"
  WMLET_IMAGE=shelley-ws-test bun run wsmanager.ts &
  WSM_PID=$!
  sleep 2

  curl -sf -X POST http://localhost:31337/workspaces \
    -d '{"name":"dtest"}' | grep -q "dtest" \
    && echo "  ✓ wsmanager + Shelley" || { echo "  ✗ wsmanager"; exit 1; }

  kill $WSM_PID 2>/dev/null; wait $WSM_PID 2>/dev/null || true
  docker rm -f agrp-ws-dtest 2>/dev/null || true
else
  echo "  ⊘ Docker unavailable"
fi

echo "=== All gates passed ==="
```

---

## Part VII — Backward Compatibility

Shelley without `--workspace-dir` behaves exactly as today. No existing
functionality is changed or removed. Workspace mode is additive. The database
schema change (new `topics` table) is additive — no existing tables modified.

---

## Part VIII — Build Order

1. **Phase 1** — WebSocket + Topics. Echo first, then real loop. Validate
   with cli.ts. **Gate: two cli.ts instances share a topic.**

2. **Phase 2** — Shelley API bridge. Topics work via SSE. **Gate: web UI +
   cli.ts in same topic, bidirectional.**

3. **Phase 3** — UI. Topics in drawer, creation, headers, attribution.
   **Gate: Playwright + mixed-client test.**

4. **Phase 4** — Workspace tools. `workspace_`-prefixed tools with grants,
   approval routing, audit logging. **Gate: connect tool via REST, agent sees
   it, approval workflow works.**

5. **Phase 5** — REST API. File access, health. **Gate: curl.**

6. **Phase 6** — State versioning. **Gate: commit, modify, rollback.**

7. **Phase 7** — Mode flag. **Gate: existing tests pass without flag.**

8. **Phase 8** — Docker. **Gate: wsmanager launches Shelley, cli.ts
   connects through it.**

The smoke script runs all prior gates on every commit.
