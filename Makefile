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

# Docker parameters
DOCKER_IMAGE=$(BINARY_NAME)
DOCKER_CONTAINER=$(BINARY_NAME)

.PHONY: all build clean test run deps help docker-build docker-run docker-stop docker-rm docker-logs

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
	docker build -t $(DOCKER_IMAGE) .

docker-run: ## Run docker container
	docker run -d \
		--name $(DOCKER_CONTAINER) \
		-p 8086:8086 \
		-p 8089:8089/udp \
		$(DOCKER_IMAGE)

docker-stop: ## Stop docker container
	docker stop $(DOCKER_CONTAINER)

docker-rm: ## Remove docker container
	docker rm $(DOCKER_CONTAINER)

docker-logs: ## Show docker container logs
	docker logs $(DOCKER_CONTAINER)

docker-clean: docker-stop docker-rm ## Stop and remove docker container

docker-test: docker-build docker-run ## Build and run docker container
	@echo "Waiting for container to start..."
	@sleep 5
	@echo "Testing HTTP endpoint..."
	@curl -s http://localhost:8086/health
	@echo "Testing UDP endpoint..."
	@echo "cpu,host=test value=42.5" | nc -u localhost 8089
	@echo "Testing query endpoint..."
	@curl -s -G "http://localhost:8086/query" --data-urlencode "db=mydb" --data-urlencode "q=SELECT * FROM cpu"
	@make docker-clean

# CI targets
ci: deps fmt vet lint test ## Run all CI tasks

# Install development tools
tools: ## Install development tools
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.DEFAULT_GOAL := help 