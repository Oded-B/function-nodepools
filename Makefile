# Makefile for function-nodepools project

.PHONY: help test-e2e test-e2e-verbose test-e2e-timeout clean-e2e deps-e2e

# Default target
help:
	@echo "Available targets:"
	@echo "  deps-e2e        - Install e2e test dependencies"
	@echo "  test-e2e        - Run e2e tests"
	@echo "  test-e2e-verbose - Run e2e tests with verbose output"
	@echo "  test-e2e-timeout - Run e2e tests with extended timeout"
	@echo "  clean-e2e       - Clean up e2e test resources"
	@echo "  help            - Show this help message"

# Install e2e test dependencies
deps-e2e:
	@echo "Installing e2e test dependencies..."
	go mod tidy
	go mod download

# Run e2e tests
test-e2e: deps-e2e
	@echo "Running e2e tests..."
	go test ./e2e -v

# Run e2e tests with verbose output
test-e2e-verbose: deps-e2e
	@echo "Running e2e tests with verbose output..."
	go test ./e2e -v -args -test.v

# Run e2e tests with extended timeout
test-e2e-timeout: deps-e2e
	@echo "Running e2e tests with extended timeout..."
	go test ./e2e -v -timeout 30m

# Clean up e2e test resources
clean-e2e:
	@echo "Cleaning up e2e test resources..."
	kind delete clusters --all 2>/dev/null || true
	kubectl config delete-context kind-* 2>/dev/null || true

# Run specific e2e test
test-e2e-specific: deps-e2e
	@echo "Running specific e2e test: $(TEST)"
	go test ./e2e -v -run $(TEST)

# Run e2e tests with coverage
test-e2e-coverage: deps-e2e
	@echo "Running e2e tests with coverage..."
	go test ./e2e -v -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Check prerequisites
check-prereqs:
	@echo "Checking prerequisites..."
	@command -v go >/dev/null 2>&1 || { echo "Go is not installed"; exit 1; }
	@command -v kind >/dev/null 2>&1 || { echo "Kind is not installed"; exit 1; }
	@command -v helm >/dev/null 2>&1 || { echo "Helm is not installed"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "kubectl is not installed"; exit 1; }
	@echo "All prerequisites are installed"

# Install prerequisites (macOS with Homebrew)
install-prereqs:
	@echo "Installing prerequisites..."
	brew install go kind helm kubectl

# Run all tests
test-all: check-prereqs test-e2e

# Development targets
dev-setup: install-prereqs deps-e2e
	@echo "Development environment setup complete"

# CI/CD targets
ci-test: check-prereqs test-e2e-timeout
	@echo "CI/CD tests completed"

# Documentation
docs:
	@echo "Generating documentation..."
	@echo "See e2e/README.md for detailed documentation"
