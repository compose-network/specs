# Makefile for Compose Protocol
# Run `make help` to see available commands

# ============================================================================
# Variables
# ============================================================================
COMPOSE_DIR := compose

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
BLUE := \033[0;34m
NC := \033[0m # No Color

.PHONY: all help build test lint fmt clean check verify

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
# Building
# ============================================================================
build: ## Build the workspace
	@echo "$(BLUE)Building...$(NC)"
	@cd $(COMPOSE_DIR) && cargo build --workspace
	@echo "$(GREEN)Build successful!$(NC)"

clean: ## Clean build artifacts
	@echo "$(BLUE)Cleaning...$(NC)"
	@cd $(COMPOSE_DIR) && cargo clean
	@echo "$(GREEN)Cleaned!$(NC)"

# ============================================================================
# Testing
# ============================================================================
test: ## Run all tests
	@echo "$(BLUE)Running tests...$(NC)"
	@cd $(COMPOSE_DIR) && cargo test --workspace
	@echo "$(GREEN)Tests passed!$(NC)"

# ============================================================================
# Linting & Formatting
# ============================================================================
lint: ## Run clippy
	@echo "$(BLUE)Running clippy...$(NC)"
	@cd $(COMPOSE_DIR) && cargo clippy --workspace --all-targets -- -D warnings
	@echo "$(GREEN)Clippy passed!$(NC)"

fmt: ## Check formatting
	@echo "$(BLUE)Checking formatting...$(NC)"
	@cd $(COMPOSE_DIR) && cargo fmt --all -- --check
	@echo "$(GREEN)Formatting OK!$(NC)"

fmt-fix: ## Fix formatting
	@echo "$(BLUE)Fixing formatting...$(NC)"
	@cd $(COMPOSE_DIR) && cargo fmt --all
	@echo "$(GREEN)Formatted!$(NC)"

# ============================================================================
# Verification (CI)
# ============================================================================
check: lint test ## Run linting and tests (quick check)
	@echo "$(GREEN)All checks passed!$(NC)"

verify: fmt lint test build ## Full verification (format, lint, test, build)
	@echo "$(GREEN)Full verification passed!$(NC)"
