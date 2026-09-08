.PHONY: build test lint clean docker cross-build web-build license-header license-header-fix

BINARY=tickraft
CMD_DIR=cmd/tickraft
VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_DIR=bin
LDFLAGS=-X github.com/tickraft/tickraft/internal/cli.version=$(VERSION) -X github.com/tickraft/tickraft/internal/cli.gitCommit=$(GIT_COMMIT) -X github.com/tickraft/tickraft/internal/cli.buildTime=$(BUILD_TIME)

build: web-build
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./$(CMD_DIR)

# web-build compiles the front-end assets and stages them under
# internal/web/dist so the Go //go:embed directive picks them up at compile
# time. The front-end source tree (web/) is always present in this repository,
# so pnpm and node are hard requirements for `make build`.
web-build:
	@echo "=== Building frontend assets ==="
	@command -v node >/dev/null 2>&1 || { echo "ERROR: node not found in PATH; install Node.js (>= 22) before building the frontend"; exit 1; }
	@command -v pnpm >/dev/null 2>&1 || { echo "ERROR: pnpm not found in PATH; install pnpm (e.g. 'npm install -g pnpm') before building the frontend"; exit 1; }
	cd web && pnpm install --frozen-lockfile && pnpm build
	@test -d web/app/dist || { echo "ERROR: web/app/dist not produced by frontend build"; exit 1; }
	@echo "=== Copying frontend assets to internal/web/dist ==="
	@rm -rf internal/web/dist && mkdir -p internal/web/dist && cp -r web/app/dist/. internal/web/dist/ && touch internal/web/dist/.gitkeep
	@echo "=== Frontend build complete ==="

test:
	go test -v -race -cover ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR) web/app/dist web/packages/*/dist

docker:
	docker build -t tickraft-ce:$(VERSION) .

cross-build:
	@for os in linux darwin windows; do \
		for arch in amd64 arm64; do \
			echo "Building $$os/$$arch..."; \
			GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-$$os-$$arch ./$(CMD_DIR); \
		done; \
	done

# license-header verifies that every eligible source file carries the
# standardized 3-line AGPLv3 + Commercial dual-license header. Fails if any
# file is missing the header.
license-header:
	@./scripts/license-headers.sh check

# license-header-fix adds the standard header to any source file that is
# missing it. Run this after creating new files or when the check fails.
license-header-fix:
	@./scripts/license-headers.sh fix
