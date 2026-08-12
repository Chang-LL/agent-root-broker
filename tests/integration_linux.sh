#!/bin/sh
set -eu

[ "$(uname -s)" = Linux ] || { echo "SKIP: Linux required"; exit 0; }

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TEST_DIR=$(mktemp -d /tmp/hostctl-go-integration.XXXXXX)
DAEMON_PID=
AGENT_PID=
cleanup() {
  [ -z "$AGENT_PID" ] || kill "$AGENT_PID" 2>/dev/null || true
  [ -z "$DAEMON_PID" ] || kill "$DAEMON_PID" 2>/dev/null || true
  [ -z "$DAEMON_PID" ] || wait "$DAEMON_PID" 2>/dev/null || true
  rm -rf -- "$TEST_DIR"
}
trap cleanup EXIT HUP INT TERM

BIN="$TEST_DIR/hostctl-bin"
RUNTIME="$TEST_DIR/run"
CONFIG="$TEST_DIR/config.json"
OUTPUT="$TEST_DIR/agent-output.jsonl"
CURRENT_USER=$(id -un)
CURRENT_GROUP=$(id -gn)
SHELL_EXE=$(readlink -f /bin/sh)

if [ -n "${HOSTCTL_TEST_BIN:-}" ]; then
  cp "$HOSTCTL_TEST_BIN" "$BIN"
  chmod 0755 "$BIN"
else
  GOCACHE=${GOCACHE:-/tmp/hostctl-go-cache} \
  GOPATH=${GOPATH:-/tmp/hostctl-go-path} \
  CGO_ENABLED=0 go build -trimpath -o "$BIN" "$PROJECT_DIR/cmd/hostctl"
fi

mkdir -p "$RUNTIME"
sed \
  -e "s|@RUNTIME@|$RUNTIME|g" \
  -e "s|@USER@|$CURRENT_USER|g" \
  -e "s|@GROUP@|$CURRENT_GROUP|g" \
  -e "s|@SHELL@|$SHELL_EXE|g" \
  "$PROJECT_DIR/tests/integration_config.json.in" >"$CONFIG"

"$BIN" hostctld --config "$CONFIG" >"$TEST_DIR/daemon.log" 2>&1 &
DAEMON_PID=$!
for _ in $(seq 1 100); do
  [ -S "$RUNTIME/admin.sock" ] && [ -S "$RUNTIME/request.sock" ] && break
  kill -0 "$DAEMON_PID" 2>/dev/null || { cat "$TEST_DIR/daemon.log"; exit 1; }
  sleep 0.05
done
[ -S "$RUNTIME/admin.sock" ] || { echo "admin socket did not start"; exit 1; }

HOSTCTL_SOCKET="$RUNTIME/request.sock" /bin/sh -c '
  bin=$1
  printf "%s\n" "{\"hookEventName\":\"session_start\",\"sessionId\":\"integration-session\"}" | "$bin" hostctl-grok-hook
  printf "%s\n" "{\"hookEventName\":\"user_prompt_submit\",\"sessionId\":\"integration-session\"}" | "$bin" hostctl-grok-hook
  "$bin" hostctl sudo --json -- /usr/bin/id -u
  "$bin" hostctl sudo --json -- /bin/echo lease-ok
  printf "%s\n" "{\"hookEventName\":\"stop\",\"sessionId\":\"integration-session\",\"reason\":\"end_turn\"}" | "$bin" hostctl-grok-hook
  "$bin" hostctl sudo --json -- /usr/bin/true || true
' hostctl-integration "$BIN" >"$OUTPUT" 2>&1 &
AGENT_PID=$!

REQUEST_ID=
for _ in $(seq 1 100); do
  PENDING=$(HOSTCTL_ADMIN_SOCKET="$RUNTIME/admin.sock" "$BIN" hostctl-admin --json pending)
  REQUEST_ID=$(printf '%s\n' "$PENDING" | sed -n 's/.*"id":"\([0-9a-f]*\)".*/\1/p')
  [ -n "$REQUEST_ID" ] && break
  sleep 0.05
done
[ -n "$REQUEST_ID" ] || { cat "$TEST_DIR/daemon.log"; cat "$OUTPUT"; exit 1; }
HOSTCTL_ADMIN_SOCKET="$RUNTIME/admin.sock" "$BIN" hostctl-admin approve "$REQUEST_ID" --scope message
wait "$AGENT_PID"
AGENT_PID=

grep -q '"ok":true' "$OUTPUT"
grep -q '"approvalScope":"message"' "$OUTPUT"
grep -q 'lease-ok' "$OUTPUT"
grep -q '"code":"no_active_turn"' "$OUTPUT"
HOSTCTL_ADMIN_SOCKET="$RUNTIME/admin.sock" "$BIN" hostctl-admin --json leases | grep -q '"leases":\[\]'

echo "PASS: Go Linux socket integration"
