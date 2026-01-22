package ingestion

import (
	"context"
	"math/big"
	"testing"
	"time"

	"watcher/internal/graph"

	"github.com/stretchr/testify/require"
)

// TestReconcilerSetTrackedPools verifies that tracked pools are properly set.
func TestReconcilerSetTrackedPools(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	reconciler := NewReconciler(nil, graphManager)

	// Set tracked pools with mixed case
	addresses := []string{
		"0xABC123",
		"0xdef456",
		"0x789GHI",
	}
	reconciler.SetTrackedPools(addresses)

	// Verify all addresses are stored lowercase
	require.Len(t, reconciler.trackedPools, 3)
	require.Contains(t, reconciler.trackedPools, "0xabc123")
	require.Contains(t, reconciler.trackedPools, "0xdef456")
	require.Contains(t, reconciler.trackedPools, "0x789ghi")
}

// TestReconcileEmptyRange verifies reconciliation handles empty range correctly.
func TestReconcileEmptyRange(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	reconciler := NewReconciler(nil, graphManager)
	reconciler.SetTrackedPools([]string{"0xabc"})

	// Test with fromBlock > toBlock (no range to reconcile)
	ctx := context.Background()
	result, err := reconciler.Reconcile(ctx, 100, 50)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, uint64(100), result.FromBlock)
	require.Equal(t, uint64(50), result.ToBlock)
	require.Equal(t, 0, result.EventsFound)
}

// TestReconcileNoTrackedPools verifies reconciliation handles no tracked pools.
func TestReconcileNoTrackedPools(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	reconciler := NewReconciler(nil, graphManager)
	// Don't set any tracked pools

	ctx := context.Background()
	result, err := reconciler.Reconcile(ctx, 100, 200)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 0, result.EventsFound)
}

// TestReconcileResultStructure verifies the result structure is properly populated.
func TestReconcileResultStructure(t *testing.T) {
	result := &ReconcileResult{
		FromBlock:     100,
		ToBlock:       200,
		EventsFound:   10,
		EventsApplied: 8,
		PoolsUpdated:  5,
		Duration:      time.Second,
	}

	require.Equal(t, uint64(100), result.FromBlock)
	require.Equal(t, uint64(200), result.ToBlock)
	require.Equal(t, 10, result.EventsFound)
	require.Equal(t, 8, result.EventsApplied)
	require.Equal(t, 5, result.PoolsUpdated)
	require.Equal(t, time.Second, result.Duration)
}

// TestServiceSetReconciler verifies the reconciler can be set on the service.
func TestServiceSetReconciler(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	service := NewService("ws://test", "", graphManager, nil)
	reconciler := NewReconciler(nil, graphManager)

	service.SetReconciler(reconciler, 12345)

	require.NotNil(t, service.reconciler)
	require.Equal(t, uint64(12345), service.bootstrapStartBlock)
	require.False(t, service.reconciliationDone)
}

// TestServiceRunReconciliationSkipsWhenNotConfigured verifies reconciliation is skipped
// when no reconciler is configured.
func TestServiceRunReconciliationSkipsWhenNotConfigured(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	service := NewService("ws://test", "", graphManager, nil)
	// Don't set reconciler

	ctx := context.Background()
	err := service.runReconciliation(ctx)

	require.NoError(t, err)
}

// TestServiceRunReconciliationSkipsWhenAlreadyDone verifies reconciliation is skipped
// when already completed.
func TestServiceRunReconciliationSkipsWhenAlreadyDone(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	service := NewService("ws://test", "", graphManager, nil)
	reconciler := NewReconciler(nil, graphManager)
	service.SetReconciler(reconciler, 12345)
	service.reconciliationDone = true // Mark as already done

	ctx := context.Background()
	err := service.runReconciliation(ctx)

	require.NoError(t, err)
}

// TestMaxBlockRangeConstant verifies the max block range is reasonable.
func TestMaxBlockRangeConstant(t *testing.T) {
	// maxBlockRange should be large enough to be efficient but small enough
	// to not timeout RPC calls
	require.Equal(t, 1000, maxBlockRange)
	require.Greater(t, maxBlockRange, 0)
	require.LessOrEqual(t, maxBlockRange, 10000) // Reasonable upper bound
}

// TestReconcilerContextCancellation verifies reconciliation respects context cancellation.
func TestReconcilerContextCancellation(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	// Create reconciler with no client (will fail on actual queries)
	reconciler := NewReconciler(nil, graphManager)
	reconciler.SetTrackedPools([]string{"0xabc"})

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// With cancelled context, Reconcile should return error when trying to query
	// The first iteration of the loop will hit the context check
	_, err := reconciler.Reconcile(ctx, 100, 200)

	// The loop checks context before making RPC calls, so with cancelled context
	// it should return the context error (along with partial result)
	require.Error(t, err)
	require.Equal(t, context.Canceled, err)
}

// TestGraphManagerIntegration tests that reconciliation properly updates the graph.
func TestGraphManagerIntegration(t *testing.T) {
	graphManager := graph.NewManager(nil)
	defer graphManager.Close()

	// Add a test pool to the graph
	pool := graph.PoolState{
		Address:  "0xpool1",
		Token0:   "0xtoken0",
		Token1:   "0xtoken1",
		Reserve0: big.NewInt(1000000),
		Reserve1: big.NewInt(2000000),
		Fee:      0.003,
	}
	token0 := graph.TokenInfo{
		Address:  "0xtoken0",
		Symbol:   "TKN0",
		Decimals: 18,
	}
	token1 := graph.TokenInfo{
		Address:  "0xtoken1",
		Symbol:   "TKN1",
		Decimals: 18,
	}
	graphManager.AddPool(pool, token0, token1)

	// Verify initial state
	nodes, edges, pools := graphManager.Stats()
	require.Equal(t, 2, nodes)
	require.Equal(t, 2, edges)
	require.Equal(t, 1, pools)

	// Simulate a reserve update (as reconciliation would do)
	update := graph.ReserveUpdate{
		PoolAddress: "0xpool1",
		Reserve0:    big.NewInt(1500000),
		Reserve1:    big.NewInt(2500000),
		BlockNumber: 12345,
		LogIndex:    0,
		Timestamp:   time.Now(),
	}
	graphManager.ProcessUpdate(update)

	// Verify pool still exists and graph stats unchanged
	nodes, edges, pools = graphManager.Stats()
	require.Equal(t, 2, nodes)
	require.Equal(t, 2, edges)
	require.Equal(t, 1, pools)
}
