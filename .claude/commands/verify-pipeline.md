---
description: Verify event pipeline flow using the event-pipeline-validator agent. Ensures WebSocket connection, event parsing, and actual event flow as shown by metrics.
---

# Verify Pipeline Command

This command invokes the **event-pipeline-validator** agent to verify the event ingestion pipeline.

## What This Command Does

1. **WebSocket Connection** - Verify connection to Alchemy
2. **Event Subscription** - Verify Sync event subscription
3. **Event Processing** - Verify events are parsed and handled
4. **Metrics Flow** - CRITICAL: Verify non-zero events processed

## Critical Issue

From project memory: "Metrics analysis revealed the event pipeline isn't flowing - showing zero events processed and zero detections run."

This is the ROOT CAUSE of the system not working. This command specifically diagnoses and fixes this issue.

## Common Failure Points

```
1. WebSocket connects but never subscribes
2. Subscribing to wrong topic hash
3. Pool filter excludes all pools
4. Messages received but not decoded
5. Events sent to channel but not consumed
6. Event processor goroutine not started
```

## Sync Event Topic

```
0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1
```

## Expected Output

```
# Event Pipeline Validation Report

## WebSocket Check
✅ Connected to Alchemy WSS endpoint
✅ Connection stable (no disconnects in 60s)

## Subscription Check
✅ Subscribed to Sync events
✅ Correct topic hash used
✅ Subscription ID: 0x1234...

## Event Processing Check
✅ Receiving messages from WebSocket
✅ Events parsed correctly
✅ Events sent to processing channel
✅ Channel is being consumed

## Metrics Check (CRITICAL)
✅ Events received: 156
✅ Events processed: 152
✅ Processing rate: 97.4%

## Result: ✅ EVENT PIPELINE VALIDATION PASSED
```

## If Failing

If this check fails, the agent will:

1. Identify exact failure point
2. Provide specific code location
3. Generate fix
4. Verify fix resolves issue

## Related Agents

This command invokes: `event-pipeline-validator`
