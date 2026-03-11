#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SHELLEY_DIR="$ROOT_DIR/shelley"
AW_DIR="$ROOT_DIR/agentic-workspace/reference-impl"
TMPDIR="$(mktemp -d)"
MANAGER_PORT_FILE="$TMPDIR/manager-port"
MANAGER_NAMESPACE="acme"
RUNTIME_MODE="${SMOKE_RUNTIME_MODE:-process}"
SHELLEY_UI_MODE="${SMOKE_SHELLEY_UI_MODE:-same_host_port}"
WORKSPACE_NAME="bp-ig-fix"
TOPIC_NAME="bp-example-validator"
TEMPLATE_NAME="acme-rpm-ig"
FILE_DIR=".workspace-smoke-$RANDOM"
FILE_PATH="$FILE_DIR/note.txt"
TOOL_NAME="smoke-tool-$RANDOM"
WORKSPACE_ROOT="$TMPDIR/manager-state/$MANAGER_NAMESPACE/$WORKSPACE_NAME/workspace"
WORKSPACE_DB="$TMPDIR/manager-state/$MANAGER_NAMESPACE/$WORKSPACE_NAME/shelley.db"
BWRAP_TMP_NAME="manager-bwrap-$RANDOM"
BWRAP_HOST_TMP="/tmp/$BWRAP_TMP_NAME"
LOCAL_TOOLS_DIR="$ROOT_DIR/test/fixtures/local-tools"
MANAGER_PID=""
CLI_PID=""
CLI_BUFFER=""
ADMIN_SUBJECT="smoke-admin"
QUEUE_SUBJECT="queue-smoke"

mint_demo_jwt() {
  python3 - "$1" "$2" <<'PY'
import base64, json, sys, time
subject = sys.argv[1]
name = sys.argv[2]
def b64(value):
    raw = json.dumps(value, separators=(",", ":")).encode("utf-8")
    return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")
header = {"alg": "none", "typ": "JWT"}
claims = {"iss": "workspace-demo", "sub": subject, "name": name, "iat": int(time.time())}
print(f"{b64(header)}.{b64(claims)}.")
PY
}

ADMIN_TOKEN="$(mint_demo_jwt "$ADMIN_SUBJECT" "Smoke Admin")"
QUEUE_TOKEN="$(mint_demo_jwt "$QUEUE_SUBJECT" "Queue Smoke")"

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

require_absent() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    printf '  ✗ %s\n' "$label"
    printf '    expected not to find: %s\n' "$needle"
    printf '    in output:\n%s\n' "$haystack"
    exit 1
  else
    printf '  ✓ %s\n' "$label"
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

log "Building shelleymanager web UI"
(
  cd "$ROOT_DIR/shelleymanager/web"
  bun install
  bun run build
  bun test
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
    -shelley-ui-mode "$SHELLEY_UI_MODE" \
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
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$WORKSPACE_NAME\",\"template\":\"$TEMPLATE_NAME\",\"topics\":[{\"name\":\"$TOPIC_NAME\"}],\"runtime\":{\"localTools\":[\"fhir-validator\",\"hl7-jira-support\"]}}")"
require_output "$CREATE_JSON" "\"name\":\"$WORKSPACE_NAME\"" "POST /apis/v1/namespaces/{ns}/workspaces creates a Shelley-backed workspace"
require_output "$CREATE_JSON" "\"fhir-validator\"" "create response includes selected local tool metadata"

log "Checking manager discovery"
WORKSPACES_JSON="$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces")"
require_output "$WORKSPACES_JSON" "$WORKSPACE_NAME" "GET /apis/v1/namespaces/{ns}/workspaces exposes the managed workspace"

HEALTH_JSON="$(curl -sf "http://localhost:$PORT/health")"
require_output "$HEALTH_JSON" '"mode":"shelleymanager"' "GET /health reports manager mode"
require_output "$HEALTH_JSON" '"fhir-validator"' "GET /health reports the published local tool catalog"

LOCAL_TOOLS_JSON="$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/local-tools")"
require_output "$LOCAL_TOOLS_JSON" '"fhir-validator"' "GET /apis/v1/local-tools lists the validator bundle"
require_output "$LOCAL_TOOLS_JSON" '"hl7-jira-support"' "GET /apis/v1/local-tools lists the Jira support bundle"

DETAIL_JSON="$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME")"
require_output "$DETAIL_JSON" "\"name\":\"$TOPIC_NAME\"" "GET workspace detail includes the proxied runtime topics"
require_output "$DETAIL_JSON" '"localTools":[{"name":"fhir-validator"' "GET workspace detail includes resolved local tool metadata"
require_output "$DETAIL_JSON" '"name":"hl7-jira-support"' "GET workspace detail includes the selected Jira support bundle"

HOME_HTML="$(curl -sf "http://localhost:$PORT/")"
require_output "$HOME_HTML" '<div id="root"></div>' "GET / serves the embedded manager SPA shell"
require_output "$HOME_HTML" '/assets/' "GET / includes built web assets"

ABOUT_HTML="$(curl -sf "http://localhost:$PORT/about")"
require_output "$ABOUT_HTML" '<div id="root"></div>' "GET /about serves the embedded SPA shell"
require_output "$ABOUT_HTML" '/assets/' "GET /about includes built web assets"

APP_HTML="$(curl -sf "http://localhost:$PORT/app/$MANAGER_NAMESPACE/$WORKSPACE_NAME/$TOPIC_NAME")"
require_output "$APP_HTML" '<div id="root"></div>' "GET /app/{ns}/{workspace}/{topic} serves the embedded SPA shell"
require_output "$APP_HTML" '/assets/' "GET /app/{ns}/{workspace}/{topic} includes built web assets"

if command -v chromium >/dev/null 2>&1; then
  HOME_DOM="$(chromium --headless --disable-gpu --virtual-time-budget=4000 --dump-dom "http://localhost:$PORT/" 2>/dev/null)"
  require_output "$HOME_DOM" "Create Workspace" "headless Chromium renders the create workspace UI"
  require_output "$HOME_DOM" "Current participant:" "headless Chromium renders participant naming controls"
  require_output "$HOME_DOM" "About" "headless Chromium renders the about link"
  require_output "$HOME_DOM" "Delete Workspace" "headless Chromium renders a per-workspace card with workspace deletion"
  require_output "$HOME_DOM" "Create Topic" "headless Chromium renders a dedicated topic-creation control"
  require_output "$HOME_DOM" "Shelley UI" "headless Chromium renders topic-level Shelley UI links"
  require_output "$HOME_DOM" "Current participant:" "headless Chromium renders the saved participant name"
  ABOUT_DOM="$(chromium --headless --disable-gpu --virtual-time-budget=4000 --dump-dom "http://localhost:$PORT/about" 2>/dev/null)"
  require_output "$ABOUT_DOM" "About This Demo" "headless Chromium renders the about page"
  require_output "$ABOUT_DOM" "Things To Try" "headless Chromium renders the demo guidance"
  APP_DOM="$(chromium --headless --disable-gpu --virtual-time-budget=4000 --dump-dom "http://localhost:$PORT/app/$MANAGER_NAMESPACE/$WORKSPACE_NAME/$TOPIC_NAME" 2>/dev/null)"
  require_output "$APP_DOM" "About" "topic web UI renders the about link in the browser"
  require_output "$APP_DOM" "Open in Shelley" "topic web UI renders the Shelley UI link in the browser"
  require_output "$APP_DOM" "Open in CLI" "topic web UI renders the CLI link in the browser"
  require_absent "$APP_DOM" "WS Reference" "topic web UI no longer renders the old ws reference action"
  require_output "$APP_DOM" "Send" "topic web UI renders the prompt composer in the browser"
fi

PATIENT_CONTENT="$(cat "$WORKSPACE_ROOT/input/examples/Patient-bp-alice-smith.json")"
require_output "$PATIENT_CONTENT" '"gender": "woman"' "workspace creation seeds the broken patient example"
OBS_CONTENT="$(cat "$WORKSPACE_ROOT/input/examples/Observation-bp-alice-morning.json")"
require_output "$OBS_CONTENT" '"effectiveDateTime": "2026-02-30T07:00:00Z"' "workspace creation seeds the broken observation example"

log "Checking manager-proxied workspace file endpoints"
curl -sf -X POST -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/directories?path=$FILE_DIR" >/dev/null
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" --data-binary 'workspace file body' "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/content?path=$FILE_PATH" >/dev/null
FILE_BODY="$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files/content?path=$FILE_PATH")"
require_output "$FILE_BODY" "workspace file body" "proxied GET /files/content reads file content"
HOST_FILE_BODY="$(cat "$WORKSPACE_ROOT/$FILE_PATH")"
require_output "$HOST_FILE_BODY" "workspace file body" "managed Shelley runtime roots file writes in its workspace directory"

DIR_JSON="$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files?path=$FILE_DIR")"
require_output "$DIR_JSON" "\"path\":\"$FILE_PATH\"" "proxied GET /files lists directories"

curl -sf -X DELETE -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files?path=$FILE_PATH" >/dev/null
curl -sf -X DELETE -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/files?path=$FILE_DIR" >/dev/null

log "Checking manager-proxied workspace tool endpoints"
TOOL_JSON="$(curl -sf -X POST "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d @- <<JSON
{
  "name": "hl7-jira",
  "description": "Search and inspect issues from the real HL7 Jira SQLite snapshot",
  "provider": "demo@acme.example",
  "protocol": "mcp",
  "transport": {
    "type": "stdio",
    "command": "bun",
    "args": ["/tools/hl7-jira-support/bin/hl7-jira-mcp.js"],
    "cwd": "/tools/hl7-jira-support",
    "env": {
      "HL7_JIRA_DB": "/tools/hl7-jira-support/data/jira-data.db"
    }
  }
}
JSON
)"
require_output "$TOOL_JSON" '"name":"hl7-jira"' "proxied POST /tools creates the Jira MCP tool"
require_output "$TOOL_JSON" '"/tools/hl7-jira-support/bin/hl7-jira-mcp.js"' "workspace tool response preserves the public stdio transport"
require_output "$TOOL_JSON" '"redacted":true' "workspace tool response redacts sensitive transport values on read"

GRANT_JSON="$(curl -sf -X POST "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools/hl7-jira/grants" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"subject":"agent:*","tools":["*"],"access":"allowed"}')"
require_output "$GRANT_JSON" '"*"' "proxied POST /tools/{tool}/grants accepts wildcard tool grants"

TOOLS_JSON="$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools")"
require_output "$TOOLS_JSON" '"hl7-jira"' "proxied GET /tools lists the Jira MCP tool"

SYSTEM_PROMPT_TOOLS_JSON="$(sqlite3 "$WORKSPACE_DB" "SELECT COALESCE(display_data, '') FROM messages WHERE conversation_id = (SELECT conversation_id FROM topics WHERE topic_name = '$TOPIC_NAME') AND type = 'system' ORDER BY sequence_id ASC LIMIT 1;")"
require_output "$SYSTEM_PROMPT_TOOLS_JSON" 'workspace_hl7-jira' "registered Jira tool is reflected in the stored Shelley system-prompt tool display"

log "Running real Bun CLI against shelleymanager"
(
  cd "$AW_DIR"
  coproc CLI1 { WS_MANAGER="http://localhost:$PORT" WS_JWT="$ADMIN_TOKEN" bun run cli.ts connect "$WORKSPACE_NAME" "$TOPIC_NAME" 2>&1; }
  exec 3>&"${CLI1[1]}"
  exec 4<&"${CLI1[0]}"

  read_cli_until "Connected to topic" "cli.ts connected via canonical manager discovery"
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

  coproc CLI2 { WS_MANAGER="http://localhost:$PORT" WS_JWT="$ADMIN_TOKEN" bun run cli.ts connect "$WORKSPACE_NAME" "$TOPIC_NAME" 2>&1; }
  exec 3>&"${CLI2[1]}"
  exec 4<&"${CLI2[0]}"

  read_cli_until "Connected to topic" "second cli session connected"
  read_cli_until "FHIR Validation tool Version" "late join replay includes prior validator output"
  read_cli_until "Patient.gender" "late join replay includes the patient validation detail"
  read_cli_until "Observation.component" "late join replay includes the validator failure detail"
  printf '%s\n' 'workspace_tool_json: hl7-jira jira.search {"query":"validation error handling"}' >&3
  read_cli_until "FHIR-20482" "cli.ts received the Jira MCP search result"
  read_cli_until "FHIR-31991" "cli.ts received multiple realistic Jira hits"
  printf '%s\n' 'workspace_tool_json: hl7-jira jira.read {"key":"FHIR-20482"}' >&3
  read_cli_until "FHIRPath conformsTo Validation of Warnings/Error handling pull request" "cli.ts received the Jira issue detail output"
  read_cli_until "\"key\": \"FHIR-20482\"" "cli.ts received the full Jira issue JSON"

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
TOPICS_JSON="$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics")"
require_output "$TOPICS_JSON" "$TOPIC_NAME" "proxied GET /topics lists the connected topic"

CLI_TOPICS_OUTPUT="$(
  cd "$AW_DIR"
  WS_MANAGER="http://localhost:$PORT" WS_JWT="$ADMIN_TOKEN" bun run cli.ts topics "$WORKSPACE_NAME"
)"
require_output "$CLI_TOPICS_OUTPUT" "$TOPIC_NAME" "cli.ts topics lists the connected topic"

log "Checking prompt queueing through the public manager routes"
QUEUE_WS_OUTPUT="$(
  bun -e '
    const ws = new WebSocket("ws://127.0.0.1:'"$PORT"'/apis/v1/namespaces/'"$MANAGER_NAMESPACE"'/workspaces/'"$WORKSPACE_NAME"'/topics/'"$TOPIC_NAME"'/events");
    const ids = {};
    const seen = [];
    ws.onopen = () => {
      ws.send(JSON.stringify({ type: "authenticate", token: "'"$QUEUE_TOKEN"'" }));
    };
    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      seen.push(msg);
      if (msg.type === "authenticated") {
        return;
      }
      if (msg.type === "connected") {
      ws.send(JSON.stringify({ type: "prompt", data: "ws validator \"input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json\" toolpause3 aftertext \"validator finished\"" }));
      ws.send(JSON.stringify({ type: "prompt", data: "ws text \"smoke queue second\"" }));
      ws.send(JSON.stringify({ type: "prompt", data: "ws text \"smoke queue third\"" }));
        return;
      }
      if (msg.type === "prompt_status" && msg.status === "accepted" && msg.data === "ws validator \"input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json\" toolpause3 aftertext \"validator finished\"") {
        ids.first = msg.promptId;
      }
      if (msg.type === "prompt_status" && msg.status === "accepted" && msg.data === "ws text \"smoke queue second\"") {
        ids.second = msg.promptId;
      }
      if (msg.type === "prompt_status" && msg.status === "accepted" && msg.data === "ws text \"smoke queue third\"") {
        ids.third = msg.promptId;
      }
      if (msg.type === "prompt_status" && msg.promptId === ids.third && msg.status === "queued") {
        console.log(JSON.stringify({ ids, seen }));
        process.exit(0);
      }
    };
    setTimeout(() => {
      console.log(JSON.stringify({ ids, seen }));
      process.exit(2);
    }, 4000);
  '
)"
QUEUE_FIRST_PROMPT_ID="$(printf '%s' "$QUEUE_WS_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["ids"]["first"])')"
QUEUE_SECOND_PROMPT_ID="$(printf '%s' "$QUEUE_WS_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["ids"]["second"])')"
QUEUE_THIRD_PROMPT_ID="$(printf '%s' "$QUEUE_WS_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["ids"]["third"])')"
require_output "$QUEUE_WS_OUTPUT" "\"promptId\":\"$QUEUE_SECOND_PROMPT_ID\"" "topic websocket emits a prompt id for the queued prompt"
require_output "$QUEUE_WS_OUTPUT" "\"promptId\":\"$QUEUE_THIRD_PROMPT_ID\"" "topic websocket emits a prompt id for the third queued prompt"
require_output "$QUEUE_WS_OUTPUT" '"status":"queued"' "later prompts are explicitly queued while the first turn is active"

QUEUE_JSON="$(curl -sf -H "Authorization: Bearer $QUEUE_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue")"
require_output "$QUEUE_JSON" "\"activePromptId\":\"$QUEUE_FIRST_PROMPT_ID\"" "GET /topics/{topic}/queue shows the active prompt"
require_output "$QUEUE_JSON" "\"promptId\":\"$QUEUE_SECOND_PROMPT_ID\"" "GET /topics/{topic}/queue lists the queued prompt"
require_output "$QUEUE_JSON" "\"promptId\":\"$QUEUE_THIRD_PROMPT_ID\"" "GET /topics/{topic}/queue lists all queued prompts"

CLI_QUEUE_OUTPUT="$(
  cd "$AW_DIR"
  WS_MANAGER="http://localhost:$PORT" WS_JWT="$QUEUE_TOKEN" bun run cli.ts queue "$WORKSPACE_NAME" "$TOPIC_NAME"
)"
require_output "$CLI_QUEUE_OUTPUT" "active=$QUEUE_FIRST_PROMPT_ID" "cli.ts queue reports the active prompt"
require_output "$CLI_QUEUE_OUTPUT" "$QUEUE_SECOND_PROMPT_ID queued" "cli.ts queue reports the queued prompt"
require_output "$CLI_QUEUE_OUTPUT" "$QUEUE_THIRD_PROMPT_ID queued" "cli.ts queue reports later queued prompts"

QUEUE_PATCH_JSON="$(curl -sf -X PATCH -H "Authorization: Bearer $QUEUE_TOKEN" -H 'Content-Type: application/json' \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/$QUEUE_THIRD_PROMPT_ID" \
  -d '{"data":"ws text \"smoke queue third edited\""}')"
require_output "$QUEUE_PATCH_JSON" 'smoke queue third edited' "PATCH /queue/{promptId} updates queued prompt text"

QUEUE_MOVE_JSON="$(curl -sf -X POST -H "Authorization: Bearer $QUEUE_TOKEN" -H 'Content-Type: application/json' \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/$QUEUE_THIRD_PROMPT_ID/move" \
  -d '{"direction":"top"}')"
require_output "$QUEUE_MOVE_JSON" "\"promptId\":\"$QUEUE_THIRD_PROMPT_ID\"" "POST /queue/{promptId}/move returns the updated queue snapshot"
require_output "$QUEUE_MOVE_JSON" '"position":1' "moving a queued prompt to the top changes its queue position"

QUEUE_MOVE_BOTTOM_JSON="$(curl -sf -X POST -H "Authorization: Bearer $QUEUE_TOKEN" -H 'Content-Type: application/json' \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/$QUEUE_THIRD_PROMPT_ID/move" \
  -d '{"direction":"bottom"}')"
require_output "$QUEUE_MOVE_BOTTOM_JSON" "\"promptId\":\"$QUEUE_THIRD_PROMPT_ID\"" "POST /queue/{promptId}/move also supports bottom"
require_output "$QUEUE_MOVE_BOTTOM_JSON" '"position":2' "moving a queued prompt to the bottom updates its queue position"

if command -v chromium >/dev/null 2>&1; then
  QUEUE_APP_DOM="$(chromium --headless --disable-gpu --virtual-time-budget=1000 --dump-dom "http://localhost:$PORT/app/$MANAGER_NAMESPACE/$WORKSPACE_NAME/$TOPIC_NAME" 2>/dev/null)"
  require_output "$QUEUE_APP_DOM" "Prompt Queue" "topic web UI renders a dedicated queue panel"
  require_output "$QUEUE_APP_DOM" "Active prompt $QUEUE_FIRST_PROMPT_ID" "topic web UI shows the active prompt in the queue panel"
  require_output "$QUEUE_APP_DOM" "$QUEUE_SECOND_PROMPT_ID" "topic web UI renders the queued prompt"
  require_output "$QUEUE_APP_DOM" "smoke queue third edited" "topic web UI renders edited queued prompt text"
  require_output "$QUEUE_APP_DOM" "Save" "topic web UI exposes queued prompt save controls"
  require_output "$QUEUE_APP_DOM" "Top" "topic web UI exposes queued prompt move-to-top controls"
  require_output "$QUEUE_APP_DOM" "Up" "topic web UI exposes queued prompt reorder controls"
  require_output "$QUEUE_APP_DOM" "Down" "topic web UI exposes queued prompt downward reorder controls"
  require_output "$QUEUE_APP_DOM" "Bottom" "topic web UI exposes queued prompt move-to-bottom controls"
  require_output "$QUEUE_APP_DOM" "Delete" "topic web UI exposes queued prompt deletion controls"
fi

curl -sf -X DELETE -H "Authorization: Bearer $QUEUE_TOKEN" \
  "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue/$QUEUE_SECOND_PROMPT_ID" >/dev/null
QUEUE_AFTER_CANCEL="$(curl -sf -H "Authorization: Bearer $QUEUE_TOKEN" "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics/$TOPIC_NAME/queue")"
if [[ "$QUEUE_AFTER_CANCEL" == *"\"promptId\":\"$QUEUE_SECOND_PROMPT_ID\""* ]]; then
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
