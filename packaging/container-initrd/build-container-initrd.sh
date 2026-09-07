#!/usr/bin/env bash
# Build a generic container-boot initrd without modifying the OCI image.
#
# Produces two artifacts:
#   1. $PAYLOAD/ contains the files consumed by
#      AugmentInitrdForContainerBoot: init, vz-init, busybox and urunit-agent.
#   2. $INITRD is a complete, self-contained cpio(newc) boot initrd. It is left
#      UNCOMPRESSED so urunc's cpio-append augmentation concatenates cleanly
#      (the kernel initramfs loader walks concatenated archives).
#
# BUSYBOX_URL may override the static BusyBox download. URUNIT_AGENT may name a
# prebuilt Linux guest binary; otherwise the agent is cross-built from this
# checkout for TARGET_ARCH.
# Usage: ./build-container-initrd.sh [PAYLOAD_DIR] [INITRD_OUT]
set -euo pipefail

PAYLOAD="${1:-/opt/urunc/container-initrd/payload}"
INITRD="${2:-/opt/urunc/container-initrd/container-initrd}"
case "${TARGET_ARCH:-$(uname -m)}" in
  arm64|aarch64) busybox_arch=aarch64; agent_goarch=arm64 ;;
  x86_64|amd64)  busybox_arch=x86_64;  agent_goarch=amd64 ;;
  *) echo "ERROR: unsupported TARGET_ARCH=${TARGET_ARCH:-$(uname -m)}"; exit 1 ;;
esac
if [ "$busybox_arch" = aarch64 ]; then
  # busybox.net publishes no 64-bit ARM binary. Alpine's busybox-static package
  # is dependency-free and contains /bin/busybox.static.
  BUSYBOX_APK_URL="${BUSYBOX_APK_URL:-https://dl-cdn.alpinelinux.org/alpine/v3.23/main/aarch64/busybox-static-1.37.0-r30.apk}"
else
  BUSYBOX_URL="${BUSYBOX_URL:-https://busybox.net/downloads/binaries/1.35.0-x86_64-linux-musl/busybox}"
fi
here="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

command -v cpio >/dev/null || { echo "ERROR: cpio required (apt-get install -y cpio)"; exit 1; }

# Use sudo only when the selected output directory actually requires it. This
# keeps /tmp and user-owned macOS build directories entirely unprivileged while
# preserving the /opt defaults used by host installations.
need_sudo=false
if ! mkdir -p "$PAYLOAD" "$(dirname "$INITRD")" 2>/dev/null || [ ! -w "$PAYLOAD" ] || [ ! -w "$(dirname "$INITRD")" ]; then
  sudo mkdir -p "$PAYLOAD" "$(dirname "$INITRD")"
  need_sudo=true
fi
install_file() {
  if $need_sudo; then sudo install "$@"; else install "$@"; fi
}

# ---- 1. stage the reusable payload directory ----
install_file -m0755 "$here/container-init" "$PAYLOAD/init"
install_file -m0755 "$here/vz-init"        "$PAYLOAD/vz-init"
# NOTE: staged as "busybox": busybox dispatches `busybox
# <applet>` only when argv[0]'s basename is exactly "busybox"; a rename makes
# every invocation exit 127 (applet not found), panicking init.
if [ ! -s "$PAYLOAD/busybox" ] || [ ! -x "$PAYLOAD/busybox" ]; then
  tmp_busybox="$(mktemp)"
  if [ "$busybox_arch" = aarch64 ] && [ -z "${BUSYBOX_URL:-}" ]; then
    apk="$(mktemp)"
    apk_dir="$(mktemp -d)"
    if ! curl -fsSL "$BUSYBOX_APK_URL" -o "$apk" ||
       ! tar -xzf "$apk" -C "$apk_dir" bin/busybox.static; then
      rm -f "$apk" "$tmp_busybox"
      rm -rf "$apk_dir"
      echo "ERROR: could not fetch/extract ARM64 busybox-static" >&2
      exit 1
    fi
    install -m0755 "$apk_dir/bin/busybox.static" "$tmp_busybox"
    rm -f "$apk"
    rm -rf "$apk_dir"
  elif ! curl -fsSL "$BUSYBOX_URL" -o "$tmp_busybox"; then
    rm -f "$tmp_busybox"
    echo "ERROR: could not download static BusyBox" >&2
    exit 1
  fi
  [ -s "$tmp_busybox" ] || { echo "ERROR: downloaded BusyBox is empty" >&2; exit 1; }
  chmod +x "$tmp_busybox"
  install_file -m0755 "$tmp_busybox" "$PAYLOAD/busybox"
  rm -f "$tmp_busybox"
fi

# urunit-agent must be a static Linux binary: it starts inside the container's
# root before any image-provided dynamic loader or libraries can be assumed.
tmp_agent="$(mktemp)"
if [ -n "${URUNIT_AGENT:-}" ]; then
  [ -s "$URUNIT_AGENT" ] || {
    rm -f "$tmp_agent"
    echo "ERROR: URUNIT_AGENT is missing or empty: $URUNIT_AGENT" >&2
    exit 1
  }
  install -m0755 "$URUNIT_AGENT" "$tmp_agent"
else
  command -v go >/dev/null || {
    rm -f "$tmp_agent"
    echo "ERROR: go required to build urunit-agent" >&2
    exit 1
  }
  if ! (cd "$repo_root" && env CGO_ENABLED=0 GOOS=linux GOARCH="$agent_goarch" \
    go build -trimpath -o "$tmp_agent" ./cmd/urunit-agent); then
    rm -f "$tmp_agent"
    echo "ERROR: could not build Linux/$agent_goarch urunit-agent" >&2
    exit 1
  fi
fi
[ -s "$tmp_agent" ] || { echo "ERROR: urunit-agent build is empty" >&2; exit 1; }
install_file -m0755 "$tmp_agent" "$PAYLOAD/urunit-agent"
rm -f "$tmp_agent"

# ---- 2. assemble the complete, uncompressed boot initrd ----
stage="$(mktemp -d)"
install -m0755 "$PAYLOAD/init"              "$stage/init"
install -m0755 "$PAYLOAD/vz-init"           "$stage/vz-init"
install -m0755 "$PAYLOAD/busybox"           "$stage/busybox"
install -m0755 "$PAYLOAD/urunit-agent"       "$stage/urunit-agent"
# minimal mountpoints the injected /init expects
mkdir -p "$stage/proc" "$stage/sys" "$stage/dev" "$stage/newroot" "$stage/lowerroot" "$stage/run" "$stage/var/log"
archive="$(mktemp)"
( cd "$stage" && find . -mindepth 1 -print | LC_ALL=C sort | cpio -o -H newc 2>/dev/null ) > "$archive"
install_file -m0644 "$archive" "$INITRD"
rm -f "$archive"
rm -rf "$stage"

case "$(uname -s)" in
  Darwin) initrd_size="$(stat -f%z "$INITRD")" ;;
  *)      initrd_size="$(stat -c%s "$INITRD")" ;;
esac
echo "== Container-boot payload: $PAYLOAD =="; ls -la "$PAYLOAD"
echo "== Container initrd:       $INITRD ($initrd_size bytes, cpio newc, uncompressed) =="
echo
echo "Use $INITRD as the boot initrd and append workload metadata as needed."
