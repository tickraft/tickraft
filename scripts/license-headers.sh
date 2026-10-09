#!/usr/bin/env bash
#
# license-headers.sh — Check or add standardized license headers to source files.
#
# Usage:
#   ./scripts/license-headers.sh check   # Report files missing the header (exit 1 if any)
#   ./scripts/license-headers.sh fix     # Add the header to files that are missing it
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

MODE="${1:-check}"
if [[ "$MODE" != "check" && "$MODE" != "fix" ]]; then
    echo "Usage: $0 [check|fix]"
    exit 2
fi

# ── Header content (without comment markers) ──────────────────────────────
COPYRIGHT_LINE="Copyright © 2026 Beijing Zhichuan Technology Co., Ltd."
SPDX_LINE="SPDX-License-Identifier: AGPL-3.0-or-later"
DUAL_LINE="Dual-licensed — see LICENSE for details."

# Marker string used to detect an existing header (searched in first 5 lines)
COPYRIGHT_MARKER="Beijing Zhichuan Technology Co., Ltd."

# ── Comment prefix by file extension ───────────────────────────────────────
get_comment_prefix() {
    case "$1" in
        go|ts|js|vue|scss) printf '// ' ;;
        yaml|yml|toml)     printf '# '  ;;
        *)                 return 1    ;;
    esac
}

# ── Generate the header block for a given file ─────────────────────────────
generate_header() {
    local ext="${1##*.}"
    if [[ "$ext" == "html" ]]; then
        printf '<!--\n  %s\n  %s\n  %s\n-->\n' \
            "$COPYRIGHT_LINE" "$SPDX_LINE" "$DUAL_LINE"
    else
        local prefix
        prefix="$(get_comment_prefix "$ext")"
        printf '%s%s\n%s%s\n%s%s\n' \
            "$prefix" "$COPYRIGHT_LINE" \
            "$prefix" "$SPDX_LINE" \
            "$prefix" "$DUAL_LINE"
    fi
}

# ── Check whether a file already has the header ────────────────────────────
has_header() {
    head -5 "$1" 2>/dev/null | grep -qF "$COPYRIGHT_MARKER"
}

# ── Prepend the header + blank line to a file ──────────────────────────────
add_header() {
    local file="$1"
    local tmp
    tmp="$(mktemp)"
    generate_header "$file" > "$tmp"
    printf '\n' >> "$tmp"
    cat "$file" >> "$tmp"
    mv "$tmp" "$file"
}

# ── Find all eligible source files ─────────────────────────────────────────
find_eligible_files() {
    find "$REPO_ROOT" -type f \
        \( \
            -name '*.go'   -o \
            -name '*.ts'   -o \
            -name '*.js'   -o \
            -name '*.vue'  -o \
            -name '*.scss' -o \
            -name '*.yaml' -o \
            -name '*.yml'  -o \
            -name '*.toml' -o \
            -name '*.html' \
        \) \
        -not -path '*/.git/*' \
        -not -path '*/node_modules/*' \
        -not -path '*/dist/*' \
        -not -path '*/.trae/*' \
        -not -path '*/.vscode/*' \
        -not -name 'go.mod' \
        -not -name 'go.sum' \
        -not -name 'openapi.yaml' \
        -not -name 'pnpm-lock.yaml' \
        -not -name 'components.d.ts' \
        -not -path '*/docs/api/*' \
        | sort
}

# ── Main loop ───────────────────────────────────────────────────────────────
total=0
missing=0
fixed=0

while IFS= read -r file; do
    total=$((total + 1))
    if ! has_header "$file"; then
        missing=$((missing + 1))
        if [[ "$MODE" == "fix" ]]; then
            add_header "$file"
            fixed=$((fixed + 1))
            echo "  FIXED  ${file#"$REPO_ROOT"/}"
        else
            echo "  MISSING  ${file#"$REPO_ROOT"/}"
        fi
    fi
done < <(find_eligible_files)

# ── Summary ─────────────────────────────────────────────────────────────────
echo "---"
echo "Total eligible files: $total"
if [[ "$MODE" == "fix" ]]; then
    echo "Headers added:       $fixed"
    echo "Already had header:  $((total - fixed))"
else
    echo "Files with header:   $((total - missing))"
    echo "Files missing:       $missing"
    [[ $missing -eq 0 ]] || exit 1
fi
