.PHONY: build run test bench lint clean infra infra-down fmt docker-build docker-run docker-stop docker-logs docker-rebuild

# Build the watcher binary
build:
	@mkdir -p bin
	go build -o bin/watcher ./cmd/watcher

# Run the watcher application
run: build
	@mkdir -p data
	./bin/watcher

# Run tests
test:
	go test -v -race ./...

# Run benchmarks
bench:
	go test -bench=. -benchmem ./internal/detector/

# Run linter
lint:
	golangci-lint run

# Format Go code
fmt:
	gofmt -s -w .

# Clean build artifacts and data
clean:
	rm -rf bin/ data/

# Start infrastructure (Neo4j for future analytical use)
infra:
	docker compose up -d neo4j

# Stop infrastructure
infra-down:
	docker compose down

# Run with debug logging
debug: build
	@mkdir -p data
	LOG_LEVEL=debug ./bin/watcher

# Run with reduced pool count for faster testing
test-run: build
	@mkdir -p data
	CURATOR_TOP_POOLS_COUNT=100 ./bin/watcher

# Docker commands
docker-build:
	docker compose build watcher

docker-run: docker-build
	docker compose up -d watcher

docker-stop:
	docker compose stop watcher

docker-logs:
	docker compose logs -f watcher

docker-rebuild:
	docker compose build --no-cache watcher
	docker compose up -d watcher

# Full Docker restart (stop, rebuild, run)
docker-restart: docker-stop docker-rebuild
