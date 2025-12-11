# Makefile for Compose Protocol
# Run `make help` to see available commands

# ============================================================================
# Variables
# ============================================================================
GO := go
GOLANGCI_LINT := golangci-lint
GOPATH := $(shell go env GOPATH)
GOBIN := $(GOPATH)/bin
MODULE := github.com/compose-network/specs
COMPOSE_DIR := compose

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
BLUE := \033[0;34m
NC := \033[0m # No Color

.PHONY: all help install-tools lint lint-fix fmt test test-verbose test-coverage \
        build clean proto check verify tidy vet sec

# ============================================================================
# Default target
# ============================================================================
all: verify

# ============================================================================
# Help
# ============================================================================
help: ## Display this help message
	@echo "$(BLUE)Compose Protocol - Development Commands$(NC)"
	@echo ""
	@echo "$(YELLOW)Usage:$(NC)"
	@echo "  make [target]"
	@echo ""
	@echo "$(YELLOW)Targets:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

# ============================================================================
# Installation
# ============================================================================
install-tools: ## Install required development tools
	@echo "$(BLUE)Installing development tools...$(NC)"
	@echo "Installing golangci-lint..."
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOBIN) latest
	@echo "Installing goimports..."
	@$(GO) install golang.org/x/tools/cmd/goimports@latest
	@echo "Installing gofumpt..."
	@$(GO) install mvdan.cc/gofumpt@latest
	@echo "Installing gosec..."
	@$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "Installing protoc-gen-go..."
	@$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@echo "$(GREEN)All tools installed successfully!$(NC)"


# ============================================================================
# Linting & Formatting
# ============================================================================
lint: ## Run golangci-lint
	@echo "$(BLUE)Running golangci-lint...$(NC)"
	@cd $(COMPOSE_DIR) && $(GOLANGCI_LINT) run --config ../.golangci.yml ./...

lint-fix: ## Run golangci-lint with auto-fix
	@echo "$(BLUE)Running golangci-lint with auto-fix...$(NC)"
	@cd $(COMPOSE_DIR) && $(GOLANGCI_LINT) run --config ../.golangci.yml --fix ./...

fmt: ## Format Go code with gofumpt and goimports
	@echo "$(BLUE)Formatting Go code...$(NC)"
	@find $(COMPOSE_DIR) -name '*.go' -not -path '*/.git/*' -not -name '*.pb.go' -exec gofumpt -w {} \;
	@find $(COMPOSE_DIR) -name '*.go' -not -path '*/.git/*' -not -name '*.pb.go' -exec goimports -w -local $(MODULE) {} \;
	@echo "$(GREEN)Code formatted!$(NC)"

vet: ## Run go vet
	@echo "$(BLUE)Running go vet...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) vet ./...

sec: ## Run security scanner (gosec)
	@echo "$(BLUE)Running security scanner...$(NC)"
	@cd $(COMPOSE_DIR) && gosec -exclude-generated ./...

# ============================================================================
# Testing
# ============================================================================
test: ## Run all tests
	@echo "$(BLUE)Running tests...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) test ./...

test-verbose: ## Run all tests with verbose output
	@echo "$(BLUE)Running tests (verbose)...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) test -v ./...

test-coverage: ## Run tests with coverage report
	@echo "$(BLUE)Running tests with coverage...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) test -coverprofile=coverage.out ./...
	@cd $(COMPOSE_DIR) && $(GO) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report: $(COMPOSE_DIR)/coverage.html$(NC)"

test-race: ## Run tests with race detector
	@echo "$(BLUE)Running tests with race detector...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) test -race ./...

# ============================================================================
# Building
# ============================================================================
build: ## Build the project
	@echo "$(BLUE)Building...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) build ./...
	@echo "$(GREEN)Build successful!$(NC)"

clean: ## Clean build artifacts
	@echo "$(BLUE)Cleaning...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) clean ./...
	@rm -f $(COMPOSE_DIR)/coverage.out $(COMPOSE_DIR)/coverage.html
	@echo "$(GREEN)Cleaned!$(NC)"

# ============================================================================
# Dependencies
# ============================================================================
tidy: ## Run go mod tidy
	@echo "$(BLUE)Running go mod tidy...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) mod tidy

deps: ## Download dependencies
	@echo "$(BLUE)Downloading dependencies...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) mod download

deps-update: ## Update dependencies
	@echo "$(BLUE)Updating dependencies...$(NC)"
	@cd $(COMPOSE_DIR) && $(GO) get -u ./...
	@cd $(COMPOSE_DIR) && $(GO) mod tidy

# ============================================================================
# Protobuf
# ============================================================================
proto: ## Generate protobuf files
	@echo "$(BLUE)Generating protobuf files...$(NC)"
	@cd $(COMPOSE_DIR) && protoc \
		--proto_path=proto \
		--go_out=proto \
		--go_opt=paths=source_relative \
		proto/protocol_messages.proto
	@echo "$(GREEN)Protobuf files generated!$(NC)"

# ============================================================================
# Verification (CI)
# ============================================================================
check: lint test ## Run linting and tests (quick check)
	@echo "$(GREEN)All checks passed!$(NC)"

verify: tidy fmt lint test build ## Full verification (format, lint, test, build)
	@echo "$(GREEN)Full verification passed!$(NC)"

ci: ## Run CI pipeline locally
	@echo "$(BLUE)Running CI pipeline...$(NC)"
	@$(MAKE) tidy
	@$(MAKE) fmt
	@$(MAKE) lint
	@$(MAKE) test-race
	@$(MAKE) build
	@echo "$(GREEN)CI pipeline passed!$(NC)"
