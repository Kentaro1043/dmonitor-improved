#!/bin/sh
set -eu

ROOTFS="${DMONITOR_ROOTFS:-/var/lib/dmonitor/rootfs}"
CACHE="${DMONITOR_CACHE:-/var/cache/dmonitor}"
COMPAT_SOURCE="/usr/share/dmonitor-improved/dmonitor-compat.so"
COMPAT_TARGET="$ROOTFS/usr/lib/dmonitor-compat.so"

copy_compat() {
  if [ -f "$COMPAT_SOURCE" ]; then
    mkdir -p "$(dirname "$COMPAT_TARGET")"
    cp "$COMPAT_SOURCE" "$COMPAT_TARGET"
  fi
}

install_rootfs() {
  mkdir -p "$ROOTFS" "$CACHE"
  dmonitor-install -rootfs "$ROOTFS" -cache "$CACHE" -log-level "${DMONITOR_LOG_LEVEL:-info}" "$@"
  copy_compat
}

if [ "${1:-}" = "install-rootfs" ]; then
  shift
  install_rootfs "$@"
  exit 0
fi

if [ "$#" -eq 0 ] || [ "${1#-}" != "$1" ]; then
  if [ "${DMONITOR_AUTO_INSTALL:-1}" = "1" ] && [ ! -x "$ROOTFS/usr/bin/dmonitor" ]; then
    install_rootfs
  else
    copy_compat
  fi

  set -- dmonitor-improved \
    -addr "${DMONITOR_ADDR:-0.0.0.0:8080}" \
    -rootfs "$ROOTFS" \
    -qemu "${DMONITOR_QEMU:-qemu-arm}" \
    -static "${DMONITOR_STATIC:-/usr/share/dmonitor-improved/web}" \
    -preload "${DMONITOR_PRELOAD:-/usr/lib/dmonitor-compat.so}" \
    -dstar-device "${DMONITOR_DSTAR_DEVICE:-/dev/dstar}" \
    -log-level "${DMONITOR_LOG_LEVEL:-info}" \
    "$@"
fi

exec "$@"
