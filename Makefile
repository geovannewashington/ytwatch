.PHONY: build install test fmt vet clean

# The version comes from the latest git tag, such as 0.1.0 or 0.1.0-3-gabc123.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')

build:
	go build -ldflags "-X main.version=$(VERSION)" -o ytwatch ./cmd/ytwatch

install: build
	install -Dm755 ytwatch $(HOME)/.local/bin/ytwatch

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

clean:
	rm -f ytwatch
