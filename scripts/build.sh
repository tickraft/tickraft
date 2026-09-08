#!/usr/bin/env bash
# Local release-style build: frontend + versioned binary into bin/.
# Mirrors the Makefile web-build + build targets (pnpm, internal/web/dist,
# internal/cli ldflags).
set -euo pipefail
echo "Building tickraft..."
cd "$(dirname "$0")/.."
VERSION="${VERSION:-dev}"
GIT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X github.com/tickraft/tickraft/internal/cli.version=${VERSION} -X github.com/tickraft/tickraft/internal/cli.gitCommit=${GIT_COMMIT} -X github.com/tickraft/tickraft/internal/cli.buildTime=${BUILD_TIME}"
# Build frontend (pnpm workspace; output lands in web/app/dist)
cd web && pnpm install --frozen-lockfile && pnpm build && cd ..
# Stage frontend dist for the internal/web go:embed directive
rm -rf internal/web/dist
mkdir -p internal/web/dist && cp -r web/app/dist/. internal/web/dist/
# Build binary
go build -ldflags "${LDFLAGS}" -o bin/tickraft ./cmd/tickraft
echo "Build complete: bin/tickraft"
