#!/bin/sh
set -eu

[ "$(uname -s)" = Linux ] || { echo "SKIP: Linux required"; exit 0; }
[ "$(id -u)" -eq 0 ] || { echo "Debian package test must run as root" >&2; exit 1; }

PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TEST_DIR=$(mktemp -d /tmp/rootbroker-deb-test.XXXXXX)
PACKAGE_INSTALLED=0
ROOTBROKER_CONFIGURED=0
APPROVER_USER=rootbroker-pkg-approver
APPROVER_CREATED=0
cleanup() {
  if [ "$ROOTBROKER_CONFIGURED" -eq 1 ] && [ -x /usr/local/sbin/rootbroker-uninstall ]; then
    /usr/local/sbin/rootbroker-uninstall --purge-agent-account >/dev/null 2>&1 || true
  fi
  if [ "$PACKAGE_INSTALLED" -eq 1 ]; then
    dpkg --purge rootbroker >/dev/null 2>&1 || true
  fi
  if [ "$APPROVER_CREATED" -eq 1 ]; then
    userdel -r "$APPROVER_USER" >/dev/null 2>&1 || true
  fi
  rm -rf -- /home/grok-agent
  rm -rf -- "$TEST_DIR"
}
trap cleanup EXIT HUP INT TERM

! getent passwd "$APPROVER_USER" >/dev/null || { echo "test approver already exists" >&2; exit 1; }
! getent passwd grok-agent >/dev/null || { echo "test agent already exists" >&2; exit 1; }
useradd --create-home --shell /bin/sh "$APPROVER_USER"
APPROVER_CREATED=1

BINARY="$TEST_DIR/rootbroker"
FAKE_GROK="$TEST_DIR/grok-system-agent"
GOCACHE="$TEST_DIR/go-cache" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags '-s -w -X main.version=v0.1.0-alpha.1' -o "$BINARY" "$PROJECT_DIR/cmd/rootbroker"
GOCACHE="$TEST_DIR/go-cache" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -o "$FAKE_GROK" "$PROJECT_DIR/tests/systemagent"
mkdir "$TEST_DIR/first" "$TEST_DIR/second"
SOURCE_DATE_EPOCH=1700000000 "$PROJECT_DIR/scripts/build-deb.sh" \
  v0.1.0-alpha.1 amd64 "$BINARY" "$TEST_DIR/first"
SOURCE_DATE_EPOCH=1700000000 "$PROJECT_DIR/scripts/build-deb.sh" \
  v0.1.0-alpha.1 amd64 "$BINARY" "$TEST_DIR/second"
cmp "$TEST_DIR/first/rootbroker_0.1.0-alpha.1_amd64.deb" \
  "$TEST_DIR/second/rootbroker_0.1.0-alpha.1_amd64.deb"
PACKAGE="$TEST_DIR/first/rootbroker_0.1.0-alpha.1_amd64.deb"
[ -f "$PACKAGE" ] || { echo "expected package was not built" >&2; exit 1; }
dpkg-deb --info "$PACKAGE" | /bin/grep -q 'Package: rootbroker'
dpkg-deb --contents "$PACKAGE" | /bin/grep -q './usr/sbin/rootbroker-setup'
dpkg-deb --contents "$PACKAGE" | /bin/grep -q './usr/sbin/rootbroker-migrate-private-prealpha'
dpkg -i "$PACKAGE"
PACKAGE_INSTALLED=1
[ "$(/usr/bin/rootbroker version)" = 'rootbroker v0.1.0-alpha.1' ]
/usr/sbin/rootbroker-setup --help >/dev/null
/usr/sbin/rootbroker-migrate-private-prealpha --help >/dev/null
/usr/sbin/rootbroker-setup \
  --profile grok \
  --approver-user "$APPROVER_USER" \
  --agent-bin "$FAKE_GROK" >"$TEST_DIR/setup.log"
ROOTBROKER_CONFIGURED=1
systemctl is-active --quiet rootbrokerd.service
[ "$(/usr/local/bin/rootbroker version)" = 'rootbroker v0.1.0-alpha.1' ]
/usr/bin/rootbroker --json doctor | /bin/grep -q '"socketIsUnix":true'

if dpkg --remove rootbroker >"$TEST_DIR/remove-configured.log" 2>&1; then
  echo "configured package removal unexpectedly succeeded" >&2
  exit 1
fi
/bin/grep -q 'Run sudo rootbroker-uninstall' "$TEST_DIR/remove-configured.log"
/usr/local/sbin/rootbroker-uninstall --purge-agent-account >"$TEST_DIR/uninstall.log"
ROOTBROKER_CONFIGURED=0
[ -d /home/grok-agent ]
dpkg --purge rootbroker
PACKAGE_INSTALLED=0
[ ! -e /usr/bin/rootbroker ]
userdel -r "$APPROVER_USER"
APPROVER_CREATED=0

CHECKSUM=$(sha256sum "$BINARY" | /usr/bin/awk '{print $1}')
"$PROJECT_DIR/scripts/render-homebrew-formula.sh" v0.1.0-alpha.1 \
  https://example.invalid/rootbroker-amd64.tar.gz "$CHECKSUM" \
  https://example.invalid/rootbroker-arm64.tar.gz "$CHECKSUM" \
  "$TEST_DIR/agent-root-broker.rb"
ruby -c "$TEST_DIR/agent-root-broker.rb" >/dev/null
/bin/grep -q 'class AgentRootBroker < Formula' "$TEST_DIR/agent-root-broker.rb"
/bin/grep -q 'depends_on :linux' "$TEST_DIR/agent-root-broker.rb"

echo "PASS: Debian carrier package build, install, entry points, and removal"
