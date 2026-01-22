# Critical Constraints

These constraints are NON-NEGOTIABLE. Violation of any constraint means the system is BROKEN.

## 1. Start Tokens MUST Be In Graph

**Priority: CRITICAL**

WETH, USDC, and USDbC MUST always be in the graph, regardless of TVL filtering.

```go
var StartTokens = []common.Address{
    common.HexToAddress("0x4200000000000000000000000000000000000006"), // WETH
    common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"), // USDC
    common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA"), // USDbC
}
```

**Why**: Detection starts from these tokens. If they're not in the graph, detection silently returns nothing - no error, just no results.

**Verification**:
```go
func (g *Graph) ValidateStartTokens() error {
    for _, token := range StartTokens {
        if !g.HasNode(token) {
            return fmt.Errorf("start token %s not in graph", token)
        }
        if len(g.EdgesFrom(token)) == 0 {
            return fmt.Errorf("start token %s has no edges", token)
        }
    }
    return nil
}
```

## 2. No Pool Reuse In Paths

**Priority: CRITICAL**

A pool can only be used ONCE within a single arbitrage path.

```go
// WRONG: May reuse pool and infinite loop
type Path struct {
    Tokens []common.Address
}

// CORRECT: Tracks used pools
type Path struct {
    Hops      []Hop
    UsedPools map[common.Address]bool
}

func (p *Path) CanUsePool(pool common.Address) bool {
    return !p.UsedPools[pool]
}
```

**Why**: Aerodrome pools are bidirectional (can swap A→B or B→A in same pool). Without this constraint, the algorithm can loop indefinitely through the same pool.

**Verification**:
```go
func validatePath(path *Path) error {
    seen := make(map[common.Address]bool)
    for _, hop := range path.Hops {
        if seen[hop.Pool] {
            return fmt.Errorf("pool %s used multiple times", hop.Pool)
        }
        seen[hop.Pool] = true
    }
    return nil
}
```

## 3. Sub-100ms Detection Latency

**Priority: HIGH**

Complete pipeline from event receipt to opportunity identification MUST complete in under 100ms.

**Why**: Base has 2-second block times. If detection takes longer than 100ms, opportunities will be stale by the time we try to execute.

**Verification**:
```go
func (d *Detector) FindArbitrage(ctx context.Context) ([]Cycle, error) {
    start := time.Now()
    defer func() {
        latency := time.Since(start)
        d.metrics.RecordLatency(latency)
        if latency > 100*time.Millisecond {
            d.log.Warn("detection exceeded 100ms", "latency", latency)
        }
    }()
    
    // ... detection logic
}
```

## 4. Non-Zero Event Flow

**Priority: CRITICAL**

After running for 30 seconds, metrics MUST show non-zero values for:
- Events received
- Events processed
- Detections run

**Why**: Zero events = broken pipeline. The system appears to work (connects, starts) but does nothing.

**Verification**:
```go
func (s *System) HealthCheck() error {
    metrics := s.Metrics()
    
    if metrics.EventsReceived == 0 {
        return errors.New("no events received - check WebSocket")
    }
    if metrics.EventsProcessed == 0 {
        return errors.New("no events processed - check handler")
    }
    if metrics.DetectionsRun == 0 {
        return errors.New("no detections run - check trigger")
    }
    
    return nil
}
```

## 5. Graph Consistency

**Priority: HIGH**

Graph updates must be atomic. No partial updates that leave graph inconsistent.

```go
// WRONG: Non-atomic update
func (g *Graph) UpdatePool(pool Pool) {
    g.edges[key1] = edge1  // What if crash here?
    g.edges[key2] = edge2
}

// CORRECT: Atomic with copy-on-write
func (g *Graph) UpdatePool(pool Pool) {
    g.mu.Lock()
    defer g.mu.Unlock()
    
    newEdges := make(map[EdgeKey]*Edge)
    for k, v := range g.edges {
        newEdges[k] = v
    }
    newEdges[key1] = edge1
    newEdges[key2] = edge2
    
    g.edges = newEdges
}
```

## Constraint Verification Checklist

Before declaring Phase 1 complete, ALL must be verified:

- [ ] Start tokens present in graph (tested with 100+ cases)
- [ ] No pool reuse in any detected path (tested with 100+ cases)
- [ ] Detection latency under 100ms (p99)
- [ ] Events processed > 0 after 30 seconds
- [ ] Detections run > 0 after 30 seconds
- [ ] Graph updates are atomic (race detector clean)

## If Any Constraint Violated

**STOP** and fix immediately. Do not proceed with other work.

1. Identify the violation
2. Write a failing test that catches it
3. Fix the code
4. Verify the test passes
5. Run full verification suite
6. Only then continue
