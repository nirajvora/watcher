FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 go build -o watcher ./cmd/watcher

# Runtime image
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies (SQLite needs these)
RUN apk add --no-cache ca-certificates

# Copy binary and configs
COPY --from=builder /app/watcher .
COPY --from=builder /app/configs ./configs

# Create data directory for SQLite
RUN mkdir -p /app/data

# Expose metrics port
EXPOSE 8080

CMD ["./watcher"]
