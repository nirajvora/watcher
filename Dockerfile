FROM golang:1.22.2-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o watcher ./cmd/dexgraph

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/watcher .
CMD ["./watcher"]