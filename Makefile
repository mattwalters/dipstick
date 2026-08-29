.PHONY: all build install test lint fmt tidy matrix capture clean help

all: build test lint

build:
	go build ./...

install:
	go install ./cmd/dipstick

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w -s .
	goimports -w -local github.com/mattwalters/dipstick .

tidy:
	go mod tidy

matrix:
	go run ./cmd/genmatrix

capture:
	go run ./internal/tools/capture

clean:
	go clean
	rm -f dipstick
	rm -rf dist/

help:
	@echo "Available targets:"
	@echo "  all      - Build, test, and lint"
	@echo "  build    - Build all packages and binaries"
	@echo "  install  - Install dipstick binary to GOBIN"
	@echo "  test     - Run all unit and integration tests with race detector"
	@echo "  lint     - Run golangci-lint"
	@echo "  fmt      - Format Go files and optimize imports"
	@echo "  tidy     - Prune and verify go.mod / go.sum"
	@echo "  matrix   - Synchronize support matrix in README.md"
	@echo "  capture  - Capture local vendor test fixtures"
	@echo "  clean    - Remove build artifacts and dist/ directory"
	@echo "  help     - Show this help message"
