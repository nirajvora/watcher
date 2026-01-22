---
description: Analyze system metrics using the metrics-analyzer agent. Identifies issues, validates component health, and provides actionable recommendations.
---

# Verify Metrics Command

This command invokes the **metrics-analyzer** agent to analyze and validate system metrics.

## What This Command Does

1. **Collect Metrics** - Gather all relevant system metrics
2. **Health Assessment** - Determine component health
3. **Issue Detection** - Identify anomalies and problems
4. **Recommendations** - Provide specific fixes

## Critical Metrics

```
# Must NOT be zero after system runs for 30+ seconds:
arbitrage_events_received_total
arbitrage_events_processed_total
arbitrage_graph_nodes_total
arbitrage_graph_edges_total
arbitrage_detections_total
```

## Expected Output

```
# Metrics Analysis Report

## Event Pipeline Metrics
| Metric | Value | Status |
|--------|-------|--------|
| Events Received | 342 | 🟢 |
| Events Processed | 338 | 🟢 |
| Events Failed | 4 | 🟢 |
| Processing Rate | 98.8% | 🟢 |

## Graph State Metrics
| Metric | Value | Status |
|--------|-------|--------|
| Nodes (Tokens) | 127 | 🟢 |
| Edges (Pools) | 456 | 🟢 |
| Last Update | 3s ago | 🟢 |

## Detection Metrics
| Metric | Value | Status |
|--------|-------|--------|
| Detections Run | 89 | 🟢 |
| Profitable Found | 2 | 🟢 |
| Avg Latency | 42ms | 🟢 |

## Anomalies
None detected ✅

## Result: ✅ ALL METRICS HEALTHY
```

## Zero Values Alert

If critical metrics are zero:

```
## ⚠️ CRITICAL ISSUE DETECTED

Events Processed: 0

### Root Cause Analysis
1. Checking WebSocket connection... OK
2. Checking subscription... OK
3. Checking message receipt... OK
4. Checking event parsing... FAILED

### Finding
Events received but not parsed. Log decoder failing.

### Recommended Fix
Check ABI definition for Sync event in internal/abi/aerodrome.go
```

## Related Agents

This command invokes: `metrics-analyzer`
