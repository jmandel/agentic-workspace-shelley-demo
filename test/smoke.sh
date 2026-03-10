#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SHELLEY_DIR="$ROOT_DIR/shelley"
AW_DIR="$ROOT_DIR/agentic-workspace/reference-impl"
TMPDIR="$(mktemp -d)"
PORT_FILE="$TMPDIR/port"
WORKSPACE_NAME="smoke-workspace"
RESPONSE_TEXT="workspace-smoke-response-123"
FILE_DIR=".workspace-smoke-$RANDOM"
FILE_PATH="$FILE_DIR/note.txt"
SERVER_PID=""
CLI_PID=""
CLI_BUFFER=""

cleanup() {
  if [[ -n "$CLI_PID" ]]; then
    kill "$CLI_PID" 2>/dev/null || true
    wait "$CLI_PID" 2>/dev/null || true
  fi
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
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
  timeout 20 bash -c 'until curl -sf "$1" >/dev/null; do :; done' _ "$url"
}

read_cli_until() {
  local needle="$1"
  local label="$2"
  local line
  local deadline=$((SECONDS + 20))

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

log "Running Shelley server tests"
(
  cd "$SHELLEY_DIR"
  go test ./server
)

log "Installing Bun workspace client dependencies"
(
  cd "$AW_DIR"
  bun install
)

log "Starting Shelley in predictable workspace mode"
(
  cd "$SHELLEY_DIR"
  WORKSPACE_NAME="$WORKSPACE_NAME" "$TMPDIR/shelley" \
    -db "$TMPDIR/shelley.db" \
    -predictable-only \
    -default-model predictable \
    serve \
    -port 0 \
    -port-file "$PORT_FILE" \
    >"$TMPDIR/shelley.log" 2>&1
) &
SERVER_PID=$!

timeout 20 bash -c 'until [[ -s "$1" ]]; do :; done' _ "$PORT_FILE"
PORT="$(tr -d '\n' < "$PORT_FILE")"
wait_for_http "http://localhost:$PORT/ws/health"

log "Checking workspace discovery"
WORKSPACES_JSON="$(curl -sf "http://localhost:$PORT/workspaces")"
require_output "$WORKSPACES_JSON" "$WORKSPACE_NAME" "GET /workspaces exposes the running workspace"

HEALTH_JSON="$(curl -sf "http://localhost:$PORT/ws/health")"
require_output "$HEALTH_JSON" '"mode":"workspace"' "GET /ws/health reports workspace mode"

log "Checking workspace file endpoints"
curl -sf -X PUT --data-binary 'workspace file body' "http://localhost:$PORT/ws/files/$FILE_PATH" >/dev/null
FILE_BODY="$(curl -sf "http://localhost:$PORT/ws/files/$FILE_PATH")"
require_output "$FILE_BODY" "workspace file body" "GET /ws/files/{path} reads file content"

DIR_JSON="$(curl -sf "http://localhost:$PORT/ws/files/$FILE_DIR")"
require_output "$DIR_JSON" "$FILE_PATH" "GET /ws/files/{path} lists directories"

curl -sf -X DELETE "http://localhost:$PORT/ws/files/$FILE_PATH" >/dev/null
curl -sf -X DELETE "http://localhost:$PORT/ws/files/$FILE_DIR" >/dev/null

log "Running real Bun CLI against Shelley"
(
  cd "$AW_DIR"
  coproc CLI { WS_MANAGER="http://localhost:$PORT" bun run cli.ts connect "$WORKSPACE_NAME" smoke-topic 2>&1; }
  exec 3>&"${CLI[1]}"
  exec 4<&"${CLI[0]}"

  read_cli_until "Connected to topic" "cli.ts connected via /workspaces discovery"
  printf 'echo: %s\n' "$RESPONSE_TEXT" >&3
  read_cli_until "thinking..." "cli.ts saw live workspace system update"
  read_cli_until "$RESPONSE_TEXT" "cli.ts received agent response"

  TOPIC_JSON="$(curl -sf "http://localhost:$PORT/ws/topics/smoke-topic")"
  SESSION_ID="$(printf '%s' "$TOPIC_JSON" | sed -n 's/.*"sessionId":"\([^\"]*\)".*/\1/p')"
  if [[ -z "$SESSION_ID" ]]; then
    printf '  ✗ failed to extract topic session id from /ws/topics/smoke-topic\n'
    printf '    response: %s\n' "$TOPIC_JSON"
    exit 1
  fi

  curl -sf \
    -X POST \
    -H 'Content-Type: application/json' \
    -d '{"message":"echo: api-mixed-client-456","model":"predictable"}' \
    "http://localhost:$PORT/api/conversation/$SESSION_ID/chat" >/dev/null
  read_cli_until "api-mixed-client-456" "api chat is broadcast to websocket clients on the same topic"

  printf '/quit\n' >&3
  read_cli_until "Disconnected." "cli.ts disconnected cleanly"
  exec 3>&-
  exec 4<&-
  wait "$CLI_PID"
)

log "Checking topic listing and Shelley API visibility"
TOPICS_JSON="$(curl -sf "http://localhost:$PORT/ws/topics")"
require_output "$TOPICS_JSON" "smoke-topic" "GET /ws/topics lists the connected topic"

CLI_TOPICS_OUTPUT="$(
  cd "$AW_DIR"
  WS_MANAGER="http://localhost:$PORT" bun run cli.ts topics "$WORKSPACE_NAME"
)"
require_output "$CLI_TOPICS_OUTPUT" "smoke-topic" "cli.ts topics lists the connected topic"

CONVERSATIONS_JSON="$(curl -sf "http://localhost:$PORT/api/conversations")"
require_output "$CONVERSATIONS_JSON" "smoke-topic" "GET /api/conversations exposes the topic conversation"

log "Smoke test passed"
