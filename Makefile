BINARY_NAME := sherlock
BUILD_DIR := bin
GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build test lint run dev docker-build migrate clean fmt vet coverage release-dry-run

all: lint test build

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/sherlock

test:
	$(GO) test -race -count=1 ./...

lint: vet
	@which golangci-lint > /dev/null 2>&1 || echo "golangci-lint not installed, skipping"
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || true

vet:
	$(GO) vet ./...

fmt:
	gofmt -s -w .

run: build
	./$(BUILD_DIR)/$(BINARY_NAME) serve

dev:
	docker compose -f deploy/docker-compose.yml up --build

dev-down:
	docker compose -f deploy/docker-compose.yml down -v

docker-build:
	docker build -t sherlock:latest .

migrate:
	./$(BUILD_DIR)/$(BINARY_NAME) migrate

clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean -cache -testcache

coverage:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

release-dry-run:
	goreleaser release --snapshot --clean
