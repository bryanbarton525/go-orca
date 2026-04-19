SHELL := /bin/sh

GO ?= go
BINARY ?= go-orca-api
CMD_DIR ?= ./cmd/go-orca-api
CONFIG ?= orca.yml
PKGS ?= ./...
ENV_FILE ?= .env
UI_DIR ?= ./ui

.DEFAULT_GOAL := help

.PHONY: help build run dev ui-install ui-dev ui-build test test-race fmt vet tidy check clean

help:
	@printf "%s\n" \
		"Targets:" \
		"  build      Build the API binary" \
		"  run        Build, source $(ENV_FILE) if present, start the API, and remove the local binary on exit" \
		"  dev        Start API and UI dev servers concurrently" \
		"  ui-install Install UI dependencies" \
		"  ui-dev     Start the UI dev server" \
		"  ui-build   Build the UI for production" \
		"  test       Run the test suite" \
		"  test-race  Run the test suite with the race detector" \
		"  fmt        Format Go packages" \
		"  vet        Run go vet" \
		"  tidy       Run go mod tidy" \
		"  check      Run vet and tests" \
		"  clean      Remove the built binary"

build:
	$(GO) build -o $(BINARY) $(CMD_DIR)

run: build
	@set -a; \
	if [ -f $(ENV_FILE) ]; then . ./$(ENV_FILE); fi; \
	set +a; \
	trap 'rm -f ./$(BINARY)' EXIT INT TERM; \
	./$(BINARY) -config $(CONFIG)

dev: build
	@set -a; \
	if [ -f $(ENV_FILE) ]; then . ./$(ENV_FILE); fi; \
	set +a; \
	trap 'rm -f ./$(BINARY); kill 0' EXIT INT TERM; \
	./$(BINARY) -config $(CONFIG) & \
	cd $(UI_DIR) && corepack pnpm dev

ui-install:
	cd $(UI_DIR) && corepack pnpm install

ui-dev:
	cd $(UI_DIR) && corepack pnpm dev

ui-build:
	cd $(UI_DIR) && corepack pnpm build

vet:
	$(GO) vet $(PKGS)

tidy:
	$(GO) mod tidy

check: vet test

clean:
	rm -f $(BINARY)