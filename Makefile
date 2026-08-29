.PHONY: all build install test lint clean

all: build

build:
	go build ./cmd/dipstick

install:
	go install ./cmd/dipstick

test:
	go test -race ./...

lint:
	golangci-lint run

clean:
	rm -f dipstick
	rm -rf dist/
