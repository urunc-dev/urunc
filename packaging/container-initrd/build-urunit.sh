#!/usr/bin/env bash
# Build the static urunit binary that the container boot initrd ships. urunit
# is the early init, the supervisor and the exec agent of the guest, in one
# binary. It lives in its own repository and urunit.pin names the commit.
#
# The build runs with musl in an Alpine container. Only the binary comes out,
# owned by the caller, so a rootful container tool leaves no root-owned files
# behind.
#
# Environment:
#   URUNIT_SRC   a local urunit checkout to build instead of the pinned commit
#   TARGET_ARCH  x86_64|amd64 or aarch64|arm64 (default: the host arch)
#   CNTR_TOOL    container tool for the build (default: podman)
#
# Usage: ./build-urunit.sh OUT
set -euo pipefail

[ $# -eq 1 ] || { echo "usage: $0 OUT" >&2; exit 1; }
out="$1"
here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091 # urunit.pin holds plain NAME=value lines
. "$here/urunit.pin"

case "${TARGET_ARCH:-$(uname -m)}" in
  arm64|aarch64) platform=linux/arm64 ;;
  x86_64|amd64)  platform=linux/amd64 ;;
  *) echo "ERROR: unsupported TARGET_ARCH=${TARGET_ARCH:-$(uname -m)}" >&2; exit 1 ;;
esac

cntr="${CNTR_TOOL:-podman}"
command -v "$cntr" >/dev/null || { echo "ERROR: $cntr is required to build urunit" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [ -n "${URUNIT_SRC:-}" ]; then
  [ -d "$URUNIT_SRC" ] || { echo "ERROR: URUNIT_SRC is not a directory: $URUNIT_SRC" >&2; exit 1; }
  src="$(cd "$URUNIT_SRC" && pwd)"
else
  command -v git >/dev/null || { echo "ERROR: git is required to fetch urunit (or set URUNIT_SRC)" >&2; exit 1; }
  src="$work/src"
  git init -q "$src"
  git -C "$src" fetch -q --depth 1 "$URUNIT_REPO" "$URUNIT_REF"
  git -C "$src" checkout -q FETCH_HEAD
fi

mkdir "$work/out"
# The single-quoted script expands HOST_UID and HOST_GID in the container.
# shellcheck disable=SC2016
"$cntr" run --rm --platform "$platform" \
  -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
  -v "$src:/src:ro" -v "$work/out:/out" \
  docker.io/library/alpine:3.20 \
  sh -c 'apk add -q build-base linux-headers && cp -r /src /tmp/urunit && rm -rf /tmp/urunit/dist &&
         make -C /tmp/urunit static BUILD_DIR=/tmp/urunit/dist &&
         cp /tmp/urunit/dist/urunit_static /out/urunit && chown "$HOST_UID:$HOST_GID" /out/urunit'
[ -s "$work/out/urunit" ] || { echo "ERROR: building urunit produced no binary" >&2; exit 1; }
install -m0755 "$work/out/urunit" "$out"
