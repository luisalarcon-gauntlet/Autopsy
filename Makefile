.PHONY: dev build test lint clean demo-cluster demo-bundle evals \
        docker-build docker-run docker-stop docker-logs docker-shell help

BINARY_NAME=autopsy
GO=go
AIR=air

## help: Show this help message
help:
	@echo "Autopsy — Makefile targets"
	@echo ""
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/  /'

# ── Development ────────────────────────────────────────────────────────────────

## dev: Start development server with live reload (requires air)
dev:
	@which air > /dev/null 2>&1 || (echo "Installing air..." && go install github.com/air-verse/air@latest)
	@echo "Starting Autopsy dev server..."
	$(AIR) -c .air.toml

## build: Build the binary
build:
	$(GO) build -ldflags="-s -w" -o ./$(BINARY_NAME) .

## test: Run all tests with race detector
test:
	$(GO) test -race -v ./...

## test-short: Run tests without verbose output
test-short:
	$(GO) test -race ./...

## lint: Run go vet and staticcheck
lint:
	$(GO) vet ./...
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping"

## check: Run all checks (test + lint + build)
check: test lint build
	@echo "All checks passed ✓"

## clean: Remove build artifacts and temp files
clean:
	rm -f $(BINARY_NAME)
	rm -rf tmp/
	rm -rf /tmp/autopsy-*

## setup: Install all development dependencies
setup:
	go install github.com/air-verse/air@latest
	@echo "Setup complete. Run 'make dev' to start."

# ── Demo Cluster ───────────────────────────────────────────────────────────────

## demo-cluster: Create a kind cluster with scripted failures for demo
demo-cluster:
	@which kind > /dev/null 2>&1 || (echo "ERROR: kind not installed. See https://kind.sigs.k8s.io/" && exit 1)
	./scripts/setup-cluster.sh

## demo-bundle: Generate a support bundle from the demo cluster
demo-bundle:
	@which kubectl > /dev/null 2>&1 || (echo "ERROR: kubectl not installed" && exit 1)
	./scripts/generate-bundle.sh

# ── Docker ─────────────────────────────────────────────────────────────────────

## docker-build: Build the Docker image
docker-build:
	docker build -t autopsy:latest .
	@echo ""
	@echo "Image size:"
	@docker image inspect autopsy:latest --format='{{.Size}}' | \
	  awk '{printf "  %.1f MB\n", $$1/1024/1024}'

## docker-run: Run Autopsy via docker-compose (builds if needed)
docker-run:
	docker-compose up --build

## docker-stop: Stop the running container
docker-stop:
	docker-compose down

## docker-logs: Tail container logs
docker-logs:
	docker-compose logs -f autopsy

## docker-shell: Open a shell in the running container (debug only)
docker-shell:
	docker exec -it autopsy sh

## docker-clean: Remove the autopsy image and containers
docker-clean:
	docker-compose down --rmi local --volumes

## evals: Run AI evaluation suite (requires ANTHROPIC_API_KEY)
evals:
	./evals/run.sh
