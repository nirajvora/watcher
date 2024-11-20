.PHONY: all fmt ui graph

# Default target
all: graph

# Format Go code
fmt:
	gofmt -s -w .

# Run UI application
ui:
	go run cmd/ui/main.go

# Run graph application
graph:
	go run cmd/dexgraph/main.go