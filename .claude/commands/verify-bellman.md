---
description: Verify Bellman-Ford algorithm correctness using the bellman-ford-verifier agent. Tests cycle detection, pool reuse prevention, and performance.
---

# Verify Bellman-Ford Command

This command invokes the **bellman-ford-verifier** agent to verify the arbitrage detection algorithm.

## What This Command Does

1. **Algorithm Correctness** - Verify Bellman-Ford/SPFA implementation
2. **Negative Cycle Detection** - Verify profitable cycles are found
3. **Pool Reuse Prevention** - CRITICAL: Ensure no pool used twice in path
4. **Performance** - Verify sub-100ms detection

## Critical Checks

### Pool Reuse Prevention (MUST PASS)

```go
// A pool can only be used ONCE per path
// This prevents infinite loops!

func validatePath(path []Hop) error {
    usedPools := make(map[common.Address]bool)
    for _, hop := range path {
        if usedPools[hop.Pool] {
            return fmt.Errorf("pool %s used multiple times", hop.Pool)
        }
        usedPools[hop.Pool] = true
    }
    return nil
}
```

### Log Space Conversion

```go
// For arbitrage detection, use negative log of rates
// Negative cycle in log space = profitable cycle
logWeight := -math.Log(rate)
```

## Expected Output

```
# Bellman-Ford Verification Report

## Algorithm Check
✅ Uses log space for rate conversion
✅ SPFA optimization implemented correctly
✅ Correctly handles disconnected components

## Cycle Detection Check
✅ Detects known profitable cycle (3% profit)
✅ No false positives on unprofitable cycles
✅ Handles multi-hop cycles (up to 5 hops)

## Pool Reuse Prevention (CRITICAL)
✅ Tracks used pools per path
✅ All 100 test cases pass
✅ No pool appears twice in any detected path

## Performance Check
✅ Average detection time: 35ms
✅ P99 detection time: 78ms
✅ Target: <100ms ✅

## Result: ✅ BELLMAN-FORD VERIFICATION PASSED
```

## Related Agents

This command invokes: `bellman-ford-verifier`
