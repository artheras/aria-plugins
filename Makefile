.PHONY: build test run catalog-check catalog-sync
build:
	mkdir -p bin
	go build -trimpath -o bin/aria-commerce ./cmd/server
	go build -trimpath -o bin/aria-commerce-mcp ./cmd/commerce-mcp
	go build -trimpath -o bin/aria-commerce-worker ./cmd/commerce-worker
test:
	go test -race ./...
	go vet ./...
	go run ./cmd/catalog check
run:
	go run ./cmd/server
catalog-check:
	go run ./cmd/catalog check
catalog-sync:
	go run ./cmd/catalog sync
