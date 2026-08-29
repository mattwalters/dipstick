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
	@echo "Capturing local vendor fixtures..."
	@go test ./internal/adapters/... -v -run 'TestCapture' || true
	@echo "Remember to verify and redact sensitive credentials in testdata/ before committing!"

clean:
	go clean
	rm -f dipstick
	rm -rf dist/
