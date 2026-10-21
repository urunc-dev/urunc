#!/usr/bin/env bash
# Build the boot initrd of a generic container boot: the initrd that
# com.urunc.unikernel.bootInitrd points at, paired with a Linux kernel through
# com.urunc.unikernel.bootKernel. It contains only what has to run before the
# container's own rootfs takes over.
#
# Default layout: one static C binary, urunit, in three roles
#   /urunit        early init, PID 1 supervisor and exec agent in one binary
#   /init          symlink to urunit (the role is picked from argv[0])
#   /vz-init       symlink to urunit; hull on macOS boots it with rdinit=/vz-init
#   /dev /newroot /proc /run /sys   empty mountpoints for the early init
#
# INITRD_LAYOUT=legacy selects the previous layout until the new one is
# validated on every host:
#   /init          the early userspace script next to this file (container-init);
#                  hull on macOS can boot the same script and append its own
#                  /urunc-* files, see the comment there
#   /vz-init       the macOS early init (vz-init). hull boots it with
#                  rdinit=/vz-init; Linux hosts boot /init and never run it
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
#   URUNIT        path to a static urunit binary for the target arch. Default
#                 layout: built by build-urunit.sh from the commit urunit.pin
#                 names when unset. Legacy: required,
#                 and it has to read the NUL-terminated records urunc writes
#                 (NOFireAI/urunit de324cf or later); an older urunit splits
#                 values at new lines or runs the command without its
#                 environment, user and cwd
#   INITRD_LAYOUT unset (single binary) or "legacy"
#   TARGET_ARCH   x86_64|amd64 or aarch64|arm64 (default: the host arch)
#   URUNIT_SRC    a local urunit checkout for build-urunit.sh to build
#   CNTR_TOOL     container tool that builds urunit (default: podman)
#   Legacy layout only:
#   URUNIT_AGENT  prebuilt urunit-agent; built with go when unset
#   BUSYBOX       local static busybox; downloaded from BUSYBOX_URL when unset
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

layout="${INITRD_LAYOUT:-single}"
case "$layout" in
  single|legacy) ;;
  *) echo "ERROR: unknown INITRD_LAYOUT=$layout (use legacy or leave it unset)" >&2; exit 1 ;;
esac

stage="$(mktemp -d)"
build_dir=""
trap 'rm -rf "$stage"; if [ -n "$build_dir" ]; then rm -rf "$build_dir"; fi' EXIT

if [ "$layout" = single ]; then
  if [ -n "${URUNIT_AGENT:-}" ]; then
    echo "WARNING: URUNIT_AGENT is ignored by the single-binary layout (INITRD_LAYOUT=legacy uses it)" >&2
  fi
  if [ -n "${BUSYBOX:-}" ]; then
    echo "WARNING: BUSYBOX is ignored by the single-binary layout (INITRD_LAYOUT=legacy uses it)" >&2
  fi

  if [ -n "${URUNIT:-}" ]; then
    [ -s "$URUNIT" ] || { echo "ERROR: URUNIT is missing or empty: $URUNIT" >&2; exit 1; }
    urunit_bin="$URUNIT"
  else
    build_dir="$(mktemp -d)"
    urunit_bin="$build_dir/urunit"
    TARGET_ARCH="$agent_goarch" "$here/build-urunit.sh" "$urunit_bin"
  fi

  install -m0755 "$urunit_bin" "$stage/urunit"
  # The role is picked from argv[0]: rdinit=/vz-init and the kernel's /init
  # reach the same binary.
  ( cd "$stage" && ln -s urunit init && ln -s urunit vz-init )
  # Mountpoints the early init uses.
  mkdir -p "$stage/proc" "$stage/sys" "$stage/dev" "$stage/newroot" "$stage/run"
else
  [ -n "${URUNIT:-}" ] && [ -s "$URUNIT" ] || { echo "ERROR: URUNIT must name a static urunit binary" >&2; exit 1; }

  install -m0755 "$here/container-init" "$stage/init"
  install -m0755 "$here/vz-init" "$stage/vz-init"
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

  # The mountpoints /init uses; /sys for the agent's virtio-port lookup and
  # for /vz-init, which mounts sysfs there.
  mkdir -p "$stage/proc" "$stage/sys" "$stage/dev" "$stage/newroot" "$stage/run"
fi

mkdir -p "$(dirname "$INITRD")"
( cd "$stage" && find . -mindepth 1 -print | LC_ALL=C sort | cpio -o -H newc --quiet ) > "$INITRD"

case "$(uname -s)" in
  Darwin) initrd_size="$(stat -f%z "$INITRD")" ;;
  *)      initrd_size="$(stat -c%s "$INITRD")" ;;
esac
echo "Container boot initrd: $INITRD ($initrd_size bytes, cpio newc, uncompressed)"
echo "Pass it as com.urunc.unikernel.bootInitrd together with a kernel as com.urunc.unikernel.bootKernel."
