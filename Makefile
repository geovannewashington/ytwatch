.PHONY: build install test fmt vet clean

build:
	go build -o ytwatch ./cmd/ytwatch

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
