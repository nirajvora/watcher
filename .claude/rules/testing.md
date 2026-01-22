# Testing Requirements

## Minimum Coverage: 80%

All code MUST have 80%+ test coverage before Phase 1 is considered complete.

## Test Types Required

### Unit Tests
- All graph operations (add/update/remove)
- Bellman-Ford algorithm
- Exchange rate calculations
- Pool filtering logic
- Event parsing

### Integration Tests
- WebSocket event ingestion
- Graph state updates from events
- Detection triggered by graph updates
- End-to-end pipeline flow

### Validation Tests
- Start tokens always present
- No pool reuse in paths
- Metrics show actual activity
- Performance under 100ms

## TDD Workflow

1. **Write test first** (test should fail)
2. **Run test** - verify it fails
3. **Write minimal code** to pass test
4. **Run test** - verify it passes
5. **Refactor** if needed
6. **Check coverage** - must be 80%+

## Go Testing Commands

```bash
# Run all tests
go test -v ./...

# Run with coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific package
go test -v ./internal/graph/...

# Run with race detection
go test -v -race ./...

# Run benchmarks
go test -v -bench=. -benchmem ./...

# Run integration tests
go test -v -tags=integration ./...
```

## Test Naming

```go
// Format: Test[Function]_[Scenario]
func TestBellmanFord_DetectsNegativeCycle(t *testing.T) {}
func TestGraph_UpdateFromSyncEvent(t *testing.T) {}
func TestGraph_StartTokensAlwaysPresent(t *testing.T) {}
```

## Table-Driven Tests

```go
func TestExchangeRate_Calculation(t *testing.T) {
    tests := []struct {
        name      string
        reserve0  *big.Int
        reserve1  *big.Int
        fee       float64
        expected  float64
    }{
        {
            name:     "equal reserves",
            reserve0: big.NewInt(1000000),
            reserve1: big.NewInt(1000000),
            fee:      0.003,
            expected: 0.997,
        },
        {
            name:     "2:1 ratio",
            reserve0: big.NewInt(1000000),
            reserve1: big.NewInt(2000000),
            fee:      0.003,
            expected: 1.994,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            rate := calculateRate(tt.reserve0, tt.reserve1, tt.fee)
            require.InDelta(t, tt.expected, rate, 0.001)
        })
    }
}
```

## Mocking External Services

```go
// Mock WebSocket for testing
type MockWebSocket struct {
    events chan *SyncEvent
}

func (m *MockWebSocket) InjectEvent(e *SyncEvent) {
    m.events <- e
}

// Mock Alchemy client
type MockAlchemyClient struct{}

func (m *MockAlchemyClient) SubscribeToLogs(ctx context.Context, filter ethereum.FilterQuery) (ethereum.Subscription, error) {
    return &mockSubscription{}, nil
}
```

## Required Test Files

Every package MUST have a `*_test.go` file:

```
internal/
├── graph/
│   ├── graph.go
│   └── graph_test.go      # REQUIRED
├── detector/
│   ├── bellman_ford.go
│   └── bellman_ford_test.go  # REQUIRED
├── ingestion/
│   ├── handler.go
│   └── handler_test.go    # REQUIRED
```

## Integration Test Tags

Use build tags for integration tests:

```go
//go:build integration

package integration_test

func TestIntegration_FullPipeline(t *testing.T) {
    // Integration test code
}
```

Run with: `go test -tags=integration ./...`

## Benchmark Tests

```go
func BenchmarkBellmanFord_1000Nodes(b *testing.B) {
    graph := createLargeTestGraph(1000, 5000)
    detector := NewDetector(graph)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = detector.FindArbitrage(WETH)
    }
}
```

Target: Detection under 100ms for realistic graphs.

## Test Fixtures

Create reusable fixtures:

```go
func createProfitableTriangle(t *testing.T) *Graph {
    t.Helper()
    
    graph := NewGraph()
    // Create WETH -> USDC -> DAI -> WETH with 3% profit
    graph.AddEdge(WETH, USDC, pool1, 2000.0)
    graph.AddEdge(USDC, DAI, pool2, 1.02)
    graph.AddEdge(DAI, WETH, pool3, 0.000505)
    
    return graph
}
```

## Continuous Testing

Run tests on every change:

```bash
# Watch mode (requires entr or similar)
find . -name "*.go" | entr -c go test -v ./...

# Or use go test with -count=1 to prevent caching
go test -count=1 -v ./...
```
