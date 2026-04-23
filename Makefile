# status-saver top-level developer/operator entrypoints.
#
# Common verbs:
#   make              same as `make build`
#   make build        build all three binaries into ./bin
#   make install      install all three into $GOBIN (usually ~/go/bin)
#   make test         run unit tests with the race detector
#   make cover        ...with a coverage summary per package
#   make lint         gofmt check + go vet + golangci-lint (if installed)
#   make vuln         govulncheck the module
#   make tidy         go mod tidy
#   make clean        remove ./bin and ./dist

# Our SQLite driver (mattn/go-sqlite3) needs CGO. All build-time targets
# pin this explicitly so CGO_ENABLED=0 environments don't silently skip it.
export CGO_ENABLED := 1

BIN_DIR := bin
DIST_DIR := dist

# Packages we own (excludes the user's local whatsmeow clone if any).
PKGS := ./cmd/... ./internal/...

.PHONY: all build install test cover lint vet fmt fmtcheck vuln tidy clean help

all: build

## build: compile all binaries into ./bin
build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/ ./cmd/...
	@echo "-> built: $$(ls -1 $(BIN_DIR) | tr '\n' ' ')"

## install: install binaries to $GOBIN (usually ~/go/bin)
install:
	go install ./cmd/...

## test: unit tests with race detector
test:
	go test -race $(PKGS)

## cover: test + coverage summary per package
cover:
	go test -race -cover $(PKGS)

## fmt: rewrite source files to gofmt canonical form
fmt:
	gofmt -w cmd/ internal/

## fmtcheck: fail if anything is not gofmt-clean
fmtcheck:
	@unformatted=$$(gofmt -l cmd/ internal/); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	fi

## vet: go vet on our packages
vet:
	go vet $(PKGS)

## lint: fmtcheck + vet + golangci-lint (skipped silently if not installed)
lint: fmtcheck vet
	@if command -v golangci-lint >/dev/null; then \
		golangci-lint run $(PKGS); \
	else \
		echo "golangci-lint not installed — skipping (install: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s)"; \
	fi

## vuln: govulncheck our module + transitive deps
vuln:
	@if ! command -v govulncheck >/dev/null; then \
		echo "installing govulncheck..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	fi
	govulncheck $(PKGS)

## tidy: go mod tidy
tidy:
	go mod tidy

## clean: remove build outputs
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

## help: list targets (parsed from ## comments)
help:
	@awk '/^## / { sub(/^## /, ""); print }' $(MAKEFILE_LIST)
