#!/bin/sh
set -eu

usage() {
  echo "Usage: scripts/build-deb.sh VERSION ARCH BINARY OUTPUT_DIR" >&2
}

[ "$#" -eq 4 ] || { usage; exit 2; }
VERSION=$1
ARCH=$2
BINARY=$3
OUTPUT_DIR=$4
PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

command -v dpkg-deb >/dev/null 2>&1 || { echo "dpkg-deb is required" >&2; exit 1; }
printf '%s\n' "$VERSION" | /bin/grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$' || {
  echo "version must be semantic and start with v" >&2
  exit 2
}
case "$ARCH" in
  amd64|arm64) ;;
  *) echo "unsupported Debian architecture: $ARCH" >&2; exit 2 ;;
esac
[ -f "$BINARY" ] && [ -x "$BINARY" ] || { echo "binary is not executable: $BINARY" >&2; exit 2; }

DEB_VERSION=${VERSION#v}
FILE_VERSION=$DEB_VERSION
case "$DEB_VERSION" in
  *-*)
    DEB_BASE=${DEB_VERSION%%-*}
    DEB_PRERELEASE=${DEB_VERSION#*-}
    DEB_PRERELEASE=$(printf '%s\n' "$DEB_PRERELEASE" | /usr/bin/tr '-' '.')
    DEB_VERSION="${DEB_BASE}~${DEB_PRERELEASE}"
    ;;
esac
printf '%s\n' "$DEB_VERSION" | /bin/grep -Eq '^[0-9][0-9A-Za-z.+~]*$' || {
  echo "version cannot be represented as a Debian version: $VERSION" >&2
  exit 2
}

TMP_DIR=$(mktemp -d)
trap 'rm -rf -- "$TMP_DIR"' EXIT HUP INT TERM
PACKAGE_ROOT="$TMP_DIR/package"
INSTALLER_ROOT="$PACKAGE_ROOT/usr/share/rootbroker/installer"
DOC_ROOT="$PACKAGE_ROOT/usr/share/doc/rootbroker"
mkdir -p "$OUTPUT_DIR" "$PACKAGE_ROOT/DEBIAN" \
  "$PACKAGE_ROOT/usr/bin" "$PACKAGE_ROOT/usr/sbin" \
  "$PACKAGE_ROOT/usr/libexec/rootbroker" "$INSTALLER_ROOT/packaging/config" \
  "$INSTALLER_ROOT/packaging/systemd" "$DOC_ROOT"

install -m 0755 "$BINARY" "$PACKAGE_ROOT/usr/libexec/rootbroker/rootbroker"
install -m 0755 "$PROJECT_DIR/packaging/debian/rootbroker-setup" "$PACKAGE_ROOT/usr/sbin/rootbroker-setup"
ln -s ../libexec/rootbroker/rootbroker "$PACKAGE_ROOT/usr/bin/rootbroker"
ln -s ../libexec/rootbroker/rootbroker "$PACKAGE_ROOT/usr/bin/rootbroker-admin"
ln -s ../libexec/rootbroker/rootbroker "$PACKAGE_ROOT/usr/sbin/rootbrokerd"

install -m 0755 "$PROJECT_DIR/install.sh" "$PROJECT_DIR/uninstall.sh" \
  "$PROJECT_DIR/migrate-private-prealpha.sh" "$INSTALLER_ROOT/"
install -m 0755 "$PROJECT_DIR/migrate-private-prealpha.sh" \
  "$PACKAGE_ROOT/usr/sbin/rootbroker-migrate-private-prealpha"
cp -R "$PROJECT_DIR/profiles" "$INSTALLER_ROOT/profiles"
install -m 0644 "$PROJECT_DIR/packaging/config/config.json.in" "$INSTALLER_ROOT/packaging/config/"
install -m 0644 "$PROJECT_DIR/packaging/systemd/rootbrokerd.service" "$INSTALLER_ROOT/packaging/systemd/"
for document in README.md README.zh-CN.md SECURITY.md COMPATIBILITY.md MIGRATION.md UPGRADE.md UNINSTALL.md TROUBLESHOOTING.md THREAT_MODEL.md CHANGELOG.md LICENSE; do
  install -m 0644 "$PROJECT_DIR/$document" "$DOC_ROOT/$document"
done

install -m 0755 "$PROJECT_DIR/packaging/debian/postinst" "$PACKAGE_ROOT/DEBIAN/postinst"
install -m 0755 "$PROJECT_DIR/packaging/debian/prerm" "$PACKAGE_ROOT/DEBIAN/prerm"
INSTALLED_SIZE=$(du -sk "$PACKAGE_ROOT/usr" | /usr/bin/awk '{print $1}')
/bin/sed \
  -e "s|@DEB_VERSION@|$DEB_VERSION|g" \
  -e "s|@ARCH@|$ARCH|g" \
  -e "s|@INSTALLED_SIZE@|$INSTALLED_SIZE|g" \
  "$PROJECT_DIR/packaging/debian/control.in" >"$PACKAGE_ROOT/DEBIAN/control"

if [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
  printf '%s\n' "$SOURCE_DATE_EPOCH" | /bin/grep -Eq '^[0-9]+$' || {
    echo "SOURCE_DATE_EPOCH must be an integer" >&2
    exit 2
  }
  find "$PACKAGE_ROOT" -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
fi

OUTPUT="$OUTPUT_DIR/rootbroker_${FILE_VERSION}_${ARCH}.deb"
dpkg-deb --root-owner-group -Zxz -z9 --build "$PACKAGE_ROOT" "$OUTPUT"
echo "$OUTPUT"
