.PHONY: all fmt graph build run logs stop clean

# Default target
all: graph

# Format Go code
fmt:
	gofmt -s -w .

# Run graph application locally (without Docker)
graph:
	go run cmd/dexgraph/main.go

# Build the Docker image
build:
	docker compose build app

# Rebuild and run the application in Docker
run: build
	docker compose up -d
	docker compose logs app -f

# Show logs from the app container
logs:
	docker compose logs app -f

# Stop all containers
stop:
	docker compose down

# Clean up: stop containers, remove volumes, and prune images
clean:
	docker compose down -v
	docker image prune -f