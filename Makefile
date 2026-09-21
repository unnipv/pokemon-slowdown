# Development tasks for pokemon slowdown.
#
# `make` on its own builds and runs the checks.

BINARY := slowdown
PKG    := ./cmd/slowdown

.PHONY: help build run test race vet fmt lint check live snapshot stats clean doctor

help:
	@echo "build     build ./$(BINARY)"
	@echo "run       build and open the lobby"
	@echo "test      run the test suite (no network)"
	@echo "race      run the test suite with the race detector"
	@echo "vet       go vet"
	@echo "fmt       gofmt -w"
	@echo "lint      staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)"
	@echo "check     fmt check, vet, lint and test"
	@echo "live      opt-in tests against the real server and sprite server"
	@echo "snapshot  build release archives locally with goreleaser"
	@echo "stats     print install and traffic counts (needs gh)"
	@echo "doctor    print terminal diagnostics"
	@echo "clean     remove build output"

build:
	go build -o $(BINARY) $(PKG)

run: build
	./$(BINARY)

test:
	go test ./... -timeout 300s

race:
	go test -race ./... -timeout 600s

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

lint:
	staticcheck ./...

# `check` is what CI enforces.
check:
	@unformatted="$$(gofmt -l ./cmd ./internal)"; \
	if [ -n "$$unformatted" ]; then echo "not gofmt'd:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	staticcheck ./...
	go test ./... -timeout 300s

live:
	SLOWDOWN_LIVE=1 go test ./internal/showdown/ -run TestLive -v -timeout 300s
	SLOWDOWN_LIVE=1 go test ./internal/sprites/ -run TestLive -v -timeout 300s
	SLOWDOWN_LIVE=1 go test ./internal/tui/ -run TestLive -v -timeout 300s

snapshot:
	goreleaser build --snapshot --clean

doctor: build
	./$(BINARY) doctor

stats:
	./scripts/stats.sh

clean:
	rm -f $(BINARY)
	rm -rf dist
