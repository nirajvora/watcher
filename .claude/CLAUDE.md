# DEX Arbitrage Detection System - Claude Code Configuration

## Project Overview

Real-time DEX arbitrage detection system for Aerodrome V2 pools on Base Network. The system monitors Sync events via WebSocket, maintains an in-memory graph state, and uses SPFA-based Bellman-Ford algorithm to detect profitable arbitrage cycles.

**Target Performance**: Complete pipeline from event receipt to opportunity identification in under 100ms (Base has 2-second block times).

## Architecture Components

```
┌─────────────────────────────────────────────────────────────────┐
│                    Event Ingestion Service                       │
│         WebSocket connection to Alchemy for Sync events          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Graph State Manager                           │
│      In-memory graph with copy-on-write snapshots               │
│      Tokens as nodes, pools as edges with exchange rates         │
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
│                    Pool Curator                                  │
│      Manages tracked pools, TVL filtering, token validation      │
└─────────────────────────────────────────────────────────────────┘
```

## Critical Rules (MUST FOLLOW)

### 1. Start Tokens MUST Be In Graph
CRITICAL: WETH, USDC, and USDbC must ALWAYS be included in the graph regardless of TVL filtering. Detection silently fails if they're not present.

```go
// Start tokens that must always be in the graph
var StartTokens = []common.Address{
    common.HexToAddress("0x4200000000000000000000000000000000000006"), // WETH
    common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"), // USDC
    common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA"), // USDbC
}
```

### 2. No Pool Reuse in Arbitrage Paths
CRITICAL: A pool can only be used ONCE within a single arbitrage path. This prevents infinite loops in the Bellman-Ford algorithm.

### 3. Real-Time Event Flow Validation
CRITICAL: Metrics must show actual event processing. Zero events processed = broken pipeline.

### 4. Sub-100ms Response Time
All operations from event receipt to detection must complete in under 100ms.

## Verification Requirements

### Phase 1: Foundation Verification
1. **Graph State Accuracy**: In-memory graph must accurately reflect real-time state from Sync events
2. **Bellman-Ford Correctness**: Algorithm must correctly detect profitable cycles from start tokens
3. **Event Pipeline Flow**: Metrics must show non-zero events processed and detections run
4. **No Infinite Loops**: Pool reuse prevention must be verified

## Testing Strategy

### Unit Tests (Required)
- Graph operations (add/update/remove edges)
- Bellman-Ford cycle detection
- Exchange rate calculations
- Pool filtering logic

### Integration Tests (Required)
- WebSocket event ingestion
- Graph state updates from events
- End-to-end detection pipeline

### Validation Tests (Required)
- Verify start tokens always present
- Verify no pool reuse in paths
- Verify metrics show actual activity
- Performance benchmarks (<100ms)

## Code Standards

### Go Patterns
- Use interfaces for testability
- Prefer channels for concurrency
- Use context for cancellation
- Structured logging with slog

### Error Handling
```go
// Always wrap errors with context
if err != nil {
    return fmt.Errorf("failed to update graph for pool %s: %w", poolAddr, err)
}
```

### Testing Pattern
```go
func TestBellmanFord_DetectsNegativeCycle(t *testing.T) {
    // Arrange
    graph := NewTestGraph()
    graph.AddEdge(tokenA, tokenB, pool1, rate1)
    
    // Act
    cycles, err := detector.FindArbitrage(startToken)
    
    // Assert
    require.NoError(t, err)
    require.NotEmpty(t, cycles)
}
```

## Agent Delegation

Use specialized agents for:
- **graph-validator**: Verify graph state accuracy
- **bellman-ford-verifier**: Verify algorithm correctness
- **event-pipeline-validator**: Verify event flow
- **metrics-analyzer**: Analyze and validate metrics
- **integration-tester**: Run end-to-end tests

## Success Criteria

Phase 1 is COMPLETE when:
- [ ] All unit tests pass with 80%+ coverage
- [ ] Integration tests verify event flow
- [ ] Metrics show non-zero events processed
- [ ] Bellman-Ford correctly detects known profitable cycles
- [ ] No pool reuse in detected paths
- [ ] Start tokens always present in graph
- [ ] Sub-100ms detection time verified

## Commands

- `/verify-all` - Run complete verification suite
- `/verify-graph` - Verify graph state accuracy
- `/verify-bellman` - Verify Bellman-Ford correctness
- `/verify-pipeline` - Verify event pipeline flow
- `/verify-metrics` - Analyze metrics for issues
