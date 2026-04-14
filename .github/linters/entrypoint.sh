#!/usr/bin/env bash
set -euo pipefail

CSPELL_CONFIG="${CSPELL_CONFIG:-.github/linters/.cspell.json}"
COMMITLINT_CONFIG="${COMMITLINT_CONFIG:-.github/linters/commitlint.config.js}"
COMMITLINT_FROM="${COMMITLINT_FROM:-origin/main}"

run_cspell() {
    cspell --config "${CSPELL_CONFIG}"
}

run_commitlint() {
    # Bind-mounted repo is owned by a different UID than the container user;
    # git refuses to operate on it unless the path is marked safe.
    git config --global --add safe.directory /app
    git fetch origin main --depth=1 >/dev/null 2>&1 \
        || echo "warning: git fetch failed; using cached origin/main if available" >&2
    commitlint --from="${COMMITLINT_FROM}" --config "${COMMITLINT_CONFIG}"
}

case "${1:-all}" in
    cspell)
        run_cspell
        ;;
    commitlint)
        run_commitlint
        ;;
    all|"")
        run_cspell
        run_commitlint
        ;;
    *)
        echo "Usage: $0 {cspell|commitlint|all}" >&2
        exit 2
        ;;
esac
