---
description: Run complete verification suite with parallel sub-agents. Validates all components, runs tests, and ensures Phase 1 requirements are met. Runs continuously until all checks pass.
---

# Verify All Command

This command orchestrates a comprehensive verification of the entire DEX arbitrage detection system using specialized sub-agents running in parallel.

## What This Command Does

1. **Parallel Agent Verification** - Launch specialized agents for each component
2. **Continuous Iteration** - Run until all checks pass
3. **Test Execution** - Run all unit, integration, and validation tests
4. **Metrics Validation** - Ensure non-zero event flow
5. **Performance Verification** - Validate sub-100ms latency

## Verification Phases

### Phase 1: Component Verification (Parallel)

Launch these agents simultaneously:

```markdown
**Agent 1: graph-validator**
- Verify graph structure correctness
- Ensure start tokens always present
- Validate exchange rate accuracy
- Check copy-on-write snapshots

**Agent 2: bellman-ford-verifier**
- Verify algorithm correctness
- Test negative cycle detection
- Verify NO pool reuse in paths
- Validate path construction

**Agent 3: event-pipeline-validator**
- Verify WebSocket connection
- Validate Sync event subscription
- Check event parsing
- Ensure events reach graph

**Agent 4: metrics-analyzer**
- Analyze current metrics
- Identify zero-value issues
- Detect anomalies
- Recommend fixes
```

### Phase 2: Test Execution

```bash
# Run all unit tests with coverage
go test -v -coverprofile=coverage.out ./...

# Run integration tests
go test -v -tags=integration -timeout=5m ./...

# Run benchmarks for performance
go test -v -bench=. -benchmem ./...
```

### Phase 3: Validation Criteria Check

```markdown
| Criterion | Required | Verification Method |
|-----------|----------|---------------------|
| Unit test coverage | ≥80% | go test -cover |
| Integration tests pass | 100% | go test -tags=integration |
| Events processed | >0 | Metrics check |
| Detections run | >0 | Metrics check |
| Start tokens present | Always | Graph inspection |
| No pool reuse | Verified | Test suite |
| Detection latency | <100ms | Benchmark |
```

## Orchestration Workflow

```
┌─────────────────────────────────────────────────────────────────┐
│                     /verify-all Started                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│              Phase 1: Parallel Component Verification            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│   │    Graph     │  │  Bellman-    │  │    Event     │          │
│   │  Validator   │  │    Ford      │  │   Pipeline   │          │
│   │              │  │  Verifier    │  │  Validator   │          │
│   └──────────────┘  └──────────────┘  └──────────────┘          │
│          │                 │                 │                   │
│          ▼                 ▼                 ▼                   │
│   ┌──────────────────────────────────────────────────────────┐  │
│   │              Collect All Results                          │  │
│   └──────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                 Phase 2: Test Execution                          │
├─────────────────────────────────────────────────────────────────┤
│   1. Unit tests with coverage                                   │
│   2. Integration tests                                          │
│   3. Benchmarks                                                 │
│   4. Race detection                                             │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                 Phase 3: Criteria Check                          │
├─────────────────────────────────────────────────────────────────┤
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  All criteria met?                                      │   │
│   │                                                         │   │
│   │  YES ──────────▶ ✅ VERIFICATION COMPLETE               │   │
│   │                                                         │   │
│   │  NO ───────────▶ 🔄 Generate fixes, iterate            │   │
│   └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ (if NO)
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Iteration Loop                                │
├─────────────────────────────────────────────────────────────────┤
│   1. Identify failing criteria                                  │
│   2. Generate specific fixes                                    │
│   3. Apply fixes                                                │
│   4. Re-run verification                                        │
│   5. Repeat until all pass (max 10 iterations)                  │
└─────────────────────────────────────────────────────────────────┘
```

## How to Use

```
User: /verify-all

Claude: Starting comprehensive verification of DEX arbitrage system...

## Phase 1: Launching Parallel Verification Agents

### 🚀 Agent 1: graph-validator
[Running graph structure validation...]
[Checking start token presence...]
[Validating exchange rates...]

### 🚀 Agent 2: bellman-ford-verifier  
[Verifying algorithm implementation...]
[Testing negative cycle detection...]
[Validating pool reuse prevention...]

### 🚀 Agent 3: event-pipeline-validator
[Checking WebSocket connection...]
[Validating Sync event handling...]
[Verifying event flow to graph...]

### 🚀 Agent 4: metrics-analyzer
[Analyzing current metrics...]
[Checking for zero-value issues...]
[Detecting anomalies...]

## Phase 2: Test Execution

$ go test -v -coverprofile=coverage.out ./...
[Test results...]

$ go test -v -tags=integration -timeout=5m ./...
[Integration test results...]

## Phase 3: Criteria Check

| Criterion | Status | Details |
|-----------|--------|---------|
| Unit test coverage | ✅ 85% | Target: ≥80% |
| Integration tests | ✅ PASS | 12/12 passing |
| Events processed | ❌ 0 | CRITICAL: Pipeline issue |
| Detections run | ❌ 0 | Blocked by events |
| Start tokens present | ✅ YES | WETH, USDC, USDbC |
| No pool reuse | ✅ VERIFIED | All tests pass |
| Detection latency | ✅ 45ms | Target: <100ms |

## Issues Found

### ❌ CRITICAL: Event Pipeline Not Flowing

**Root Cause:** Event channel not being consumed
**Location:** internal/ingestion/handler.go:45
**Fix:** Start event processing goroutine

## Applying Fixes...

[Code fix applied]

## Re-running Verification...

[Results after fix...]

## Final Result: ✅ ALL CRITERIA MET

Phase 1 verification complete!
```

## Iteration Logic

```go
// Pseudo-code for verification loop
func verifyAll() {
    maxIterations := 10
    
    for i := 0; i < maxIterations; i++ {
        // Run parallel verification
        results := runParallelVerification()
        
        // Run tests
        testResults := runTests()
        
        // Check criteria
        allPassed, failures := checkCriteria(results, testResults)
        
        if allPassed {
            fmt.Println("✅ ALL CRITERIA MET")
            return
        }
        
        // Generate and apply fixes
        fixes := generateFixes(failures)
        applyFixes(fixes)
        
        fmt.Printf("Iteration %d: %d failures, attempting fixes...\n", i+1, len(failures))
    }
    
    fmt.Println("❌ Max iterations reached, manual intervention required")
}
```

## Success Criteria

Verification is COMPLETE when ALL of the following pass:

- [ ] **Unit Tests**: 80%+ coverage, all passing
- [ ] **Integration Tests**: 100% passing
- [ ] **Graph Validation**: Start tokens present, rates accurate
- [ ] **Bellman-Ford**: Correct detection, no pool reuse
- [ ] **Event Pipeline**: Non-zero events processed
- [ ] **Metrics**: Non-zero detections run
- [ ] **Performance**: Sub-100ms detection latency

## Related Agents

- `graph-validator` - Graph state verification
- `bellman-ford-verifier` - Algorithm verification
- `event-pipeline-validator` - Event flow verification
- `metrics-analyzer` - Metrics analysis
- `integration-tester` - End-to-end tests

## Important Notes

**CRITICAL**: This command runs CONTINUOUSLY until all criteria pass or max iterations (10) reached. It will:

1. Identify specific failures
2. Generate targeted fixes
3. Apply fixes to code
4. Re-verify to confirm fix worked
5. Repeat until complete

The goal is ZERO human intervention required to get Phase 1 working.
