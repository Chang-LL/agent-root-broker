#!/bin/sh
set -eu

case "$(/usr/bin/basename -- "$0"):$*" in
  "hostctl-bin:version")
    echo "hostctl 0.2.0-dev+system-fixture"
    ;;
  "hostctl-admin:home-access revoke")
    echo revoked >/tmp/rootbroker-prealpha-revoke-called
    ;;
  "hostctld:"*)
    exec /bin/sleep 3600
    ;;
  *)
    exit 2
    ;;
esac
