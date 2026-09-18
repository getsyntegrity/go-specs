# go-specs Makefile
# Single Go module at the repository root; run targets from repo root.

BENCH_RESULTS := benchmarks/results
BENCHSTAT := $(shell go env GOPATH)/bin/benchstat
ifeq ($(wildcard $(BENCHSTAT)),)
BENCHSTAT := benchstat
endif

.PHONY: help test test-race coverage bench bench-smoke bench-report bench-compare fmt fmt-check lint build tidy clean check-go-version

# Default target: show all tasks with short descriptions
help:
	@echo "go-specs Makefile targets (run from repo root):"
	@echo ""
	@echo "  make test          Run all tests"
	@echo "  make test-race     Run tests with race detector"
	@echo "  make coverage      Run tests with coverage report (coverage.out)"
	@echo "  make bench         Quick benchmark run (terminal output)"
	@echo "  make bench-smoke   Run every benchmark once (path coverage, no timings) -- what CI runs"
	@echo "  make bench-report  Benchmarks with 10 iterations → benchmarks/results/current.txt"
	@echo "  make bench-compare Compare previous.txt vs current.txt (benchstat)"
	@echo "  make fmt           Rewrite tracked Go files with gofmt"
	@echo "  make fmt-check     Fail when any tracked Go file is not gofmt-clean"
	@echo "  make lint          Lint with golangci-lint (go vet only when it is not installed)"
	@echo "  make build         Build all packages in the module"
	@echo "  make tidy          go mod tidy"
	@echo "  make clean         Remove coverage.* and benchmark results"
	@echo "  make check-go-version  Verify every go.mod matches .go-version exactly"
	@echo ""

# Fail when any go.mod's `go` directive differs from .go-version, patch
# component included. Same script CI runs, so a bad bump is caught before
# pushing.
check-go-version:
	./.github/scripts/check-go-version.sh

# Run tests
test:
	go test ./...

# Run tests with race detector
test-race:
	go test -race ./...

# Run tests with coverage; report to stdout and write coverage.out
coverage:
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

# Run benchmarks (quick run, output to terminal)
bench:
	go test ./benchmarks -run='^$$' -bench=. -benchmem

# Execute every benchmark body exactly once, module-wide.
#
# This is coverage, not measurement. `go test ./...` never runs a Benchmark
# function, so without this target benchmark-only code paths compile and are
# never executed -- a panic in one ships green. -benchtime 1x runs each
# benchmark for a single iteration: enough to execute the path, useless as a
# timing, which is deliberate so that neither this target nor the CI step that
# calls it ever fails on a slow or noisy machine.
#
# Real numbers come from `make bench-report` (or benchmarks.yml on main).
# Allocation *guarantees* are gated by specs/allocation_contract_test.go under
# `make test`, not here. See BENCHMARKS.md for contractual vs observational.
bench-smoke:
	go test -run='^$$' -bench=. -benchtime=1x -benchmem ./...

# Run benchmarks with multiple iterations and write report to benchmarks/results/current.txt
bench-report:
	@mkdir -p $(BENCH_RESULTS)
	go test ./benchmarks -run='^$$' -bench=. -benchmem -count=10 2>&1 | tee $(BENCH_RESULTS)/current.txt
	@echo "Report written to $(BENCH_RESULTS)/current.txt"

# Compare previous vs current benchmark report (requires: go install golang.org/x/perf/cmd/benchstat@latest)
bench-compare:
	@(test -x $(BENCHSTAT) 2>/dev/null || command -v benchstat >/dev/null 2>&1) || (echo "Install benchstat: go install golang.org/x/perf/cmd/benchstat@latest" && exit 1)
	@test -f $(BENCH_RESULTS)/previous.txt || (echo "No $(BENCH_RESULTS)/previous.txt; run 'make bench-report' then: cp $(BENCH_RESULTS)/current.txt $(BENCH_RESULTS)/previous.txt" && exit 1)
	@test -f $(BENCH_RESULTS)/current.txt || (echo "Run 'make bench-report' first" && exit 1)
	$(BENCHSTAT) $(BENCH_RESULTS)/previous.txt $(BENCH_RESULTS)/current.txt

# Format every tracked Go file.
#
# The file list comes from `git ls-files`, not from `gofmt -l .`: gofmt walks
# into dotted directories, and a developer checkout can hold nested clones
# under .claude/worktrees/ whose formatting is not this tree's business. The
# tracked set is exactly what a fresh CI checkout contains, so local and CI
# agree while a developer's scratch worktrees stay out of it.
fmt:
	@files="$$(git ls-files '*.go')"; \
	if [ -n "$$files" ]; then gofmt -w $$files; fi

# Fail (non-zero) when any tracked Go file is not gofmt-clean, and name them.
fmt-check:
	@files="$$(git ls-files '*.go')"; \
	if [ -z "$$files" ]; then exit 0; fi; \
	drift="$$(gofmt -l $$files)"; \
	if [ -n "$$drift" ]; then \
		echo "gofmt drift in:"; \
		echo "$$drift" | sed 's/^/  /'; \
		echo ""; \
		echo "Run 'make fmt' to fix."; \
		exit 1; \
	fi

# Lint the whole module.
#
# This is a real if/else, not `which golangci-lint && golangci-lint run || go
# vet`: in that shell pattern a golangci-lint run that *found lint errors*
# falls through to `go vet`, and a passing vet makes the whole target exit 0 --
# silently hiding the failure. Here go vet runs only when golangci-lint is
# genuinely absent, so an installed linter's non-zero exit is the target's
# non-zero exit.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint run ./..."; \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; falling back to: go vet ./..."; \
		go vet ./...; \
	fi

# Build all packages
build:
	go build ./...

# Tidy the module
tidy:
	go mod tidy

clean:
	rm -f coverage.out coverage.html
	rm -f $(BENCH_RESULTS)/*.txt $(BENCH_RESULTS)/*.png
