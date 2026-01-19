# Watcher - Base Chain DEX Arbitrage Detector

A high-performance Go application that fetches liquidity pool data from Aerodrome V2 on Base chain and stores it in a Neo4j graph database for arbitrage opportunity detection using the Bellman-Ford algorithm.

## Features

- **Fast Pool Fetching**: Uses Multicall3 batching to fetch ~17,000 pools in ~3.5 minutes
- **Smart Filtering**: Automatically filters out stable pools and low-liquidity pools
- **Graph-Based Arbitrage Detection**: Uses Neo4j GDS for efficient negative cycle detection
- **Resilient**: Built-in retry logic with exponential backoff for transient RPC failures

## Architecture

### Component Overview

```
.
├── cmd/
│   └── dexgraph/              # Application entrypoint
├── pkg/
│   ├── chain/
│   │   └── base/
│   │       ├── client.go      # Base chain RPC client
│   │       └── multicall.go   # Multicall3 batching for efficient RPC calls
│   ├── config/                # Configuration and .env loading
│   ├── db/                    # Neo4j database operations & arbitrage detection
│   ├── dex/
│   │   ├── interface.go       # PoolFetcher interface
│   │   ├── service.go         # DEX service orchestrator
│   │   └── aerodrome/         # Aerodrome V2 pool fetcher
│   └── models/                # Pool data structures
├── docker-compose.yaml        # Neo4j + App containers
├── Makefile                   # Build and run commands
└── .env                       # Environment configuration
```

### Key Components

1. **Base Chain Client** (`pkg/chain/base/`)
   - Ethereum RPC client for Base network
   - **Multicall3 batching**: Batches up to 100 contract calls per RPC request
   - Retry logic with exponential backoff for transient failures
   - Handles EOF, timeout, and rate limit errors gracefully

2. **Aerodrome Integration** (`pkg/dex/aerodrome/`)
   - Fetches all V2 pools from Aerodrome Factory (`0x420DD381b31aEf6683db6B902084cB0FFECe40Da`)
   - Batched fetching: pool addresses, reserves, token metadata
   - **Filtering**:
     - Skips stable pools (only volatile x*y=k pools)
     - Skips pools with zero reserves
     - Skips pools where both reserves < 1e15 wei (~$1-10)
   - Calculates exchange rates with 0.3% fee adjustment

3. **Database Layer** (`pkg/db/`)
   - Neo4j graph database with GDS plugin
   - Pools stored as bidirectional `PROVIDES_SWAP` edges between `Asset` nodes
   - Bellman-Ford negative cycle detection for arbitrage
   - Optimized indexes on exchange rates and asset IDs

4. **Data Models** (`pkg/models/`)
   - Pool structure with exchange rates, decimals, and fees
   - Safe negative log calculations (handles edge cases to prevent infinity)

## Performance

| Metric | Value |
|--------|-------|
| Total pools in factory | ~17,000 |
| Pool fetching time | ~3.5 minutes |
| RPC calls (with multicall) | ~700 |
| Valid pools after filtering | ~10,000 |
| Stable pools filtered | ~600 |
| Low liquidity filtered | ~6,000 |

## Getting Started

### Prerequisites

- Go 1.22 or later
- Docker and Docker Compose
- Base chain RPC endpoint (Alchemy, QuickNode, Infura, etc.)

### Configuration

Create a `.env` file in the project root:

```bash
# Base chain RPC endpoint (required)
BASE_RPC_URL=https://base-mainnet.g.alchemy.com/v2/YOUR_API_KEY

# Neo4j credentials (optional - defaults shown)
NEO4J_URI=neo4j://localhost:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=your-secure-password
```

### Quick Start

```bash
# Start everything and follow logs
make run
```

This builds the Docker image, starts Neo4j and the app, and follows the logs.

### Makefile Commands

| Command | Description |
|---------|-------------|
| `make run` | Build, start containers, and follow logs |
| `make build` | Build the Docker image only |
| `make logs` | Follow logs from the running app |
| `make stop` | Stop all containers |
| `make clean` | Stop containers, remove volumes, prune images |
| `make graph` | Run locally without Docker |
| `make fmt` | Format Go code |

### Manual Setup

1. Clone the repository

2. Create your `.env` file with your Base RPC URL

3. Start Neo4j:
```bash
docker compose up -d neo4j
```

4. Wait for Neo4j to be healthy (~60 seconds), then run:
```bash
make graph
```

### Neo4j Database Access

- **Web Interface**: http://localhost:7474
- **Bolt URI**: neo4j://localhost:7687
- **Default Credentials**: neo4j / your-secure-password

## How It Works

### Pool Fetching Pipeline

```
1. Get pool count from Factory
   └── Single RPC call

2. Fetch all pool addresses (batched)
   └── ~173 multicall requests (100 addresses each)

3. Fetch pool details (batched)
   └── For each pool: stable, reserves, token0, token1
   └── 25 pools per multicall (4 calls × 25 = 100)
   └── Filter: stable pools, zero reserves, low liquidity

4. Fetch token metadata (batched)
   └── decimals, symbol for unique tokens
   └── 50 tokens per multicall (2 calls × 50 = 100)

5. Build Pool objects with exchange rates
```

### Arbitrage Detection

1. **Graph Construction**: Pools stored as weighted edges in Neo4j
2. **Edge Weight**: `-log(exchangeRate)` for each swap direction
3. **Negative Cycle Detection**: Bellman-Ford algorithm via Neo4j GDS
4. **Profit Calculation**: `profit = exp(-sum(weights))` where profit > 1 indicates arbitrage
5. **Filtering**: Only cycles starting with WETH, USDC, or USDbC

### Key Token Addresses (Base)

| Token | Address |
|-------|---------|
| WETH | `0x4200000000000000000000000000000000000006` |
| USDC | `0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913` |
| USDbC | `0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA` |

## Example Output

```
2024/01/19 19:02:25 Found 17292 pools in Aerodrome V2 Factory
2024/01/19 19:03:17 Fetched 17292 pool addresses in 52.28s
2024/01/19 19:05:40 Fetching info for 9040 unique tokens...
2024/01/19 19:05:56 Pool fetching complete in 3m31s
2024/01/19 19:05:56 Stats: 10514 valid pools, 632 stable (skipped), 5886 low liquidity (skipped), 0 errors
2024/01/19 19:06:30 Finding Arbitrage Opportunities
2024/01/19 19:06:35 Found desired cycle with profit factor: 1.002 and start liquidity: 150.5
2024/01/19 19:06:35 WETH -> USDC -> TOKEN_X -> WETH
```

## Development Guide

### Project Structure

```
pkg/
├── chain/base/
│   ├── client.go       # Base RPC client with rate limiting
│   └── multicall.go    # Multicall3 batching with retry logic
├── config/
│   └── config.go       # Environment configuration loader
├── db/
│   ├── neo4j.go        # Neo4j connection and pool storage
│   ├── arb.go          # Bellman-Ford arbitrage detection
│   ├── filter.go       # Cycle filtering by starting asset
│   └── calculations.go # Liquidity calculations
├── dex/
│   ├── interface.go    # PoolFetcher interface definition
│   ├── service.go      # Multi-DEX orchestration
│   └── aerodrome/
│       ├── client.go   # Aerodrome V2 pool fetcher
│       ├── contracts.go # ABI definitions
│       └── types.go    # Token and address types
└── models/
    └── pool.go         # Pool struct and exchange rate calculations
```

### Key Interfaces

**PoolFetcher** (`pkg/dex/interface.go`):
```go
type PoolFetcher interface {
    FetchPools(ctx context.Context) ([]models.Pool, error)
    Name() string
}
```

**Pool Model** (`pkg/models/pool.go`):
- `ExchangeRate`: Rate from Asset1 → Asset2 (includes fee)
- `ReciprocalExchangeRate`: Rate from Asset2 → Asset1 (includes fee)
- `NegativeLogExchangeRate()`: Used for Bellman-Ford edge weights
- `IsValidForArbitrage()`: Validates rates are finite and positive

### Adding a New DEX

1. Create directory: `pkg/dex/<dex_name>/`
2. Implement `PoolFetcher` interface
3. Use `pkg/chain/base.Client.BatchCallContract()` for efficient RPC calls
4. Register in `cmd/dexgraph/main.go`:
   ```go
   newDexClient := newdex.NewClient(baseClient)
   dexService := dex.NewService(aerodromeClient, newDexClient)
   ```

### Adding a New Chain

1. Create directory: `pkg/chain/<chain_name>/`
2. Copy structure from `pkg/chain/base/`
3. Update Multicall3 address if different
4. Create chain-specific DEX integrations

### Graceful Shutdown

The application handles `SIGINT` and `SIGTERM` signals:
- Context cancellation propagates to all RPC calls
- Retry loops check for context cancellation between attempts
- Database connections are properly closed via `defer`

### Code Style

- Follow standard Go project layout
- Use `make fmt` before committing
- All RPC calls should use context for cancellation
- Use the multicall infrastructure for batched RPC calls (>10 calls)
- Handle errors explicitly; don't ignore them

### Testing Locally

```bash
# Start Neo4j only
docker compose up -d neo4j

# Run application locally (faster iteration)
make graph

# Or run with Docker
make run
```

## Monitoring

### Neo4j Queries

```cypher
-- Count assets and relationships
MATCH (a:Asset) RETURN count(a) AS assets;
MATCH ()-[r:PROVIDES_SWAP]->() RETURN count(r) AS swaps;

-- View top liquidity pools
MATCH (a:Asset)-[r:PROVIDES_SWAP]->(b:Asset)
RETURN a.name, b.name, r.sourceLiquidity, r.exchangeRate
ORDER BY r.sourceLiquidity DESC
LIMIT 20;

-- Find paths between tokens
MATCH path = (a:Asset {name: 'WETH'})-[:PROVIDES_SWAP*1..3]->(b:Asset {name: 'USDC'})
RETURN path LIMIT 10;
```

## Troubleshooting

### RPC Issues
- **Rate limits**: The app uses 100ms delays between multicalls; increase if hitting limits
- **Timeouts**: Check your RPC provider's timeout settings
- **EOF errors**: Transient; the app retries automatically (3 attempts with backoff)

### Database Issues
- **Connection refused**: Ensure Neo4j is healthy: `docker compose ps`
- **Authentication failed**: Check NEO4J_PASSWORD matches docker-compose.yaml
- **Out of memory**: Neo4j needs ~2GB RAM; check Docker memory limits

### No Arbitrage Found
- This is normal - profitable arbitrage is rare and usually captured by MEV bots
- Try lowering the profit threshold in `pkg/db/arb.go`
- Check that pools are being stored: query Neo4j for asset count

## Known Limitations

- **Volatile pools only**: Currently skips Aerodrome stable pools (different AMM curve)
- **No real-time updates**: Fetches pools once at startup; no continuous monitoring
- **Single chain**: Only supports Base chain currently
- **No execution**: Detects arbitrage but doesn't execute trades

## Future Improvements

- [ ] Add support for Aerodrome stable pools (x³y + y³x = k curve)
- [ ] Implement continuous pool monitoring with WebSocket subscriptions
- [ ] Add more DEXs on Base (Uniswap V3, SushiSwap, etc.)
- [ ] Multi-chain support (Ethereum, Arbitrum, Optimism)
- [ ] Flash loan simulation for arbitrage execution
- [ ] Web dashboard for monitoring opportunities
- [ ] Unit and integration tests

## References

- [Neo4j Documentation](https://neo4j.com/docs/)
- [Neo4j Graph Data Science](https://neo4j.com/docs/graph-data-science/current/)
- [Aerodrome Finance](https://aerodrome.finance/)
- [Base Chain](https://base.org/)
- [go-ethereum](https://github.com/ethereum/go-ethereum)
- [Multicall3](https://github.com/mds1/multicall)
