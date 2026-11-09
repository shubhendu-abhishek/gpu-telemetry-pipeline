SHELL := /bin/bash
.DEFAULT_GOAL := help

APP_NAME := gpu-telemetry-pipeline
COVERAGE_THRESHOLD ?= 90
COVERAGE_FILE ?= coverage.out
COVERAGE_PACKAGES := ./internal/api ./internal/config ./internal/telemetry ./internal/streamer ./internal/collector
COMPOSE_FILE := deployments/docker-compose.yaml
HELM_CHART := deployments/helm/gpu-telemetry

.PHONY: help doctor bootstrap fmt vet lint test test-unit test-postgres test-e2e coverage coverage-html openapi build docker-build compose-up compose-down helm-lint helm-template k8s-smoke deploy-local clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doctor: ## Check local development prerequisites
	@command -v go >/dev/null || (echo "Go is required" && exit 1)
	@command -v git >/dev/null || (echo "Git is required" && exit 1)
	@go version
	@git --version
	@command -v docker >/dev/null && docker --version || echo "Docker not installed (required for Compose/E2E)"
	@command -v helm >/dev/null && helm version --short || echo "Helm not installed (required for Helm targets)"

bootstrap: ## Download Go dependencies and prepare local environment
	go mod download
	@test -f .env || cp .env.example .env

fmt: ## Format all Go files
	gofmt -w $$(find cmd internal tests -name '*.go' -type f)

vet: ## Run go vet
	go vet ./...

lint: ## Run static analysis (golangci-lint when installed, otherwise go vet)
	@if command -v golangci-lint >/dev/null; then golangci-lint run ./...; else go vet ./...; fi

test: ## Run all unit and available integration tests
	go test -race -count=1 ./...

test-unit: ## Run unit tests for application logic
	go test -race -count=1 $(COVERAGE_PACKAGES)

test-postgres: ## Run PostgreSQL repository integration tests (requires TEST_DATABASE_URL)
	@test -n "$(TEST_DATABASE_URL)" || (echo "Set TEST_DATABASE_URL" && exit 1)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -count=1 ./internal/postgres

test-e2e: ## Start Docker Compose stack and run black-box end-to-end tests
	@command -v docker >/dev/null || (echo "Docker is required" && exit 1)
	@set -e; trap 'docker compose -f $(COMPOSE_FILE) down' EXIT; \
	  STREAMER_INTERVAL=50ms docker compose -f $(COMPOSE_FILE) up -d --build --scale streamer=2 --scale collector=2; \
	  RUN_E2E=1 API_BASE_URL=http://localhost:8080 go test -count=1 -v ./tests/e2e

coverage: ## Enforce minimum unit coverage for core application packages
	go test -race -count=1 -covermode=atomic -coverprofile=$(COVERAGE_FILE) $(COVERAGE_PACKAGES)
	@go tool cover -func=$(COVERAGE_FILE) | tee /tmp/$(APP_NAME)-coverage.txt
	@awk -v min=$(COVERAGE_THRESHOLD) '/^total:/ {gsub("%", "", $$3); if (($$3 + 0) < min) {printf "Coverage %.1f%% is below required %.1f%%\n", $$3, min; exit 1} else {printf "Coverage gate passed: %.1f%% (minimum %.1f%%)\n", $$3, min}}' /tmp/$(APP_NAME)-coverage.txt

coverage-html: coverage ## Generate an HTML coverage report
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html

openapi: ## Generate OpenAPI JSON from the Go API contract
	go run ./cmd/openapi-gen -output api/openapi.json

build: ## Build API, Streamer and Collector binaries
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -o bin/streamer ./cmd/streamer
	CGO_ENABLED=0 go build -trimpath -o bin/collector ./cmd/collector

docker-build: ## Build all application images
	docker build -f deployments/docker/Dockerfile --build-arg APP=api -t $(APP_NAME):api .
	docker build -f deployments/docker/Dockerfile --build-arg APP=streamer -t $(APP_NAME):streamer .
	docker build -f deployments/docker/Dockerfile --build-arg APP=collector -t $(APP_NAME):collector .

compose-up: ## Start local stack (PostgreSQL, API, Streamers and Collectors)
	docker compose -f $(COMPOSE_FILE) up -d --build

compose-down: ## Stop local stack (preserve database volume)
	docker compose -f $(COMPOSE_FILE) down

helm-lint: ## Lint Helm chart
	helm lint $(HELM_CHART)

helm-template: ## Render Helm templates locally
	helm template gpu-telemetry $(HELM_CHART)

k8s-smoke: ## Deploy the chart to a disposable Kind cluster and verify readiness/API/HPA
	./scripts/k8s-smoke.sh

deploy-local: ## Install/upgrade chart into the current Kubernetes context
	helm upgrade --install gpu-telemetry $(HELM_CHART) --namespace gpu-telemetry --create-namespace

clean: ## Remove generated binaries and coverage files
	rm -rf bin dist $(COVERAGE_FILE) coverage.html
