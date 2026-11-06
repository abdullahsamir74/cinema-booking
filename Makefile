# syntax=docker/dockerfile:1

APP        := cinema
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X main.version=$(VERSION)
REDIS_ADDR ?= localhost:6380
ADMIN_TOKEN ?= admin-demo-token

.PHONY: help run build test race cover vet fmt lint integration docker-up docker-down docker-logs clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

run: ## Run locally with the in-memory store
	BOOKING_STORE=memory ADMIN_TOKEN=$(ADMIN_TOKEN) go run ./cmd/server

build: ## Build a static binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/server

test: ## Run unit and HTTP tests
	go test ./...

race: ## Run tests with the race detector
	go test -race ./...

cover: ## Run tests and print total coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go source
	gofmt -w ./cmd ./internal

lint: vet ## Run vet and golangci-lint when installed
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not installed; skipped"; fi

integration: ## Run Redis integration tests (requires Redis)
	REDIS_ADDR=$(REDIS_ADDR) go test -tags=integration -count=1 ./internal/adapters/redis

docker-up: ## Build and start app + Redis
	docker compose up --build -d

docker-down: ## Stop containers
	docker compose down

docker-logs: ## Follow application logs
	docker compose logs -f app

clean: ## Remove build artifacts
	rm -rf bin coverage.out
