#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SHELLEY_DIR="$ROOT_DIR/shelley"
AW_DIR="$ROOT_DIR/agentic-workspace/reference-impl"
TMPDIR="$(mktemp -d)"
MANAGER_PORT_FILE="$TMPDIR/manager-port"
MANAGER_NAMESPACE="acme"
RUNTIME_MODE="${SMOKE_RUNTIME_MODE:-process}"
WORKSPACE_NAME="smoke-workspace"
RESPONSE_TEXT="workspace-smoke-response-123"
FILE_DIR=".workspace-smoke-$RANDOM"
FILE_PATH="$FILE_DIR/note.txt"
TOOL_NAME="smoke-tool-$RANDOM"
WORKSPACE_ROOT="$TMPDIR/manager-state/$MANAGER_NAMESPACE/$WORKSPACE_NAME/workspace"
BWRAP_TMP_NAME="manager-bwrap-$RANDOM"
BWRAP_HOST_TMP="/tmp/$BWRAP_TMP_NAME"
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
  -d "{\"name\":\"$WORKSPACE_NAME\",\"topics\":[{\"name\":\"smoke-topic\"}]}")"
require_output "$CREATE_JSON" "\"name\":\"$WORKSPACE_NAME\"" "POST /apis/v1/namespaces/{ns}/workspaces creates a Shelley-backed workspace"
require_output "$CREATE_JSON" "\"endpoint\":\"ws://localhost:$PORT/acp/$MANAGER_NAMESPACE/$WORKSPACE_NAME\"" "create response exposes public ACP endpoint"

log "Checking manager discovery"
WORKSPACES_JSON="$(curl -sf "http://localhost:$PORT/workspaces")"
require_output "$WORKSPACES_JSON" "$WORKSPACE_NAME" "GET /workspaces exposes the managed workspace"

HEALTH_JSON="$(curl -sf "http://localhost:$PORT/health")"
require_output "$HEALTH_JSON" '"mode":"shelleymanager"' "GET /health reports manager mode"

DETAIL_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME")"
require_output "$DETAIL_JSON" '"name":"smoke-topic"' "GET workspace detail includes the proxied runtime topics"

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
TOOL_JSON="$(curl -sf -X POST "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$TOOL_NAME\",\"description\":\"smoke tool\",\"actions\":[\"read\"],\"provider\":\"smoke\"}")"
require_output "$TOOL_JSON" "$TOOL_NAME" "proxied POST /tools creates a workspace tool"

TOOLS_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools")"
require_output "$TOOLS_JSON" "$TOOL_NAME" "proxied GET /tools lists the created tool"

curl -sf -X DELETE "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/tools/$TOOL_NAME" >/dev/null

log "Running real Bun CLI against shelleymanager"
(
  cd "$AW_DIR"
  coproc CLI { WS_MANAGER="http://localhost:$PORT" bun run cli.ts connect "$WORKSPACE_NAME" smoke-topic 2>&1; }
  exec 3>&"${CLI[1]}"
  exec 4<&"${CLI[0]}"

  read_cli_until "Connected to topic" "cli.ts connected via /workspaces discovery"
  printf 'echo: %s\n' "$RESPONSE_TEXT" >&3
  read_cli_until "thinking..." "cli.ts saw live workspace system update"
  read_cli_until "$RESPONSE_TEXT" "cli.ts received agent response"

  if [[ "$RUNTIME_MODE" == "bwrap" ]]; then
    printf 'bash: touch bwrap-inside.txt && touch /tmp/%s && echo bwrap-ok\n' "$BWRAP_TMP_NAME" >&3
    read_cli_until "[tool]" "cli.ts saw a bash tool invocation under bwrap"
    read_cli_until "---" "cli.ts completed the bwrap bash turn"
  fi

  printf '/quit\n' >&3
  read_cli_until "Disconnected." "cli.ts disconnected cleanly"
  exec 3>&-
  exec 4<&-
  wait "$CLI_PID"
)

log "Checking topic listing through manager routes"
TOPICS_JSON="$(curl -sf "http://localhost:$PORT/apis/v1/namespaces/$MANAGER_NAMESPACE/workspaces/$WORKSPACE_NAME/topics")"
require_output "$TOPICS_JSON" "smoke-topic" "proxied GET /topics lists the connected topic"

CLI_TOPICS_OUTPUT="$(
  cd "$AW_DIR"
  WS_MANAGER="http://localhost:$PORT" bun run cli.ts topics "$WORKSPACE_NAME"
)"
require_output "$CLI_TOPICS_OUTPUT" "smoke-topic" "cli.ts topics lists the connected topic"

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
