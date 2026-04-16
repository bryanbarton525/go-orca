SHELL := /bin/sh

GO ?= go
BINARY ?= go-orca-api
CMD_DIR ?= ./cmd/go-orca-api
CONFIG ?= orca.yml
PKGS ?= ./...
ENV_FILE ?= .env

.DEFAULT_GOAL := help

.PHONY: help build run test test-race fmt vet tidy check clean

help:
	@printf "%s\n" \
		"Targets:" \
		"  build      Build the API binary" \
		"  run        Build, source $(ENV_FILE) if present, start the API, and remove the local binary on exit" \
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

test:
	$(GO) test $(PKGS)

test-race:
	$(GO) test -race $(PKGS)

fmt:
	$(GO) fmt $(PKGS)

vet:
	$(GO) vet $(PKGS)

tidy:
	$(GO) mod tidy

check: vet test

clean:
	rm -f $(BINARY)