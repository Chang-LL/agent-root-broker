#!/bin/sh

set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH

repository_url="https://dl.cloudsmith.io/public/lc-software/agent-root-broker/deb"
key_url="https://dl.cloudsmith.io/public/lc-software/agent-root-broker/gpg.1C034B0267F8FDD9.key"
key_fingerprint="15AC793B1EA501CF36930F021C034B0267F8FDD9"
keyring_path="/usr/share/keyrings/lc-software-agent-root-broker-archive-keyring.gpg"
source_path="/etc/apt/sources.list.d/lc-software-agent-root-broker.list"
component="alpha"

usage() {
	cat <<'EOF'
Usage: setup-apt-repository.sh [--component alpha|main]

Configure the signed Agent Root Broker Cloudsmith APT repository.
Only the binary repository is enabled; Debian source indexes are not requested.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--component)
		[ "$#" -ge 2 ] || {
			echo "--component requires a value" >&2
			exit 2
		}
		component=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

case "$component" in
alpha | main) ;;
*)
	echo "unsupported component: $component (expected alpha or main)" >&2
	exit 2
	;;
esac

[ "$(id -u)" -eq 0 ] || {
	echo "run this script as root" >&2
	exit 1
}

[ -r /etc/os-release ] || {
	echo "/etc/os-release is missing or unreadable" >&2
	exit 1
}

# shellcheck disable=SC1091
. /etc/os-release
distro=${ID:-}
codename=${VERSION_CODENAME:-}
case "$distro" in
debian | ubuntu) ;;
*)
	echo "unsupported distribution: ${distro:-unknown} (expected debian or ubuntu)" >&2
	exit 1
	;;
esac
case "$codename" in
'' | *[!a-z0-9._-]*)
	echo "invalid or missing VERSION_CODENAME in /etc/os-release" >&2
	exit 1
	;;
esac

for command_name in apt-get awk curl gpg install mktemp; do
	command -v "$command_name" >/dev/null 2>&1 || {
		echo "required command is missing: $command_name" >&2
		exit 1
	}
done

work_dir=$(mktemp -d /tmp/agent-root-broker-apt.XXXXXX)
key_source="$work_dir/repository-key.asc"
keyring_candidate="$work_dir/repository-key.gpg"
source_candidate="$work_dir/agent-root-broker.list"
cleanup() {
	rm -rf -- "$work_dir"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Downloading the Agent Root Broker repository signing key..."
curl -1fsSLo "$key_source" "$key_url"
key_listing=$(gpg --batch --no-options --homedir "$work_dir" --show-keys --with-colons "$key_source")
actual_fingerprint=$(printf '%s\n' "$key_listing" | awk -F: '$1 == "fpr" { print $10; exit }')
primary_key_count=$(printf '%s\n' "$key_listing" | awk -F: '$1 == "pub" { count++ } END { print count + 0 }')
[ "$primary_key_count" -eq 1 ] || {
	echo "repository key file contains $primary_key_count primary keys; expected exactly one" >&2
	exit 1
}
[ "$actual_fingerprint" = "$key_fingerprint" ] || {
	echo "repository signing key fingerprint mismatch" >&2
	echo "expected: $key_fingerprint" >&2
	echo "actual:   ${actual_fingerprint:-missing}" >&2
	exit 1
}

gpg --batch --no-options --homedir "$work_dir" --yes --dearmor \
	--output "$keyring_candidate" "$key_source"
install -D -m 0644 "$keyring_candidate" "$keyring_path"

cat >"$source_candidate" <<EOF
# Agent Root Broker binary packages hosted by Cloudsmith.
# Source packages are not currently published, so deb-src is intentionally omitted.
deb [signed-by=$keyring_path] $repository_url/$distro $codename $component
EOF
install -D -m 0644 "$source_candidate" "$source_path"

echo "Refreshing APT package metadata..."
apt-get update
echo "Configured Agent Root Broker APT repository: $distro $codename $component"
echo "Source list: $source_path"
