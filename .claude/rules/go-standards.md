# Go Coding Standards

## Code Organization

### File Size
- Target: 200-400 lines
- Maximum: 800 lines
- If larger, split by responsibility

### Package Structure
```
internal/
├── graph/           # Graph data structures
│   ├── graph.go
│   ├── graph_test.go
│   ├── node.go
│   └── edge.go
├── detector/        # Arbitrage detection
│   ├── bellman_ford.go
│   ├── bellman_ford_test.go
│   └── path.go
├── ingestion/       # Event ingestion
│   ├── websocket.go
│   ├── handler.go
│   └── parser.go
├── curator/         # Pool management
│   ├── curator.go
│   └── filter.go
└── metrics/         # Observability
    ├── metrics.go
    └── prometheus.go
```

## Error Handling

### Always Wrap Errors

```go
// GOOD: Context added
if err != nil {
    return fmt.Errorf("failed to update pool %s: %w", poolAddr, err)
}

// BAD: No context
if err != nil {
    return err
}
```

### Use Sentinel Errors

```go
var (
    ErrPoolNotFound    = errors.New("pool not found")
    ErrInvalidReserves = errors.New("invalid reserves")
    ErrStartTokenMissing = errors.New("start token missing from graph")
)

// Check with errors.Is
if errors.Is(err, ErrPoolNotFound) {
    // Handle missing pool
}
```

## Concurrency

### Use Channels for Communication

```go
// Event processing pipeline
events := make(chan *SyncEvent, 100) // Buffered channel

// Producer
go func() {
    for {
        event := receiveEvent()
        events <- event
    }
}()

// Consumer
go func() {
    for event := range events {
        processEvent(event)
    }
}()
```

### Use sync.Mutex for Shared State

```go
type Graph struct {
    mu    sync.RWMutex
    nodes map[common.Address]*Node
    edges map[EdgeKey]*Edge
}

func (g *Graph) GetNode(addr common.Address) *Node {
    g.mu.RLock()
    defer g.mu.RUnlock()
    return g.nodes[addr]
}

func (g *Graph) AddNode(node *Node) {
    g.mu.Lock()
    defer g.mu.Unlock()
    g.nodes[node.Address] = node
}
```

### Use Context for Cancellation

```go
func (s *Service) Start(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case event := <-s.events:
            if err := s.processEvent(event); err != nil {
                s.log.Error("process event failed", "err", err)
            }
        }
    }
}
```

## Logging

### Use Structured Logging (slog)

```go
import "log/slog"

// Create logger
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

// Use structured fields
logger.Info("event processed",
    "pool", poolAddr,
    "reserve0", reserve0,
    "reserve1", reserve1,
    "latency_ms", latency.Milliseconds(),
)

// Log errors with context
logger.Error("failed to update graph",
    "err", err,
    "pool", poolAddr,
)
```

## Interfaces

### Define Small Interfaces

```go
// Good: Small, focused interface
type EventHandler interface {
    HandleSyncEvent(event *SyncEvent) error
}

type GraphUpdater interface {
    UpdateEdge(from, to common.Address, pool common.Address, rate float64) error
}

// Bad: Large interface
type Everything interface {
    HandleSyncEvent(event *SyncEvent) error
    UpdateEdge(...) error
    FindArbitrage(...) ([]Cycle, error)
    // ... too many methods
}
```

### Accept Interfaces, Return Structs

```go
// Good: Accept interface
func NewDetector(graph GraphReader) *Detector {
    return &Detector{graph: graph}
}

// This allows easy testing with mocks
type mockGraph struct{}
func (m *mockGraph) GetEdges() []Edge { ... }
```

## Testing

### Use testify/require

```go
import "github.com/stretchr/testify/require"

func TestSomething(t *testing.T) {
    result, err := doSomething()
    
    require.NoError(t, err)
    require.NotNil(t, result)
    require.Equal(t, expected, result.Value)
    require.InDelta(t, 1.5, result.Rate, 0.001)
}
```

### Use Table-Driven Tests

```go
func TestCalculateRate(t *testing.T) {
    tests := []struct {
        name     string
        r0, r1   *big.Int
        expected float64
    }{
        {"1:1", big.NewInt(1000), big.NewInt(1000), 1.0},
        {"2:1", big.NewInt(1000), big.NewInt(2000), 2.0},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := calculateRate(tt.r0, tt.r1)
            require.InDelta(t, tt.expected, got, 0.001)
        })
    }
}
```

## Performance

### Avoid Allocations in Hot Paths

```go
// Good: Reuse slice
func (d *Detector) FindArbitrage() []Cycle {
    d.cycles = d.cycles[:0] // Reuse underlying array
    // ... populate cycles
    return d.cycles
}

// Bad: Allocate every call
func (d *Detector) FindArbitrage() []Cycle {
    cycles := make([]Cycle, 0)
    // ... populate cycles
    return cycles
}
```

### Use sync.Pool for Temporary Objects

```go
var pathPool = sync.Pool{
    New: func() interface{} {
        return &Path{
            Hops:      make([]Hop, 0, 10),
            UsedPools: make(map[common.Address]bool),
        }
    },
}

func getPath() *Path {
    return pathPool.Get().(*Path)
}

func putPath(p *Path) {
    p.Hops = p.Hops[:0]
    clear(p.UsedPools)
    pathPool.Put(p)
}
```

## Naming Conventions

```go
// Package names: lowercase, single word
package graph

// Types: PascalCase
type ArbitrageDetector struct {}

// Functions: PascalCase for exported, camelCase for internal
func FindArbitrage() {}
func calculateWeight() {}

// Variables: camelCase
var eventChannel chan *Event

// Constants: PascalCase for exported
const MaxPathLength = 5
const defaultTimeout = 30 * time.Second

// Interfaces: -er suffix when single method
type Reader interface { Read() }
type GraphReader interface { GetEdges() []Edge }
```
