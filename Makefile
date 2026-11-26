# grab the latest git tag, remove the 'v' prefix if present, and handle dirty/dev commits
VERSION ?= $(or $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//'),dev)
BUILT_BY ?= $(shell whoami)

.DEFAULT_GOAL := help

## help: print this help message
.PHONY: help
help:
	@echo 'usage: make <target>'
	@echo
	@echo '  where <target> is one of the following:'
	@echo
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

.PHONY: needs-golangci-lint
needs-golangci-lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo >&2 "golangci-lint is required but it's not installed. Aborting."; exit 1; }

.PHONY: needs-govulncheck
needs-govulncheck:
	@command -v govulncheck >/dev/null 2>&1 || { echo >&2 "govulncheck is required but it's not installed. Aborting."; exit 1; }

.PHONY: needs-rumdl
needs-rumdl:
	@command -v rumdl >/dev/null 2>&1 || { echo >&2 "rumdl is required but it's not installed. Aborting."; exit 1; }

## build: build the gum MCP server
.PHONY: build
build:
	@echo "Building gum version $(VERSION)"
	@go build -ldflags "-X main.version=$(VERSION) -X 'main.builtBy=$(BUILT_BY)'"

## package: build snapshot .deb packages and archives into dist/ with GoReleaser (in Docker)
GORELEASER_IMAGE ?= goreleaser/goreleaser:v2.20.1
.PHONY: package
package:
	docker run --rm --user $$(id -u):$$(id -g) -e HOME=/tmp -e GOTOOLCHAIN=auto \
		-v "$(CURDIR):/src" -w /src $(GORELEASER_IMAGE) release --snapshot --clean

## test: run tests
.PHONY: test
test:
	@echo "Running tests"
	@go test ./...

## check-security: run golangci-lint (including gosec) and govulncheck to check for security vulnerabilities
.PHONY: check-security
check-security: needs-golangci-lint needs-govulncheck
	golangci-lint run ./...
	govulncheck ./...

## lint-md: lint Markdown files with rumdl
.PHONY: lint-md
lint-md: needs-rumdl
	rumdl check .
