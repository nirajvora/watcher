---
description: Verify graph state accuracy using the graph-validator agent. Checks start token presence, exchange rate accuracy, and state consistency.
---

# Verify Graph Command

This command invokes the **graph-validator** agent to comprehensively verify the in-memory graph state.

## What This Command Does

1. **Structure Validation** - Verify nodes and edges are correct
2. **Start Token Presence** - Ensure WETH, USDC, USDbC are ALWAYS present
3. **Exchange Rate Accuracy** - Verify rates match on-chain reserves
4. **State Consistency** - Ensure graph reflects real-time state

## Critical Checks

### Start Tokens (MUST PASS)

```go
// These MUST always be in the graph
WETH  = 0x4200000000000000000000000000000000000006
USDC  = 0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913
USDbC = 0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA
```

### Exchange Rate Formula

```
rate_A_to_B = reserve_B / reserve_A * (1 - fee)
```

## Expected Output

```
# Graph Validation Report

## Structure Check
✅ Nodes represent tokens correctly
✅ Edges represent pools with bidirectional rates
✅ Graph is connected from start tokens

## Start Token Check
✅ WETH (0x4200...0006): Present with 15 edges
✅ USDC (0x8335...2913): Present with 23 edges
✅ USDbC (0xd9aA...b6CA): Present with 8 edges

## Exchange Rate Check
✅ Sampled 50 edges, all rates within 0.01% of on-chain

## State Consistency
✅ Last update: 2 seconds ago
✅ No stale edges (all updated within 60s)

## Result: ✅ GRAPH VALIDATION PASSED
```

## Related Agents

This command invokes: `graph-validator`
