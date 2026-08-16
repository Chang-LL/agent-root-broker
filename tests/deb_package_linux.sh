#!/bin/sh
set -eu

[ "$(uname -s)" = Linux ] || { echo "SKIP: Linux required"; exit 0; }
[ "$(id -u)" -eq 0 ] || { echo "Debian package test must run as root" >&2; exit 1; }

PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TEST_DIR=$(mktemp -d /tmp/rootbroker-deb-test.XXXXXX)
PACKAGE_INSTALLED=0
CONFIG_MARKER_CREATED=0
cleanup() {
  if [ "$CONFIG_MARKER_CREATED" -eq 1 ]; then
    rm -f -- /var/lib/rootbroker/install-state
    rmdir /var/lib/rootbroker 2>/dev/null || true
  fi
  if [ "$PACKAGE_INSTALLED" -eq 1 ]; then
    dpkg --purge rootbroker >/dev/null 2>&1 || true
  fi
  rm -rf -- "$TEST_DIR"
}
trap cleanup EXIT HUP INT TERM

BINARY="$TEST_DIR/rootbroker"
GOCACHE="$TEST_DIR/go-cache" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags '-s -w -X main.version=v0.1.0-alpha.1' -o "$BINARY" "$PROJECT_DIR/cmd/rootbroker"
mkdir "$TEST_DIR/first" "$TEST_DIR/second"
SOURCE_DATE_EPOCH=1700000000 "$PROJECT_DIR/scripts/build-deb.sh" \
  v0.1.0-alpha.1 amd64 "$BINARY" "$TEST_DIR/first"
SOURCE_DATE_EPOCH=1700000000 "$PROJECT_DIR/scripts/build-deb.sh" \
  v0.1.0-alpha.1 amd64 "$BINARY" "$TEST_DIR/second"
cmp "$TEST_DIR/first/rootbroker_0.1.0~alpha.1_amd64.deb" \
  "$TEST_DIR/second/rootbroker_0.1.0~alpha.1_amd64.deb"
PACKAGE="$TEST_DIR/first/rootbroker_0.1.0~alpha.1_amd64.deb"
[ -f "$PACKAGE" ] || { echo "expected package was not built" >&2; exit 1; }
dpkg-deb --info "$PACKAGE" | /bin/grep -q 'Package: rootbroker'
dpkg-deb --contents "$PACKAGE" | /bin/grep -q './usr/sbin/rootbroker-setup'
dpkg -i "$PACKAGE"
PACKAGE_INSTALLED=1
[ "$(/usr/bin/rootbroker version)" = 'rootbroker v0.1.0-alpha.1' ]
/usr/sbin/rootbroker-setup --help >/dev/null

mkdir -p /var/lib/rootbroker
: >/var/lib/rootbroker/install-state
CONFIG_MARKER_CREATED=1
if dpkg --remove rootbroker >"$TEST_DIR/remove-configured.log" 2>&1; then
  echo "configured package removal unexpectedly succeeded" >&2
  exit 1
fi
/bin/grep -q 'Run sudo rootbroker-uninstall' "$TEST_DIR/remove-configured.log"
rm -f -- /var/lib/rootbroker/install-state
rmdir /var/lib/rootbroker
CONFIG_MARKER_CREATED=0
dpkg --purge rootbroker
PACKAGE_INSTALLED=0
[ ! -e /usr/bin/rootbroker ]

CHECKSUM=$(sha256sum "$BINARY" | /usr/bin/awk '{print $1}')
"$PROJECT_DIR/scripts/render-homebrew-formula.sh" v0.1.0-alpha.1 \
  https://example.invalid/rootbroker-amd64.tar.gz "$CHECKSUM" \
  https://example.invalid/rootbroker-arm64.tar.gz "$CHECKSUM" \
  "$TEST_DIR/rootbroker.rb"
ruby -c "$TEST_DIR/rootbroker.rb" >/dev/null
/bin/grep -q 'depends_on :linux' "$TEST_DIR/rootbroker.rb"

echo "PASS: Debian carrier package build, install, entry points, and removal"
