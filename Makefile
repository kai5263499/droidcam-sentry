.PHONY: help build run stop restart clean swagger lint test start install-opencv deps
help:
	@echo "DroidCam Sentry - Makefile Commands:"
	@echo "  make build        - Build the application"
	@echo "  make start        - Start the application in background"
	@echo "  make stop         - Stop the application"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make swagger      - Generate Swagger API documentation"
	@echo ""
	@echo "Linting:"
	@echo "  make lint         - Lint with Docker (no OpenCV, fast)"
	@echo "  make lint-local   - Lint all code including OpenCV packages"
	@echo "  make lint-ci      - Lint without OpenCV (CI compatible)"
	@echo ""
	@echo "Testing:"
	@echo "  make test         - Run tests (no OpenCV packages)"
	@echo "  make test-local   - Run all tests including OpenCV"
	@echo "  make test-coverage - View test coverage in browser"
	@echo ""
	@echo "Setup:"
	@echo "  make install-opencv - Install OpenCV dependencies"
	@echo "  make deps         - Install Go dependencies"
# Build the application
build: swagger
	@echo "Building backend..."
	@cd backend && go build -tags opencv -o ../droidcam-sentry main.go
	@echo "Build complete: ./droidcam-sentry"

# Start application (background mode)
start: build
	@echo "Starting DroidCam Sentry..."
	@# Check if already running via PID file
	@if [ -f droidcam-sentry.pid ]; then \
		PID=$$(cat droidcam-sentry.pid); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "Application already running (PID: $$PID)"; \
			exit 1; \
		else \
			echo "Removing stale PID file..."; \
			rm -f droidcam-sentry.pid; \
		fi \
	fi
	@# Check if port 8080 is already in use
	@if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then \
		echo "Error: Port 8080 is already in use. Stop the existing process first:"; \
		lsof -Pi :8080 -sTCP:LISTEN; \
		echo ""; \
		echo "Run 'make stop' to kill the process using port 8080"; \
		exit 1; \
	fi
	@# Start the application
	@./droidcam-sentry & echo $$! > droidcam-sentry.pid
	@sleep 2
	@# Check if started successfully
	@if [ -f droidcam-sentry.pid ]; then \
		PID=$$(cat droidcam-sentry.pid); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "Started successfully (PID: $$PID)"; \
			echo "Web UI: http://192.168.2.149:8080"; \
			echo "Swagger API: http://192.168.2.149:8080/swagger/index.html"; \
			echo ""; \
			echo "pprof: pproftui --module-path=\"github.com/kai5263499\" -live=\"http://192.168.2.149:6060/debug/pprof/profile?seconds=5\" -refresh=10s"; \
			echo "  or: go tool pprof -http=:8081 http://192.168.2.149:6060/debug/pprof/profile?seconds=60"; \
		else \
			echo "Failed to start. Process died immediately."; \
			rm -f droidcam-sentry.pid; \
			exit 1; \
		fi \
	else \
		echo "Failed to create PID file."; \
		exit 1; \
	fi

# Stop application
stop:
	@STOPPED=0; \
	if [ -f droidcam-sentry.pid ]; then \
		PID=$$(cat droidcam-sentry.pid); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "Stopping DroidCam Sentry (PID: $$PID)..."; \
			kill $$PID 2>/dev/null || true; \
			sleep 1; \
			if ps -p $$PID > /dev/null 2>&1; then \
				echo "Process didn't stop, force killing..."; \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
			STOPPED=1; \
		else \
			echo "Process not running (stale PID file)"; \
		fi; \
		rm -f droidcam-sentry.pid; \
	fi; \
	if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then \
		echo "Killing processes on port 8080:"; \
		lsof -Pi :8080 -sTCP:LISTEN; \
		lsof -Pi :8080 -sTCP:LISTEN -t | xargs kill 2>/dev/null || true; \
		sleep 1; \
		if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then \
			echo "Force killing..."; \
			lsof -Pi :8080 -sTCP:LISTEN -t | xargs kill -9 2>/dev/null || true; \
		fi; \
		STOPPED=1; \
	fi; \
	if [ $$STOPPED -eq 1 ]; then \
		echo "Stopped"; \
	else \
		echo "Not running"; \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f droidcam-sentry
	@rm -f droidcam-sentry.pid
	@rm -rf backend/docs
	@echo "Clean complete"

# Lint backend code using Docker (same as GitHub Actions)
# Filters out GoCV-related errors since OpenCV isn't in the container
lint:
	@echo "Linting backend code using golangci-lint (Docker)..."
	@docker run --rm -v $$(pwd)/backend:/app -v $$(pwd)/.golangci.yml:/app/.golangci.yml -w /app \
		golangci/golangci-lint:latest golangci-lint run --timeout=5m ./... 2>&1 | \
		grep -v gocv | grep -v opencv | grep -v PKG_CONFIG | grep -v typecheck | grep -v "^\t^" | grep -v "^1 issues:" || echo "Lint completed (OpenCV errors filtered)"

# Run tests (non-OpenCV packages only)
test:
	@echo "Running tests (non-OpenCV packages)..."
	@cd backend && go test -v -race -coverprofile=coverage.out -covermode=atomic \
		./internal/config \
		./internal/health \
		./internal/logger
	@echo ""
	@echo "Coverage summary (non-OpenCV packages):"
	@cd backend && go tool cover -func=coverage.out | tail -1
	@echo "Full coverage report: backend/coverage.out"
	@echo ""
	@echo "To test all packages including OpenCV: make test-local"

test-coverage:
	@cd backend && go tool cover -html=coverage.out

# Generate Swagger API documentation
swagger:
	@echo "Generating Swagger documentation..."
	@if command -v ~/go/bin/swag >/dev/null 2>&1; then \
		cd backend && ~/go/bin/swag init -g main.go --output docs; \
		echo "Swagger docs generated at backend/docs/"; \
		echo "View at: http://192.168.2.149:8080/swagger/index.html"; \
	else \
		echo "swag not found. Install it with:"; \
		echo "  go install github.com/swaggo/swag/cmd/swag@latest"; \
	fi

# Install OpenCV dependencies
install-opencv:
	@echo "Installing OpenCV dependencies..."
	@sudo apt-get update
	@sudo apt-get install -y libopencv-dev pkg-config
	@echo "OpenCV dependencies installed"

# Install Go dependencies
deps:
	@echo "Installing Go dependencies..."
	@cd backend && go mod download
	@echo "Dependencies installed"

# Lint with OpenCV support (local development)
lint-local:
	@echo "Linting all code (including OpenCV packages)..."
	@cd backend && go vet -tags opencv ./...
	@echo "Lint complete"

# Lint without OpenCV (matches CI behavior)  
lint-ci:
	@echo "Linting code (excluding OpenCV packages)..."
	@cd backend && go vet ./internal/config ./internal/health ./internal/logger ./internal/server ./cmd/...
	@echo "CI lint complete"

# Run tests locally with OpenCV
test-local:
	@echo "Running all tests (including OpenCV tests)..."
	@cd backend && go test -v -race -tags opencv -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "Coverage summary:"
	@cd backend && go tool cover -func=coverage.out | tail -1
	@echo "Full coverage report: backend/coverage.out"
