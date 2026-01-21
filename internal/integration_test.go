package internal

import (
	"context"
	"encoding/json"
	"math/big"
	"sync"
	"testing"
	"time"

	"watcher/internal/detector"
	"watcher/internal/graph"
)

// TestEventFlowIntegration tests the complete event flow:
// 1. Reserve update -> Graph Manager -> Snapshot creation -> Detection
func TestEventFlowIntegration(t *testing.T) {
	// Create graph manager
	gm := graph.NewManager(nil)
	defer gm.Close()

	// Add tokens for start token (WETH)
	wethAddr := "0x4200000000000000000000000000000000000006"
	usdcAddr := "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	daiAddr := "0x50c5725949a6f0c72e6c4a641f24049a917db0cb"

	// Add pools to the graph that form a potential arbitrage path
	pools := []graph.PoolState{
		{
			Address:  "0xpool1",
			Token0:   wethAddr,
			Token1:   usdcAddr,
			Reserve0: bigInt("1000000000000000000"),   // 1 WETH
			Reserve1: bigInt("3000000000"),            // 3000 USDC
			Fee:      0.003,
		},
		{
			Address:  "0xpool2",
			Token0:   usdcAddr,
			Token1:   daiAddr,
			Reserve0: bigInt("10000000000"),           // 10000 USDC
			Reserve1: bigInt("10000000000000000000000"), // 10000 DAI
			Fee:      0.003,
		},
		{
			Address:  "0xpool3",
			Token0:   daiAddr,
			Token1:   wethAddr,
			Reserve0: bigInt("3000000000000000000000"), // 3000 DAI
			Reserve1: bigInt("1000000000000000000"),    // 1 WETH
			Fee:      0.003,
		},
	}

	tokenInfos := map[string]graph.TokenInfo{
		wethAddr: {Address: wethAddr, Symbol: "WETH", Decimals: 18},
		usdcAddr: {Address: usdcAddr, Symbol: "USDC", Decimals: 6},
		daiAddr:  {Address: daiAddr, Symbol: "DAI", Decimals: 18},
	}

	gm.AddPoolBatch(pools, tokenInfos)

	// Create detector
	cfg := detector.Config{
		MinProfitFactor: 1.0001,
		MaxPathLength:   6,
		NumWorkers:      1,
		StartTokens:     []string{wethAddr},
	}
	det := detector.NewDetector(cfg, gm.SnapshotCh(), nil)

	// Start detector in background
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var detectedOpps []*detector.Opportunity
	var detMu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case opp, ok := <-det.Opportunities():
				if !ok {
					return
				}
				detMu.Lock()
				detectedOpps = append(detectedOpps, opp)
				detMu.Unlock()
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		det.Run(ctx)
	}()

	// Simulate a reserve update (like a Sync event would trigger)
	update := graph.ReserveUpdate{
		PoolAddress: "0xpool1",
		Reserve0:    bigInt("1000000000000000000"), // Same reserves
		Reserve1:    bigInt("3000000000"),
		BlockNumber: 1,
		LogIndex:    0,
		Timestamp:   time.Now(),
	}

	// Process the update - this should trigger a snapshot after flush delay
	gm.ProcessUpdate(update)

	// Wait for flush and detection
	time.Sleep(3 * time.Second)

	// Force flush
	gm.Flush()

	// Wait a bit more for detection
	time.Sleep(500 * time.Millisecond)

	// Cancel context to stop detector
	cancel()
	wg.Wait()

	// Verify that the pipeline was exercised
	// Note: actual profitable opportunities depend on reserve ratios
	t.Logf("Detected %d opportunities", len(detectedOpps))
}

// TestSnapshotTriggersDetection verifies that reserve updates trigger detection.
func TestSnapshotTriggersDetection(t *testing.T) {
	// Create graph manager
	gm := graph.NewManager(nil)
	defer gm.Close()

	// Add a simple graph
	wethAddr := "0x0001"
	usdcAddr := "0x0002"

	tokenInfos := map[string]graph.TokenInfo{
		wethAddr: {Address: wethAddr, Symbol: "WETH", Decimals: 18},
		usdcAddr: {Address: usdcAddr, Symbol: "USDC", Decimals: 6},
	}

	pools := []graph.PoolState{
		{
			Address:  "0xpool1",
			Token0:   wethAddr,
			Token1:   usdcAddr,
			Reserve0: bigInt("1000000000000000000"),
			Reserve1: bigInt("3000000000"),
			Fee:      0.003,
		},
	}

	gm.AddPoolBatch(pools, tokenInfos)

	// Count snapshots received
	snapshotCount := 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-gm.SnapshotCh():
				if !ok {
					return
				}
				snapshotCount++
			}
		}
	}()

	// Send first update - should trigger flush after delay
	gm.ProcessUpdate(graph.ReserveUpdate{
		PoolAddress: "0xpool1",
		Reserve0:    bigInt("1100000000000000000"), // Changed
		Reserve1:    bigInt("3000000000"),
		BlockNumber: 1,
		LogIndex:    0,
		Timestamp:   time.Now(),
	})

	// Wait for flush
	time.Sleep(3 * time.Second)

	if snapshotCount == 0 {
		t.Error("Expected at least one snapshot to be created after reserve update")
	}
	t.Logf("Received %d snapshots", snapshotCount)
}

// TestManagerFlushOnNewBlock verifies that flush happens when a new block arrives.
func TestManagerFlushOnNewBlock(t *testing.T) {
	gm := graph.NewManager(nil)
	defer gm.Close()

	wethAddr := "0x0001"
	usdcAddr := "0x0002"

	tokenInfos := map[string]graph.TokenInfo{
		wethAddr: {Address: wethAddr, Symbol: "WETH", Decimals: 18},
		usdcAddr: {Address: usdcAddr, Symbol: "USDC", Decimals: 6},
	}

	pools := []graph.PoolState{
		{
			Address:  "0xpool1",
			Token0:   wethAddr,
			Token1:   usdcAddr,
			Reserve0: bigInt("1000000000000000000"),
			Reserve1: bigInt("3000000000"),
			Fee:      0.003,
		},
	}

	gm.AddPoolBatch(pools, tokenInfos)

	snapshotCount := 0
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-gm.SnapshotCh():
				if !ok {
					return
				}
				snapshotCount++
			}
		}
	}()

	// Send update for block 1
	gm.ProcessUpdate(graph.ReserveUpdate{
		PoolAddress: "0xpool1",
		Reserve0:    bigInt("1100000000000000000"),
		Reserve1:    bigInt("3000000000"),
		BlockNumber: 1,
		Timestamp:   time.Now(),
	})

	// Send update for block 2 - should trigger flush of block 1
	gm.ProcessUpdate(graph.ReserveUpdate{
		PoolAddress: "0xpool1",
		Reserve0:    bigInt("1200000000000000000"),
		Reserve1:    bigInt("3000000000"),
		BlockNumber: 2,
		Timestamp:   time.Now(),
	})

	// Wait briefly for processing
	time.Sleep(100 * time.Millisecond)

	// The arrival of block 2 should have flushed block 1
	if snapshotCount != 1 {
		t.Errorf("Expected 1 snapshot (for block 1), got %d", snapshotCount)
	}
}

// TestMockSyncEventProcessing simulates processing a Sync event message.
func TestMockSyncEventProcessing(t *testing.T) {
	// Simulate parsing a Sync event like the ingestion service does
	type LogEntry struct {
		Address         string   `json:"address"`
		Topics          []string `json:"topics"`
		Data            string   `json:"data"`
		BlockNumber     string   `json:"blockNumber"`
		TransactionHash string   `json:"transactionHash"`
		LogIndex        string   `json:"logIndex"`
		Removed         bool     `json:"removed"`
	}

	// Mock Sync event (topic 0x1c411e...)
	syncEvent := LogEntry{
		Address: "0x1234567890123456789012345678901234567890",
		Topics: []string{
			"0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1",
		},
		Data: "0x" +
			"00000000000000000000000000000000000000000000000000000001234567890" + // reserve0
			"00000000000000000000000000000000000000000000000000000000987654321", // reserve1
		BlockNumber:     "0x1234",
		TransactionHash: "0xabcd",
		LogIndex:        "0x0",
		Removed:         false,
	}

	// Serialize and deserialize (like WebSocket would)
	jsonData, err := json.Marshal(syncEvent)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed LogEntry
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify we can extract the address and topic
	if parsed.Address != syncEvent.Address {
		t.Errorf("Address mismatch")
	}
	if len(parsed.Topics) != 1 || parsed.Topics[0] != syncEvent.Topics[0] {
		t.Errorf("Topics mismatch")
	}
	if parsed.Removed {
		t.Errorf("Should not be removed")
	}
}

func bigInt(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}
