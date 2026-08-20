#!/bin/sh
set -eu

usage() {
  echo "Usage: scripts/render-homebrew-formula.sh VERSION AMD64_URL AMD64_SHA256 ARM64_URL ARM64_SHA256 OUTPUT" >&2
}

[ "$#" -eq 6 ] || { usage; exit 2; }
VERSION=${1#v}
AMD64_URL=$2
AMD64_SHA256=$3
ARM64_URL=$4
ARM64_SHA256=$5
OUTPUT=$6
PROJECT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

printf '%s\n' "$VERSION" | /usr/bin/grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$' || {
  echo "invalid formula version" >&2
  exit 2
}
for checksum in "$AMD64_SHA256" "$ARM64_SHA256"; do
  printf '%s\n' "$checksum" | /usr/bin/grep -Eq '^[0-9a-f]{64}$' || {
    echo "invalid archive checksum" >&2
    exit 2
  }
done
case "$AMD64_URL:$ARM64_URL" in
  https://*:https://*) ;;
  *) echo "formula URLs must use HTTPS" >&2; exit 2 ;;
esac

/usr/bin/awk \
  -v version="$VERSION" \
  -v amd64_url="$AMD64_URL" -v amd64_sha="$AMD64_SHA256" \
  -v arm64_url="$ARM64_URL" -v arm64_sha="$ARM64_SHA256" '
  {
    gsub(/@VERSION@/, version)
    gsub(/@AMD64_URL@/, amd64_url)
    gsub(/@AMD64_SHA256@/, amd64_sha)
    gsub(/@ARM64_URL@/, arm64_url)
    gsub(/@ARM64_SHA256@/, arm64_sha)
    print
  }
' "$PROJECT_DIR/packaging/homebrew/agent-root-broker.rb.in" >"$OUTPUT"
