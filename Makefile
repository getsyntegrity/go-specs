# go-specs Makefile
# Single Go module at the repository root; run targets from repo root.

BENCH_RESULTS := benchmarks/results
BENCHSTAT := $(shell go env GOPATH)/bin/benchstat
ifeq ($(wildcard $(BENCHSTAT)),)
BENCHSTAT := benchstat
endif

.PHONY: help test test-race coverage bench bench-report bench-compare lint build tidy clean

# Default target: show all tasks with short descriptions
help:
	@echo "go-specs Makefile targets (run from repo root):"
	@echo ""
	@echo "  make test          Run tests across all modules"
	@echo "  make test-race     Run tests with race detector"
	@echo "  make coverage      Run tests with coverage report (coverage.out)"
	@echo "  make bench         Quick benchmark run (terminal output)"
	@echo "  make bench-report  Benchmarks with 10 iterations → benchmarks/results/current.txt"
	@echo "  make bench-compare Compare previous.txt vs current.txt (benchstat)"
	@echo "  make lint          Lint (golangci-lint or go vet)"
	@echo "  make build         Build all modules and specs-cli"
	@echo "  make tidy          go mod tidy"
	@echo "  make clean         Remove specs-cli, coverage.*, benchmark results"
	@echo ""

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

# Lint (golangci-lint if available, else go vet)
lint:
	@which golangci-lint >/dev/null 2>&1 && golangci-lint run ./assert/... ./specs/... ./report/... ./mock/... ./gen/... ./snapshots/... ./benchmarks/... ./examples/... || (go vet ./assert/... ./specs/... ./report/... ./mock/... ./gen/... ./snapshots/... ./benchmarks/... ./examples/...)

# Build all packages
build:
	go build ./...

# Tidy the module
tidy:
	go mod tidy

clean:
	rm -f specs-cli coverage.out coverage.html
	rm -f $(BENCH_RESULTS)/*.txt $(BENCH_RESULTS)/*.png
