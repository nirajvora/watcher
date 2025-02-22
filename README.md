# Watcher- Cryptocurrency DEX Pool Tracker

![Graph Visualization Example](docs/images/Uatu_the_Watcher.png)

The Watcher project impliments a tool, DEX Graph, a Go-based application that fetches liquidity pool data from various decentralized exchanges (DEXs) and stores it in a Neo4j graph database for analysis. The project is designed to support arbitrage opportunity detection across multiple DEXs.

## Architecture

### Component Overview

```
.
├── README.md
├── docs
│   └── images/            # Assets for this README
├── cmd/
│   ├── dexgraph/          # Application entrypoint
├── pkg/
│   ├── client/            # Shared HTTP client
│   ├── db/                # Database operations
│   ├── dex/               # DEX interfaces and implementations
│   └── models/            # Shared data models
├── static/                # Static web assets
└── docker-compose.yaml    # Local development environment
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

3. Build and run the main application:
```bash
make graph
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
- Include error handling
- Use context for cancellation
- Write concurrent code where appropriate

## Running Tests

TODO: Impliment tests so we can run:

```bash
go test ./...
```

## Monitoring and Maintenance

### Neo4j Database

- View the database config:
```cypher
CALL dbms.listConfig
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


## References

- [Neo4j Documentation](https://neo4j.com/docs/)
- [Go Documentation](https://golang.org/doc/)
- [Tinyman API Documentation](https://docs.tinyman.org/)

## Misc.

Example of arb that gets identified with `negLogRate < 0` and not `exp(-negLogRate) > 1` 

```txt
< Found arbitrage opportunity along the following route with profit factor: 1
<  ZONE->Elephant Nips->ZONE
< Detailed information about each Node on the route:
< [{95 4:50561179-b898-4249-8075-5768feb9d457:95 [Asset] map[id:444035862 name:ZONE]} {236 4:50561179-b898-4249-8075-5768feb9d457:236 [Asset] map[id:640873368 name:Elephant Nips]} {95 4:50561179-b898-4249-8075-5768feb9d457:95 [Asset] map[id:444035862 name:ZONE]}]
< negLogRate for each liquidity pool necessary for facilitating arb:
< [0 -0.49344432415171424 -1.1102230246251565e-16]
```