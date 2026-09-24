# =============================================================================
# Staff Display Platform — backend make targets
# =============================================================================
# Windows developers can use the equivalent `go` commands directly:
#   go run ./cmd/migrate up
#   go run ./cmd/server
# =============================================================================

.PHONY: help build build-server build-migrate test vet fmt tidy run server migrate-up migrate-status clean

MODULE         := github.com/m2sound2456/staffdisplay/backend
GO_BUILD       ?= go build
OUTPUT_DIR     ?= build
VERSION        ?= 0.1.0
GIT_COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME     ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

GO_LDFLAGS     ?= -s -w \
                  -X '$(MODULE)/internal/version.Version=$(VERSION)' \
                  -X '$(MODULE)/internal/version.GitCommit=$(GIT_COMMIT)' \
                  -X '$(MODULE)/internal/version.BuildTime=$(BUILD_TIME)'

SERVER_BIN     := staffdisplay-server
MIGRATE_BIN    := staffdisplay-migrate

## help — list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

## build — build server and migration binaries into $(OUTPUT_DIR)
build: build-server build-migrate

## build-server — build the API server binary
build-server:
	@mkdir -p $(OUTPUT_DIR)
	$(GO_BUILD) -trimpath -ldflags "$(GO_LDFLAGS)" -o $(OUTPUT_DIR)/$(SERVER_BIN) ./cmd/server

## build-migrate — build the migration CLI
build-migrate:
	@mkdir -p $(OUTPUT_DIR)
	$(GO_BUILD) -trimpath -ldflags "$(GO_LDFLAGS)" -o $(OUTPUT_DIR)/$(MIGRATE_BIN) ./cmd/migrate

## run — run the API server from source
run:
	go run ./cmd/server

## server — alias for run
server: run

## migrate-up — apply pending migrations
migrate-up:
	go run ./cmd/migrate up

## migrate-status — show applied and pending migrations
migrate-status:
	go run ./cmd/migrate status

## test — run the full test suite
test:
	go test ./...

## vet — run go vet
vet:
	go vet ./...

## fmt — format all Go sources
fmt:
	go fmt ./...

## tidy — sync go.mod / go.sum
tidy:
	go mod tidy

## clean — remove build artefacts
clean:
	rm -rf $(OUTPUT_DIR)
