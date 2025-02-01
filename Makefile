.PHONY: all fmt ui graph

# Default target
all: graph

# Format Go code
fmt:
	gofmt -s -w .

# Run graph application
graph:
	go run cmd/dexgraph/main.go