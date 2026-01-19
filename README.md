# Watcher - Base Chain DEX Arbitrage Detector

A Go-based application that fetches liquidity pool data from Aerodrome V2 on Base chain and stores it in a Neo4j graph database for arbitrage opportunity detection using the Bellman-Ford algorithm.

## Architecture

### Component Overview

```
.
├── cmd/
│   └── dexgraph/          # Application entrypoint
├── pkg/
│   ├── chain/
│   │   └── base/          # Base chain RPC client
│   ├── client/            # Shared HTTP client
│   ├── config/            # Configuration and env loading
│   ├── db/                # Neo4j database operations
│   ├── dex/
│   │   └── aerodrome/     # Aerodrome V2 pool fetcher
│   └── models/            # Shared data models
├── docker-compose.yaml    # Local development environment
└── .env                   # Environment configuration
```

### Key Components

1. **Base Chain Client** (`pkg/chain/base/`)
   - Ethereum RPC client for Base network
   - Rate-limited contract calls
   - Support for reading contract state

2. **Aerodrome Integration** (`pkg/dex/aerodrome/`)
   - Fetches all V2 pools from Aerodrome Factory
   - Retrieves pool reserves and token metadata
   - Calculates exchange rates with fee adjustment (0.3% fee)
   - Filters for volatile pools (x*y=k AMM)

3. **Database Layer** (`pkg/db/`)
   - Neo4j graph database integration
   - Pool storage as bidirectional edges between assets
   - Bellman-Ford negative cycle detection using Neo4j GDS
   - Schema and index management

4. **Data Models** (`pkg/models/`)
   - Pool structure with exchange rates and decimals
   - Negative log rate calculations for arbitrage detection

## Getting Started

### Prerequisites

- Go 1.22 or later
- Docker and Docker Compose
- Base chain RPC endpoint (e.g., from Alchemy, QuickNode, etc.)

### Configuration

Create a `.env` file in the project root:

```bash
# Base chain RPC endpoint
BASE_RPC_URL=https://base-mainnet.g.alchemy.com/v2/YOUR_API_KEY

# Neo4j credentials (optional - defaults shown)
NEO4J_URI=neo4j://localhost:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=your-secure-password
```

### Local Development Setup

1. Clone the repository

2. Create your `.env` file with your Base RPC URL

3. Start the Neo4j database:
```bash
docker compose up -d neo4j
```

4. Wait for Neo4j to be healthy (about 60 seconds), then run:
```bash
make graph
```

Or run directly:
```bash
go run cmd/dexgraph/main.go
```

### Neo4j Database Access

- **Web Interface**: http://localhost:7474
- **Bolt URI**: neo4j://localhost:7687
- **Default Credentials**:
  - Username: neo4j
  - Password: your-secure-password

## How It Works

### Pool Fetching

1. Connects to Base chain via RPC
2. Queries Aerodrome V2 Factory for all pools
3. For each pool:
   - Checks if it's a volatile pool (skips stable pools)
   - Fetches token0 and token1 addresses
   - Gets reserves via `getReserves()`
   - Retrieves token decimals and symbols
   - Calculates exchange rates (accounting for 0.3% fee)

### Arbitrage Detection

1. Stores pools as edges in Neo4j graph
2. Uses negative log of exchange rates as edge weights
3. Runs Bellman-Ford algorithm via Neo4j GDS
4. Detects negative cycles (arbitrage opportunities)
5. Filters for cycles starting with major tokens (WETH, USDC, USDbC)

### Key Token Addresses (Base)

| Token | Address |
|-------|---------|
| WETH | `0x4200000000000000000000000000000000000006` |
| USDC | `0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913` |
| USDbC | `0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA` |

## Docker Deployment

Run the full stack:

```bash
docker compose up -d
```

This starts:
- Neo4j database with GDS and APOC plugins
- Watcher application (after Neo4j is healthy)

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

## Monitoring

### Neo4j Queries

View all assets:
```cypher
MATCH (a:Asset) RETURN a LIMIT 100;
```

View swap relationships:
```cypher
MATCH (a:Asset)-[r:PROVIDES_SWAP]->(b:Asset)
RETURN a.name, r.exchangeRate, b.name
LIMIT 100;
```

Check indexes:
```cypher
SHOW INDEXES;
```

## Troubleshooting

1. **RPC Connection Issues**
   - Verify BASE_RPC_URL is set correctly
   - Check RPC endpoint rate limits
   - Ensure your API key is valid

2. **Database Connection Issues**
   - Ensure Neo4j is running: `docker compose ps`
   - Check logs: `docker compose logs neo4j`
   - Verify NEO4J_PASSWORD matches docker-compose.yaml

3. **No Pools Found**
   - Check RPC connectivity
   - Verify Aerodrome Factory address is correct
   - Review application logs for errors

## References

- [Neo4j Documentation](https://neo4j.com/docs/)
- [Neo4j Graph Data Science](https://neo4j.com/docs/graph-data-science/current/)
- [Aerodrome Finance](https://aerodrome.finance/)
- [Base Chain](https://base.org/)
- [go-ethereum](https://github.com/ethereum/go-ethereum)
