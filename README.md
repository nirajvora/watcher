# Watcher- Cryptocurrency DEX Pool Tracker

The Watcher project impliments a tool, DEX Graph, a Go-based application that fetches liquidity pool data from various decentralized exchanges (DEXs) and stores it in a Neo4j graph database for analysis. The project is designed to support arbitrage opportunity detection across multiple DEXs.

## Architecture

### Component Overview

```
├── cmd/
│   └── dexgraph/          # Application entrypoint
├── pkg/
│   ├── client/            # Shared HTTP client
│   ├── db/                # Database operationsgit
│   ├── dex/               # DEX interfaces and implementations
│   │   ├── tinyman/       # Tinyman DEX client
│   │   ├── algofi/        # AlgoFi DEX client
│   │   └── pactfi/        # PactFi DEX client
│   └── models/            # Shared data models
├── docker-compose.yml     # Local development environment
└── README.md              # Project documentation
```

### Key Components

1. **HTTP Client** (`pkg/client/`)
   - Shared HTTP client with connection pooling
   - Configurable timeouts and retry logic
   - Context support for cancellation

2. **Database Layer** (`pkg/db/`)
   - Neo4j graph database integration
   - Pool storage and retrieval
   - Schema management
   - Index optimization

3. **DEX Integrations** (`pkg/dex/`)
   - Common interface for DEX interactions
   - Concurrent pool data fetching
   - Exchange-specific implementations

4. **Data Models** (`pkg/models/`)
   - Shared data structures
   - Type definitions for pools and assets

## Getting Started

### Prerequisites

- Go 1.22 or later
- Docker and Docker Compose

### Local Development Setup

1. Clone the repository

2. Start the Neo4j database:
```bash
docker compose up -d
```
Note: data will persist at `/var/lib/docker/volumes/` based on volumes defined in docker-compose.yaml

3. Build and run the application:
```bash
go run cmd/dexgraph/main.go
```

### Neo4j Database Access

- **Web Interface**: http://localhost:7474
- **Bolt URI**: neo4j://localhost:7687
- **Default Credentials**: 
  - Username: neo4j
  - Password: your-secure-password (set in docker-compose.yaml)

### Configuration

Key configuration parameters:

```go
// Database configuration in cmd/dexgraph/main.go
config := db.Neo4jConfig{
    URI:      "neo4j://localhost:7687",
    Username: "neo4j",
    Password: "your-secure-password",
}
```

## Contributing

### Adding a New DEX

1. Create a new directory under `pkg/dex/` for the DEX
2. Implement the `PoolFetcher` interface:
```go
type PoolFetcher interface {
    FetchPools(ctx context.Context) ([]models.Pool, error)
    Name() string
}
```
3. Add the new DEX client to the service in `cmd/dexgraph/main.go`

### Code Style

- Follow standard Go project layout
- Use `gofmt` for formatting
- Add comments for exported functions
- Include error handling
- Use context for cancellation
- Write concurrent code where appropriate

## Running Tests

```bash
go test ./...
```

## Monitoring and Maintenance

### Neo4j Database

- Monitor the database size:
```cypher
CALL dbms.components() YIELD name, version;
```

- Check indexes:
```cypher
SHOW INDEXES;
```

### Application Metrics

TODO: Add Prometheus metrics for:
- Pool fetch latency
- Success/failure rates
- Database operation latency

## Troubleshooting

Common issues and solutions:

1. **Database Connection Issues**
   - Ensure Neo4j is running: `docker compose ps`
   - Check logs: `docker compose logs neo4j`
   - Verify credentials in main.go match docker-compose.yml

2. **DEX API Issues**
   - Check rate limits
   - Verify API endpoints
   - Review error logs

## License

NIVO Technologies

## References

- [Neo4j Documentation](https://neo4j.com/docs/)
- [Go Documentation](https://golang.org/doc/)
- [Tinyman API Documentation](https://docs.tinyman.org/)