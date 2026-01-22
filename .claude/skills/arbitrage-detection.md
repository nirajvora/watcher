---
name: arbitrage-detection
description: Domain knowledge for DEX arbitrage detection on Aerodrome V2 pools. Covers graph theory, Bellman-Ford algorithm, exchange rate calculations, and Base Network specifics.
---

# Arbitrage Detection Domain Knowledge

## Arbitrage Fundamentals

### What is DEX Arbitrage?

Arbitrage exploits price differences between pools. If you can:
- Buy Token A for 1.0 Token B in Pool 1
- Sell Token A for 1.02 Token B in Pool 2
- You profit 0.02 Token B (2%)

In practice, this happens in cycles:
```
WETH → USDC → DAI → WETH
```

If the product of exchange rates > 1, there's profit:
```
rate1 * rate2 * rate3 > 1 → PROFIT
```

### Why Use Graph Theory?

Model the problem as a graph:
- **Nodes**: Tokens (WETH, USDC, DAI, etc.)
- **Edges**: Pools (each pool connects two tokens)
- **Edge Weights**: Exchange rates (or log of rates)

Finding arbitrage = Finding profitable cycles in the graph.

## Bellman-Ford for Arbitrage

### Standard Bellman-Ford

Finds shortest paths from source to all nodes. Detects negative cycles.

```
Initialize: dist[source] = 0, dist[others] = ∞
Repeat |V|-1 times:
  For each edge (u, v) with weight w:
    If dist[u] + w < dist[v]:
      dist[v] = dist[u] + w

Check for negative cycle:
  For each edge (u, v) with weight w:
    If dist[u] + w < dist[v]:
      NEGATIVE CYCLE EXISTS!
```

### Adapting for Arbitrage

To use Bellman-Ford for arbitrage:
1. Convert multiplication to addition (use log)
2. Invert the optimization (find negative cycles)

```
Exchange rates: r1 * r2 * r3 > 1 means profit
Log space:      log(r1) + log(r2) + log(r3) > 0 means profit

But Bellman-Ford finds NEGATIVE cycles, so:
Use weights:    -log(r1), -log(r2), -log(r3)

Then negative cycle in this space = profitable cycle in real space
```

### SPFA Optimization

SPFA (Shortest Path Faster Algorithm) is a queue-based optimization:

```go
func SPFA(graph *Graph, start common.Address) (bool, []Edge) {
    dist := make(map[common.Address]float64)
    parent := make(map[common.Address]*Edge)
    inQueue := make(map[common.Address]bool)
    count := make(map[common.Address]int)
    
    queue := []common.Address{start}
    dist[start] = 0
    inQueue[start] = true
    
    for len(queue) > 0 {
        u := queue[0]
        queue = queue[1:]
        inQueue[u] = false
        
        for _, edge := range graph.EdgesFrom(u) {
            v := edge.To
            newDist := dist[u] + edge.Weight
            
            if newDist < dist[v] {
                dist[v] = newDist
                parent[v] = edge
                
                if !inQueue[v] {
                    queue = append(queue, v)
                    inQueue[v] = true
                    count[v]++
                    
                    // Negative cycle detection
                    if count[v] >= len(graph.Nodes()) {
                        return true, reconstructCycle(parent, v)
                    }
                }
            }
        }
    }
    
    return false, nil
}
```

## Exchange Rate Calculations

### Constant Product Formula (Aerodrome V2)

```
x * y = k (constant)

When swapping dx of token X for dy of token Y:
(x + dx) * (y - dy) = k
dy = y - k / (x + dx)
dy = y * dx / (x + dx)

Rate (tokens Y per token X):
rate = dy / dx = y / (x + dx) ≈ y / x (for small dx)
```

### With Fees

Aerodrome V2 typically has 0.3% fee:

```
fee = 0.003
effective_rate = (y / x) * (1 - fee)
```

### Bidirectional Rates

Each pool has TWO edges (both directions):

```go
// Pool with token0/token1 reserves r0/r1
edge0to1 := r1 / r0 * (1 - fee)  // token0 → token1
edge1to0 := r0 / r1 * (1 - fee)  // token1 → token0
```

### Log Weight Calculation

```go
func calculateLogWeight(reserve0, reserve1 *big.Int, fee float64) float64 {
    rate := float64(reserve1.Int64()) / float64(reserve0.Int64())
    effectiveRate := rate * (1 - fee)
    return -math.Log(effectiveRate)
}
```

## Aerodrome V2 Specifics

### Sync Event

```solidity
event Sync(uint112 reserve0, uint112 reserve1);
```

Emitted after every swap, mint, burn. Contains updated reserves.

**Topic Hash**: `0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1`

### Pool Types

Aerodrome has two pool types:
- **Volatile**: Standard constant product (x*y=k)
- **Stable**: Optimized for like-kind assets (stablecoins)

For volatile pools (what we're targeting):
```
getAmountOut = (amountIn * fee * reserveOut) / (reserveIn + amountIn * fee)
```

### Common Token Addresses (Base)

```go
var (
    WETH  = common.HexToAddress("0x4200000000000000000000000000000000000006")
    USDC  = common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913")
    USDbC = common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA")
    DAI   = common.HexToAddress("0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb")
    AERO  = common.HexToAddress("0x940181a94A35A4569E4529A3CDfB74e38FD98631")
)
```

## Base Network Specifics

### Block Time

Base has ~2 second block times. This means:
- Events come frequently
- Opportunities are short-lived
- Must detect and act quickly (<100ms)

### RPC Providers

Alchemy is recommended for WebSocket connections:
```
wss://base-mainnet.g.alchemy.com/v2/YOUR_API_KEY
```

### Gas Considerations

Base is an L2 with low gas costs, making smaller arbitrage opportunities viable. However:
- Still need to account for gas in profit calculation
- Priority fees matter during congestion

## Practical Implementation Notes

### TVL Filtering

Not all pools are worth monitoring:
```go
// Only track pools with sufficient liquidity
if pool.TVL < MinTVLThreshold {
    continue
}

// BUT always include pools connected to start tokens
if pool.Token0 == WETH || pool.Token1 == WETH ||
   pool.Token0 == USDC || pool.Token1 == USDC ||
   pool.Token0 == USDbC || pool.Token1 == USDbC {
    // Include regardless of TVL
}
```

### Path Length Limits

Longer paths:
- More potential opportunities
- Higher gas costs
- More slippage risk
- Slower to execute

Typical limit: 2-5 hops

### Profit Threshold

After accounting for:
- Gas costs
- Slippage
- Transaction fees

Set minimum profit threshold:
```go
const MinProfitBPS = 10 // 0.1% minimum profit
```

### Execution Timing

From detection to execution:
1. Detect opportunity: ~50ms
2. Build transaction: ~10ms
3. Submit to mempool: ~20ms
4. Wait for inclusion: ~2000ms (next block)

Total: ~2080ms

Opportunity must still be profitable after 2+ seconds!
