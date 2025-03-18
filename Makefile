# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
BINARY_NAME=refluxdb
BINARY_UNIX=$(BINARY_NAME)
BINARY_WIN=$(BINARY_NAME).exe

# Build directory
BUILD_DIR=build

# Main package location
MAIN_PACKAGE=./cmd/refluxdb

# Test directories
TEST_DIRS=./internal/... ./tests/...

.PHONY: all build clean test run deps help

all: clean deps build test ## Build and run tests

help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

deps: ## Download dependencies
	$(GOCMD) mod download
	$(GOCMD) mod verify

build: ## Build the binary
	mkdir -p $(BUILD_DIR)
ifeq ($(OS),Windows_NT)
	$(GOBUILD) -v -o $(BUILD_DIR)/$(BINARY_WIN) $(MAIN_PACKAGE)
else
	$(GOBUILD) -v -o $(BUILD_DIR)/$(BINARY_UNIX) $(MAIN_PACKAGE)
endif

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
ifeq ($(OS),Windows_NT)
	if exist "$(BUILD_DIR)" rmdir /s /q "$(BUILD_DIR)"
else
	rm -rf $(BUILD_DIR)
endif

test: ## Run tests
ifeq ($(OS),Windows_NT)
	$(GOTEST) -v $(TEST_DIRS)
else
	$(GOTEST) -v $(TEST_DIRS)
endif

test-verbose: ## Run tests with verbose output
	$(GOTEST) -v $(TEST_DIRS)

test-coverage: ## Run tests with coverage
	mkdir -p $(BUILD_DIR)
	$(GOTEST) -coverprofile=$(BUILD_DIR)/coverage.out $(TEST_DIRS)
	$(GOCMD) tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html

run: build ## Run the application
ifeq ($(OS),Windows_NT)
	$(BUILD_DIR)/$(BINARY_WIN)
else
	$(BUILD_DIR)/$(BINARY_UNIX)
endif

# Development targets
fmt: ## Format code
	$(GOCMD) fmt ./...

vet: ## Run go vet
	$(GOCMD) vet ./...

lint: ## Run linter
	golangci-lint run

# Docker targets
docker-build: ## Build docker image
	docker build -t $(BINARY_NAME) .

docker-run: ## Run docker container
	docker run -p 8086:8086 -p 8089:8089/udp $(BINARY_NAME)

# CI targets
ci: deps fmt vet lint test ## Run all CI tasks

# Install development tools
tools: ## Install development tools
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.DEFAULT_GOAL := help 