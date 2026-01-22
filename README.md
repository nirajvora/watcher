# Watcher - Real-Time DEX Arbitrage Detection System

A high-performance Go application for real-time arbitrage detection on Aerodrome V2 pools on Base Network. The system monitors Sync events via WebSocket, maintains an in-memory graph state, and uses SPFA-based Bellman-Ford algorithm to detect profitable arbitrage cycles.

## Features

- **Real-Time Event Processing**: WebSocket subscription to Sync events for instant reserve updates
- **In-Memory Graph**: Copy-on-write snapshots for lock-free detection during updates
- **Fast Detection**: Sub-millisecond arbitrage detection (~0.3ms typical)
- **Pool Reuse Prevention**: Ensures each pool is used only once per arbitrage path
- **Simulation Verification**: AMM math simulation filters false positives from Bellman-Ford
- **Prometheus Metrics**: Full observability with latency histograms and counters

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    WebSocket Event Ingestion                     │
│         Alchemy WebSocket → Sync(uint256,uint256) events         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Graph State Manager                           │
│      In-memory graph with copy-on-write snapshots               │
│      Tokens as nodes, pools as edges with -log(rate) weights    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Arbitrage Detector                            │
│      SPFA-based Bellman-Ford for negative cycle detection        │
│      Start tokens: WETH, USDC, USDbC                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Simulation & Validation                       │
│      AMM constant-product math verification                      │
│      Optimal input calculation with slippage limits              │
└─────────────────────────────────────────────────────────────────┘
```

## Performance

| Metric | Value |
|--------|-------|
| Bellman-Ford execution | ~12 μs |
| Snapshot creation | ~126 μs |
| Full detection cycle | **~0.3 ms** |
| Target latency | <100 ms |
| Block time (Base) | 2 seconds |

The system runs **300x faster** than the target latency, leaving ample time for execution.

## Quick Start

### Prerequisites

- Go 1.24 or later
- Docker and Docker Compose
- Alchemy API key (for WebSocket access to Base)

### Configuration

Create a `.env` file:

```bash
# Required: Base chain endpoints
BASE_RPC_URL=https://base-mainnet.g.alchemy.com/v2/YOUR_API_KEY
BASE_WS_URL=wss://base-mainnet.g.alchemy.com/v2/YOUR_API_KEY

# Optional configuration
LOG_LEVEL=info                    # debug, info, warn, error
CURATOR_TOP_POOLS_COUNT=500       # Number of pools to track
```

### Running with Docker (Recommended)

```bash
# Build and run
make docker-run

# View logs
make docker-logs

# Stop
make docker-stop

# Rebuild after code changes
make docker-restart
```

### Running Locally

```bash
# Build and run
make run

# Or with debug logging
make debug

# Run with fewer pools for testing
make test-run
```

## Project Structure

```
.
├── cmd/watcher/           # Application entrypoint
├── internal/
│   ├── config/            # Configuration loading
│   ├── curator/           # Pool curation and bootstrap
│   ├── detector/          # Bellman-Ford detection & simulation
│   ├── graph/             # In-memory graph with snapshots
│   ├── ingestion/         # WebSocket event processing
│   ├── metrics/           # Prometheus metrics
│   └── persistence/       # SQLite caching
├── pkg/
│   ├── chain/base/        # Base chain RPC client
│   └── dex/aerodrome/     # Aerodrome V2 integration
├── configs/               # Default configuration files
├── Dockerfile             # Multi-stage Docker build
├── docker-compose.yaml    # Container orchestration
└── Makefile               # Build and run commands
```

## How It Works

### 1. Bootstrap Phase

On startup, the system:
1. Fetches top pools by TVL from Aerodrome V2 Factory
2. Prioritizes pools containing start tokens (WETH, USDC, USDbC)
3. Builds initial graph with exchange rate weights
4. Caches pool data in SQLite for faster subsequent startups

### 2. Event Processing

```
Sync Event (WebSocket)
    │
    ▼
Parse uint256 reserves ──► Graph Manager
    │                           │
    │                     Batch by block
    │                           │
    ▼                           ▼
Update edge weights ◄─── Apply updates atomically
    │
    ▼
Create snapshot ──────► Detector (parallel workers)
```

### 3. Arbitrage Detection

The detector uses **negative cycle detection** in log-space:

1. **Weight Calculation**: `weight = -log(rate × (1 - fee))`
2. **Bellman-Ford**: Finds paths where sum of weights < 0
3. **Profit Factor**: `profit = exp(-sum(weights))` where profit > 1 indicates arbitrage
4. **Simulation**: Verifies with actual AMM math and calculates optimal input

### 4. Pool Reuse Prevention

**Critical constraint**: Each pool can only be used once per arbitrage path. This prevents:
- Infinite loops in the algorithm
- Invalid paths that can't be executed atomically

Pool reuse is checked at three levels:
1. During cycle extraction
2. During path reconstruction
3. During final validation

## Key Token Addresses (Base)

| Token | Address |
|-------|---------|
| WETH | `0x4200000000000000000000000000000000000006` |
| USDC | `0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913` |
| USDbC | `0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA` |

## Monitoring

### Prometheus Metrics

Access metrics at `http://localhost:8080/metrics`

| Metric | Description |
|--------|-------------|
| `arb_events_received_total` | Sync events received |
| `arb_detection_latency_seconds` | Detection algorithm time |
| `arb_cycles_found_total` | Negative cycles detected |
| `arb_profitable_opportunities_total` | Opportunities passing simulation |
| `arb_graph_nodes` | Tokens in graph |
| `arb_graph_edges` | Edges (pool directions) in graph |
| `arb_websocket_connected` | WebSocket connection status |

### Log Output

When an arbitrage opportunity is found:

```
INF 🎯 ARBITRAGE OPPORTUNITY DETECTED
    block=41134020
    path=["WETH","USDC","TOKEN","WETH"]
    pools=["0xabc...","0xdef...","0x123..."]
    profit_factor=1.005
    profit_percent=0.5
    max_input=1.5
    estimated_profit=0.0075
    detection_latency=0.28ms
```

## Makefile Commands

| Command | Description |
|---------|-------------|
| `make run` | Build and run locally |
| `make test` | Run all tests |
| `make bench` | Run benchmarks |
| `make lint` | Run linter |
| `make docker-run` | Build and run with Docker |
| `make docker-logs` | Follow Docker logs |
| `make docker-stop` | Stop Docker container |
| `make docker-restart` | Rebuild and restart |

## Configuration Options

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `BASE_RPC_URL` | (required) | Base chain HTTP RPC endpoint |
| `BASE_WS_URL` | (required) | Base chain WebSocket endpoint |
| `LOG_LEVEL` | `info` | Logging level |
| `CURATOR_TOP_POOLS_COUNT` | `500` | Number of top pools to track |
| `SQLITE_PATH` | `data/watcher.db` | SQLite database path |
| `METRICS_PORT` | `8080` | Prometheus metrics port |

## Testing

```bash
# Run all tests
make test

# Run with coverage
go test -coverprofile=coverage.out ./...

# Run benchmarks
make bench

# Run with race detector
go test -race ./...
```

## Troubleshooting

### No Events Received

1. Check WebSocket URL points to Base mainnet (not Arbitrum or other chain)
2. Verify Alchemy API key has WebSocket access
3. Check `arb_websocket_connected` metric equals 1

### No Arbitrage Found

This is normal - profitable arbitrage is rare and usually captured by MEV bots within milliseconds. The system is working correctly if:
- `arb_cycles_found_total` > 0 (Bellman-Ford finds candidates)
- `arb_profitable_opportunities_total` may be 0 (simulation filters unprofitable)

To increase chances of finding opportunities:
- Increase `CURATOR_TOP_POOLS_COUNT` to track more pools
- The more pools tracked, the more potential paths exist

### High Latency

If detection latency exceeds 100ms:
- Check system resources (CPU, memory)
- Reduce `CURATOR_TOP_POOLS_COUNT`
- Verify no other processes competing for resources

## Known Limitations

- **Aerodrome V2 only**: Does not support V3 concentrated liquidity pools
- **Volatile pools only**: Stable pools use different AMM curve (not yet supported)
- **Detection only**: Does not execute trades (execution module planned)
- **Single chain**: Base network only

## Future Improvements

- [ ] Increase test coverage to 80%+
- [ ] Add trade execution via flashbots/MEV
- [ ] Support Aerodrome stable pools
- [ ] Multi-DEX support (Uniswap, SushiSwap)
- [ ] Web dashboard for monitoring
- [ ] Historical opportunity logging

## References

- [Aerodrome Finance](https://aerodrome.finance/)
- [Base Network](https://base.org/)
- [Bellman-Ford Algorithm](https://en.wikipedia.org/wiki/Bellman%E2%80%93Ford_algorithm)
- [Alchemy WebSocket API](https://docs.alchemy.com/reference/eth-subscribe-polygon)
