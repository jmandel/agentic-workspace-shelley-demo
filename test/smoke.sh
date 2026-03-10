#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SHELLEY_DIR="$ROOT_DIR/shelley"
AW_DIR="$ROOT_DIR/agentic-workspace/reference-impl"
TMPDIR="$(mktemp -d)"
MANAGER_PORT_FILE="$TMPDIR/manager-port"
MANAGER_NAMESPACE="acme"
RUNTIME_MODE="${SMOKE_RUNTIME_MODE:-process}"
WORKSPACE_NAME="bp-ig-fix"
TOPIC_NAME="bp-example-validator"
TEMPLATE_NAME="acme-rpm-ig"
FILE_DIR=".workspace-smoke-$RANDOM"
FILE_PATH="$FILE_DIR/note.txt"
TOOL_NAME="smoke-tool-$RANDOM"
WORKSPACE_ROOT="$TMPDIR/manager-state/$MANAGER_NAMESPACE/$WORKSPACE_NAME/workspace"
BWRAP_TMP_NAME="manager-bwrap-$RANDOM"
BWRAP_HOST_TMP="/tmp/$BWRAP_TMP_NAME"
LOCAL_TOOLS_DIR="$ROOT_DIR/test/fixtures/local-tools"
JIRA_FIXTURE="$ROOT_DIR/shelleymanager/manager/testdata/hl7-jira-mcp.js"
MANAGER_PID=""
CLI_PID=""
CLI_BUFFER=""

cleanup() {
  if [[ -n "$CLI_PID" ]]; then
    kill "$CLI_PID" 2>/dev/null || true
    wait "$CLI_PID" 2>/dev/null || true
  fi
  if [[ -n "$MANAGER_PID" ]]; then
    kill "$MANAGER_PID" 2>/dev/null || true
    wait "$MANAGER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

log() {
  printf '==> %s\n' "$1"
}

require_output() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    printf '  ✓ %s\n' "$label"
  else
    printf '  ✗ %s\n' "$label"
    printf '    expected to find: %s\n' "$needle"
    printf '    in output:\n%s\n' "$haystack"
    exit 1
  fi
}

wait_for_http() {
  local url="$1"
  timeout 60 bash -c 'until curl -sf "$1" >/dev/null; do :; done' _ "$url"
}

read_cli_until() {
  local needle="$1"
  local label="$2"
  local line
  local deadline=$((SECONDS + 60))

  while (( SECONDS < deadline )); do
    if IFS= read -r -t 1 line <&4; then
      CLI_BUFFER+="$line"$'\n'
      printf '%s\n' "$line"
      if [[ "$line" == *"$needle"* ]]; then
        printf '  ✓ %s\n' "$label"
        return 0
      fi
    fi
  done

  printf '  ✗ %s\n' "$label"
  printf '    expected cli output containing: %s\n' "$needle"
  printf '    collected output:\n%s\n' "$CLI_BUFFER"
  exit 1
}

log "Building Shelley UI"
(
  cd "$SHELLEY_DIR/ui"
  corepack pnpm install --frozen-lockfile
  corepack pnpm run build
)

log "Packaging Shelley templates"
(
  cd "$SHELLEY_DIR"
  make templates
)

log "Building Shelley binary"
(
  cd "$SHELLEY_DIR"
  go build -o "$TMPDIR/shelley" ./cmd/shelley
)

log "Building shelleymanager binary"
(
  cd "$ROOT_DIR/shelleymanager"
  go build -o "$TMPDIR/shelleymanager" ./cmd/shelleymanager
)

log "Running Shelley server tests"
(
  cd "$SHELLEY_DIR"
  go test ./server
)

log "Running shelleymanager tests"
(
  cd "$ROOT_DIR/shelleymanager"
  go test ./...
)

log "Installing Bun workspace client dependencies"
(
  cd "$AW_DIR"
  bun install
)

chmod +x "$LOCAL_TOOLS_DIR/fhir-validator/bin/fhir-validator"

log "Starting shelleymanager in predictable $RUNTIME_MODE-launch mode"
(
  cd "$ROOT_DIR"
  "$TMPDIR/shelleymanager" \
    -listen 127.0.0.1:0 \
    -port-file "$MANAGER_PORT_FILE" \
    -state-dir "$TMPDIR/manager-state" \
    -namespace "$MANAGER_NAMESPACE" \
    -runtime-mode "$RUNTIME_MODE" \
    -shelley-binary "$TMPDIR/shelley" \
    -tools-dir "$LOCAL_TOOLS_DIR" \
    -predictable-only \
    -default-model predictable \
    >"$TMPDIR/shelleymanager.log" 2>&1
) &
MANAGER_PID=$!

timeout 20 bash -c 'until [[ -s "$1" ]]; do :; done' _ "$MANAGER_PORT_FILE"
PORT="$(tr -d '\n' < "$MANAGER_PORT_FILE")"
wait_for_http "http://localhost:$PORT/health"

log "Creating workspace through the manager"
CREATE_JSON="$(curl -sf -X POST "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$WORKSPACE_NAME\",\"template\":\"$TEMPLATE_NAME\",\"topics\":[{\"name\":\"$TOPIC_NAME\"}],\"runtime\":{\"localTools\":[\"fhir-validator\"]}}")"
require_output "$CREATE_JSON" "\"name\":\"$WORKSPACE_NAME\"" "POST /apis/v1/namespaces/{ns}/workspaces creates a Shelley-backed workspace"
require_output "$CREATE_JSON" "\"endpoint\":\"ws://localhost:$PORT/acp/$MANAGER_NAMESPACE/$WORKSPACE_NAME\"" "create response exposes public ACP endpoint"
require_output "$CREATE_JSON" "\"fhir-validator\"" "create response includes selected local tool metadata"

log "Checking manager discovery"
WORKSPACES_JSON="$(curl -sf "http://localhost:$PORT/workspaces")"
require_output "$WORKSPACES_JSON" "$WORKSPACE_NAME" "GET /workspaces exposes the managed workspace"

HEALTH_JSON="$(curl -sf "http://localhost:$PORT/health")"
require_output "$HEALTH_JSON" '"mode":"shelleymanager"' "GET /health reports manager mode"
require_output "$HEALTH_JSON" '"fhir-validator"' "GET /health reports the published local tool catalog"

LOCAL_TOOLS_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/local-tools")"
require_output "$LOCAL_TOOLS_JSON" '"fhir-validator"' "GET /apis/v1/local-tools lists the validator bundle"

DETAIL_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME")"
require_output "$DETAIL_JSON" "\"name\":\"$TOPIC_NAME\"" "GET workspace detail includes the proxied runtime topics"
require_output "$DETAIL_JSON" '"localTools":[{"name":"fhir-validator"' "GET workspace detail includes resolved local tool metadata"

HOME_HTML="$(curl -sf "http://localhost:$PORT/")"
require_output "$HOME_HTML" "Create Workspace" "GET / serves the manager web entry point"
require_output "$HOME_HTML" "Participant Name" "GET / exposes participant naming controls"
require_output "$HOME_HTML" "/ws-language" "GET / links to the ws language tutorial"

WS_LANGUAGE_HTML="$(curl -sf "http://localhost:$PORT/ws-language")"
require_output "$WS_LANGUAGE_HTML" "WS Language Tutorial" "GET /ws-language serves the predictable-model tutorial"
require_output "$WS_LANGUAGE_HTML" "Queueing Trick" "GET /ws-language explains how to demonstrate queueing"

APP_HTML="$(curl -sf "http://localhost:$PORT/app/$MANAGER_NAMESPACE/$WORKSPACE_NAME/$TOPIC_NAME")"
require_output "$APP_HTML" "Send Prompt" "GET /app/{ns}/{workspace}/{topic} serves the topic web UI"
require_output "$APP_HTML" "Use Name" "GET /app/{ns}/{workspace}/{topic} exposes participant naming controls"

if command -v chromium >/dev/null 2>&1; then
  HOME_DOM="$(chromium --headless --disable-gpu --virtual-time-budget=4000 --dump-dom "http://localhost:$PORT/" 2>/dev/null)"
  require_output "$HOME_DOM" "Delete Workspace" "headless Chromium renders a per-workspace card with workspace deletion"
  require_output "$HOME_DOM" "Create Topic" "headless Chromium renders a dedicated topic-creation control"
  require_output "$HOME_DOM" "Open Shelley UI" "headless Chromium renders topic-level Shelley UI links"
  require_output "$HOME_DOM" "Current participant:" "headless Chromium renders the saved participant name"
  APP_DOM="$(chromium --headless --disable-gpu --virtual-time-budget=4000 --dump-dom "http://localhost:$PORT/app/$MANAGER_NAMESPACE/$WORKSPACE_NAME/$TOPIC_NAME" 2>/dev/null)"
  require_output "$APP_DOM" '<span id="status" class="meta">Connected</span>' "headless Chromium connects to the topic web UI websocket"
  require_output "$APP_DOM" '<div class="meta">Connected</div>' "topic web UI renders the initial connected event in a browser"
  require_output "$APP_DOM" "Participant Name" "topic web UI renders participant naming controls in the browser"
fi

PATIENT_CONTENT="$(cat "$WORKSPACE_ROOT/input/examples/Patient-bp-alice-smith.json")"
require_output "$PATIENT_CONTENT" '"gender": "woman"' "workspace creation seeds the broken patient example"
OBS_CONTENT="$(cat "$WORKSPACE_ROOT/input/examples/Observation-bp-alice-morning.json")"
require_output "$OBS_CONTENT" '"effectiveDateTime": "2026-02-30T07:00:00Z"' "workspace creation seeds the broken observation example"

log "Checking manager-proxied workspace file endpoints"
curl -sf -X PUT --data-binary 'workspace file body' "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/$FILE_PATH" >/dev/null
FILE_BODY="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/$FILE_PATH")"
require_output "$FILE_BODY" "workspace file body" "proxied GET /files/{path} reads file content"
HOST_FILE_BODY="$(cat "$WORKSPACE_ROOT/$FILE_PATH")"
require_output "$HOST_FILE_BODY" "workspace file body" "managed Shelley runtime roots file writes in its workspace directory"

DIR_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/$FILE_DIR")"
require_output "$DIR_JSON" "$FILE_PATH" "proxied GET /files/{path} lists directories"

curl -sf -X DELETE "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/$FILE_PATH" >/dev/null
curl -sf -X DELETE "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/$FILE_DIR" >/dev/null

log "Checking manager-proxied workspace tool endpoints"
curl -sf -X PUT --data-binary @"$JIRA_FIXTURE" \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/.demo/hl7-jira-mcp.js" >/dev/null

TOOL_JSON="$(curl -sf -X POST "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools" \
  -H 'Content-Type: application/json' \
  -d @- <<JSON
{
  "name": "hl7-jira",
  "description": "Search realistic HL7 Jira fixture data",
  "provider": "demo@acme.example",
  "protocol": "mcp",
  "transport": {
    "type": "stdio",
    "command": "bun",
    "args": ["./.demo/hl7-jira-mcp.js"],
    "cwd": "."
  },
  "tools": [
    {
      "name": "jira.search",
      "title": "Search HL7 Jira",
      "description": "Search realistic HL7 Jira issues related to validation and FHIRPath behavior",
      "inputSchema": {
        "type": "object",
        "properties": {
          "query": { "type": "string" }
        },
        "required": ["query"],
        "additionalProperties": false
      }
    }
  ]
}
JSON
)"
require_output "$TOOL_JSON" '"name":"hl7-jira"' "proxied POST /tools creates the Jira MCP tool"
require_output "$TOOL_JSON" '"command":"bun"' "workspace tool response normalizes the stdio transport"

GRANT_JSON="$(curl -sf -X POST "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools/hl7-jira/grants" \
  -H 'Content-Type: application/json' \
  -d '{"subject":"agent:*","tools":["jira.search"],"access":"allowed"}')"
require_output "$GRANT_JSON" '"jira.search"' "proxied POST /tools/{tool}/grants accepts RFC-shaped tool grants"

TOOLS_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools")"
require_output "$TOOLS_JSON" '"hl7-jira"' "proxied GET /tools lists the Jira MCP tool"

log "Running real Bun CLI against shelleymanager"
(
  cd "$AW_DIR"
  coproc CLI1 { WS_MANAGER="http://localhost:$PORT" bun run cli.ts connect "$WORKSPACE_NAME" "$TOPIC_NAME" 2>&1; }
  exec 3>&"${CLI1[1]}"
  exec 4<&"${CLI1[0]}"

  read_cli_until "Connected to topic" "cli.ts connected via /workspaces discovery"
  printf '%s\n' 'bash: fhir-validator input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json' >&3
  read_cli_until "thinking..." "cli.ts saw live workspace system update"
  read_cli_until "[tool]" "cli.ts saw the validator bash tool invocation"
  read_cli_until "FHIR Validation tool Version" "cli.ts received real validator output"
  read_cli_until "Patient.gender" "validator output includes the patient validation detail"
  read_cli_until "Observation.component" "validator output includes the blood pressure component detail"
  printf '/quit\n' >&3
  read_cli_until "Disconnected." "first cli session disconnected cleanly"
  exec 3>&-
  exec 4<&-

  coproc CLI2 { WS_MANAGER="http://localhost:$PORT" bun run cli.ts connect "$WORKSPACE_NAME" "$TOPIC_NAME" 2>&1; }
  exec 3>&"${CLI2[1]}"
  exec 4<&"${CLI2[0]}"

  read_cli_until "Connected to topic" "second cli session connected"
  read_cli_until "FHIR Validation tool Version" "late join replay includes prior validator output"
  read_cli_until "Patient.gender" "late join replay includes the patient validation detail"
  read_cli_until "Observation.component" "late join replay includes the validator failure detail"
  printf '%s\n' 'workspace_tool_json: hl7-jira jira.search {"query":"validation error handling"}' >&3
  read_cli_until "FHIR-53953" "cli.ts received the Jira MCP search result"
  read_cli_until "FHIR-53960" "cli.ts received multiple realistic Jira hits"

  if [[ "$RUNTIME_MODE" == "bwrap" ]]; then
    printf 'bash: touch bwrap-inside.txt && touch /tmp/%s && echo bwrap-ok\n' "$BWRAP_TMP_NAME" >&3
    read_cli_until "[tool]" "cli.ts saw a bash tool invocation under bwrap"
    read_cli_until "---" "cli.ts completed the bwrap bash turn"
  fi

  printf '/quit\n' >&3
  read_cli_until "Disconnected." "second cli session disconnected cleanly"
  exec 3>&-
  exec 4<&-
)

log "Checking topic listing through manager routes"
TOPICS_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics")"
require_output "$TOPICS_JSON" "$TOPIC_NAME" "proxied GET /topics lists the connected topic"

CLI_TOPICS_OUTPUT="$(
  cd "$AW_DIR"
  WS_MANAGER="http://localhost:$PORT" bun run cli.ts topics "$WORKSPACE_NAME"
)"
require_output "$CLI_TOPICS_OUTPUT" "$TOPIC_NAME" "cli.ts topics lists the connected topic"

log "Checking prompt queueing through the public manager routes"
QUEUE_WS_OUTPUT="$(
  bun -e '
    const ws = new WebSocket("ws://127.0.0.1:'"$PORT"'/acp/'"$MANAGER_NAMESPACE"'/'"$WORKSPACE_NAME"'/topics/'"$TOPIC_NAME"'?client_id=queue-smoke");
    const seen = [];
    ws.onopen = () => {
      ws.send(JSON.stringify({ type: "prompt", promptId: "p-smoke-1", data: "ws validator \"input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json\" toolpause3 aftertext \"validator finished\"" }));
      ws.send(JSON.stringify({ type: "prompt", promptId: "p-smoke-2", data: "ws text \"smoke queue second\"" }));
      ws.send(JSON.stringify({ type: "prompt", promptId: "p-smoke-3", data: "ws text \"smoke queue third\"" }));
    };
    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      seen.push(msg);
      if (msg.type === "prompt_status" && msg.promptId === "p-smoke-3" && msg.status === "queued") {
        console.log(JSON.stringify(seen));
        process.exit(0);
      }
    };
    setTimeout(() => {
      console.log(JSON.stringify(seen));
      process.exit(2);
    }, 4000);
  '
)"
require_output "$QUEUE_WS_OUTPUT" '"promptId":"p-smoke-2"' "topic websocket emits a prompt id for the queued prompt"
require_output "$QUEUE_WS_OUTPUT" '"promptId":"p-smoke-3"' "topic websocket emits a prompt id for the third queued prompt"
require_output "$QUEUE_WS_OUTPUT" '"status":"queued"' "later prompts are explicitly queued while the first turn is active"

QUEUE_JSON="$(curl -sf -H 'X-Workspace-Client-ID: queue-smoke' "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue")"
require_output "$QUEUE_JSON" '"activePromptId":"p-smoke-1"' "GET /topics/{topic}/queue shows the active prompt"
require_output "$QUEUE_JSON" '"promptId":"p-smoke-2"' "GET /topics/{topic}/queue lists the queued prompt"
require_output "$QUEUE_JSON" '"promptId":"p-smoke-3"' "GET /topics/{topic}/queue lists all queued prompts"

CLI_QUEUE_OUTPUT="$(
  cd "$AW_DIR"
  WS_MANAGER="http://localhost:$PORT" WS_CLIENT_ID="queue-smoke" bun run cli.ts queue "$WORKSPACE_NAME" "$TOPIC_NAME"
)"
require_output "$CLI_QUEUE_OUTPUT" "active=p-smoke-1" "cli.ts queue reports the active prompt"
require_output "$CLI_QUEUE_OUTPUT" "p-smoke-2 queued" "cli.ts queue reports the queued prompt"
require_output "$CLI_QUEUE_OUTPUT" "p-smoke-3 queued" "cli.ts queue reports later queued prompts"

QUEUE_PATCH_JSON="$(curl -sf -X PATCH -H 'X-Workspace-Client-ID: queue-smoke' -H 'Content-Type: application/json' \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/p-smoke-3" \
  -d '{"text":"ws text \"smoke queue third edited\""}')"
require_output "$QUEUE_PATCH_JSON" 'smoke queue third edited' "PATCH /queue/{promptId} updates queued prompt text"

QUEUE_MOVE_JSON="$(curl -sf -X POST -H 'X-Workspace-Client-ID: queue-smoke' -H 'Content-Type: application/json' \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/p-smoke-3/move" \
  -d '{"direction":"top"}')"
require_output "$QUEUE_MOVE_JSON" '"promptId":"p-smoke-3"' "POST /queue/{promptId}/move returns the updated queue snapshot"
require_output "$QUEUE_MOVE_JSON" '"position":1' "moving a queued prompt to the top changes its queue position"

QUEUE_MOVE_BOTTOM_JSON="$(curl -sf -X POST -H 'X-Workspace-Client-ID: queue-smoke' -H 'Content-Type: application/json' \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/p-smoke-3/move" \
  -d '{"direction":"bottom"}')"
require_output "$QUEUE_MOVE_BOTTOM_JSON" '"promptId":"p-smoke-3"' "POST /queue/{promptId}/move also supports bottom"
require_output "$QUEUE_MOVE_BOTTOM_JSON" '"position":2' "moving a queued prompt to the bottom updates its queue position"

if command -v chromium >/dev/null 2>&1; then
  QUEUE_APP_DOM="$(chromium --headless --disable-gpu --virtual-time-budget=1000 --dump-dom "http://localhost:$PORT/app/$MANAGER_NAMESPACE/$WORKSPACE_NAME/$TOPIC_NAME?client_id=queue-smoke" 2>/dev/null)"
  require_output "$QUEUE_APP_DOM" "Prompt Queue" "topic web UI renders a dedicated queue panel"
  require_output "$QUEUE_APP_DOM" "Active prompt p-smoke-1" "topic web UI shows the active prompt in the queue panel"
  require_output "$QUEUE_APP_DOM" "p-smoke-2" "topic web UI renders the queued prompt"
  require_output "$QUEUE_APP_DOM" "smoke queue third edited" "topic web UI renders edited queued prompt text"
  require_output "$QUEUE_APP_DOM" "Save" "topic web UI exposes queued prompt save controls"
  require_output "$QUEUE_APP_DOM" "Top" "topic web UI exposes queued prompt move-to-top controls"
  require_output "$QUEUE_APP_DOM" "Up" "topic web UI exposes queued prompt reorder controls"
  require_output "$QUEUE_APP_DOM" "Down" "topic web UI exposes queued prompt downward reorder controls"
  require_output "$QUEUE_APP_DOM" "Bottom" "topic web UI exposes queued prompt move-to-bottom controls"
  require_output "$QUEUE_APP_DOM" "Delete" "topic web UI exposes queued prompt deletion controls"
fi

curl -sf -X DELETE -H 'X-Workspace-Client-ID: queue-smoke' \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/p-smoke-2" >/dev/null
QUEUE_AFTER_CANCEL="$(curl -sf -H 'X-Workspace-Client-ID: queue-smoke' "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue")"
if [[ "$QUEUE_AFTER_CANCEL" == *'"promptId":"p-smoke-2"'* ]]; then
  printf '  ✗ queued prompt still present after DELETE /queue/{promptId}\n'
  printf '    queue body:\n%s\n' "$QUEUE_AFTER_CANCEL"
  exit 1
fi
printf '  ✓ DELETE /queue/{promptId} removes the queued prompt through the manager\n'

if [[ "$RUNTIME_MODE" == "bwrap" ]]; then
  log "Checking bwrap filesystem isolation"
  if [[ ! -f "$WORKSPACE_ROOT/bwrap-inside.txt" ]]; then
    printf '  ✗ expected bwrap-inside.txt inside managed workspace\n'
    exit 1
  fi
  printf '  ✓ bash tool can write inside the mounted workspace root\n'

  if [[ -e "$BWRAP_HOST_TMP" ]]; then
    printf '  ✗ host tmp marker unexpectedly exists outside bwrap sandbox\n'
    exit 1
  fi
  printf '  ✓ host tmp outside the mounted workspace root was not touched\n'

  if [[ ! -e "$TMPDIR/manager-state/$MANAGER_NAMESPACE/$WORKSPACE_NAME/tmp/$BWRAP_TMP_NAME" ]]; then
    printf '  ✗ expected sandbox tmp file inside workspace state tmp\n'
    exit 1
  fi
  printf '  ✓ bwrap /tmp is sandbox-local under the workspace state root\n'
fi

log "Smoke test passed"
