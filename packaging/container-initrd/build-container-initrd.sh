#!/usr/bin/env bash
# Build the boot initrd of a generic container boot: the initrd that
# com.urunc.unikernel.bootInitrd points at, paired with a Linux kernel through
# com.urunc.unikernel.bootKernel. It contains only what has to run before the
# container's own rootfs takes over:
#   /init          the early userspace script next to this file (container-init)
#   /busybox       a static BusyBox, for the shell and the mount/switch_root tools
#   /urunit        the guest init that becomes PID 1 (URUNIT)
#   /urunit-agent  the in-guest exec agent, cross-built from this checkout
#                  unless URUNIT_AGENT names a prebuilt static Linux binary
#
# The archive is left uncompressed (cpio newc), because urunc appends the
# per-container urunit configuration to a copy of it and the kernel walks the
# concatenated archives.
#
# Environment:
#   URUNIT        path to a static urunit binary for the target arch (required)
#   URUNIT_AGENT  prebuilt urunit-agent; built with go when unset
#   BUSYBOX       local static busybox; downloaded from BUSYBOX_URL when unset
#   TARGET_ARCH   x86_64 (default: the host arch) or aarch64
#
# Usage: ./build-container-initrd.sh [INITRD_OUT]
set -euo pipefail

INITRD="${1:-/opt/urunc/container-initrd/container-initrd}"
here="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

case "${TARGET_ARCH:-$(uname -m)}" in
  arm64|aarch64) busybox_arch=aarch64; agent_goarch=arm64 ;;
  x86_64|amd64)  busybox_arch=x86_64;  agent_goarch=amd64 ;;
  *) echo "ERROR: unsupported TARGET_ARCH=${TARGET_ARCH:-$(uname -m)}" >&2; exit 1 ;;
esac
BUSYBOX_URL="${BUSYBOX_URL:-https://busybox.net/downloads/binaries/1.35.0-${busybox_arch}-linux-musl/busybox}"

command -v cpio >/dev/null || { echo "ERROR: cpio is required (apt-get install -y cpio)" >&2; exit 1; }
[ -n "${URUNIT:-}" ] && [ -s "$URUNIT" ] || { echo "ERROR: URUNIT must name a static urunit binary" >&2; exit 1; }

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

install -m0755 "$here/container-init" "$stage/init"
install -m0755 "$URUNIT" "$stage/urunit"

# Must be named exactly "busybox": busybox dispatches `busybox <applet>` only
# when argv[0]'s basename is "busybox".
if [ -n "${BUSYBOX:-}" ]; then
  install -m0755 "$BUSYBOX" "$stage/busybox"
else
  curl -fsSL "$BUSYBOX_URL" -o "$stage/busybox" || { echo "ERROR: could not download BusyBox from $BUSYBOX_URL" >&2; exit 1; }
  chmod 0755 "$stage/busybox"
fi
[ -s "$stage/busybox" ] || { echo "ERROR: BusyBox binary is empty" >&2; exit 1; }

# urunit-agent runs inside the container's root before any loader or library
# of the image can be assumed, so it has to be a static Linux binary.
if [ -n "${URUNIT_AGENT:-}" ]; then
  [ -s "$URUNIT_AGENT" ] || { echo "ERROR: URUNIT_AGENT is missing or empty: $URUNIT_AGENT" >&2; exit 1; }
  install -m0755 "$URUNIT_AGENT" "$stage/urunit-agent"
else
  command -v go >/dev/null || { echo "ERROR: go is required to build urunit-agent" >&2; exit 1; }
  (cd "$repo_root" && CGO_ENABLED=0 GOOS=linux GOARCH="$agent_goarch" \
    go build -trimpath -ldflags "-s -w" -o "$stage/urunit-agent" ./cmd/urunit-agent)
fi

# The mountpoints /init uses.
mkdir -p "$stage/proc" "$stage/dev" "$stage/newroot" "$stage/run"

mkdir -p "$(dirname "$INITRD")"
( cd "$stage" && find . -mindepth 1 -print | LC_ALL=C sort | cpio -o -H newc --quiet ) > "$INITRD"

echo "Container boot initrd: $INITRD ($(stat -c%s "$INITRD") bytes, cpio newc, uncompressed)"
echo "Pass it as com.urunc.unikernel.bootInitrd together with a kernel as com.urunc.unikernel.bootKernel."
