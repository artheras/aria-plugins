.PHONY: build test run
build:
	mkdir -p bin
	go build -trimpath -o bin/aria-commerce ./cmd/server
	go build -trimpath -o bin/aria-commerce-mcp ./cmd/commerce-mcp
test:
	go test -race ./...
	go vet ./...
run:
	go run ./cmd/server
