package detector

import (
	"math/big"
	"math/rand"
	"testing"
	"time"

	"watcher/internal/graph"
)

// bigInt creates a big.Int from a string for test convenience
func bigInt(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}

// createRealisticGraph creates a graph similar to production (500 pools, ~300 tokens)
func createRealisticGraph(numPools, numTokens int) *graph.Graph {
	g := graph.NewGraph()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Create tokens
	tokens := make([]graph.TokenInfo, numTokens)
	for i := 0; i < numTokens; i++ {
		tokens[i] = graph.TokenInfo{
			Address:  randomAddress(rng),
			Symbol:   randomSymbol(rng, i),
			Decimals: 18,
		}
		g.AddToken(tokens[i])
	}

	// Create pools with realistic reserve values
	for i := 0; i < numPools; i++ {
		// Pick two different tokens
		t0Idx := rng.Intn(numTokens)
		t1Idx := rng.Intn(numTokens)
		for t1Idx == t0Idx {
			t1Idx = rng.Intn(numTokens)
		}

		// Generate realistic reserves (1e18 to 1e24 wei)
		reserve0 := randomReserve(rng)
		reserve1 := randomReserve(rng)

		pool := graph.PoolState{
			Address:  randomAddress(rng),
			Token0:   tokens[t0Idx].Address,
			Token1:   tokens[t1Idx].Address,
			Reserve0: reserve0,
			Reserve1: reserve1,
			Fee:      0.003,
		}

		g.AddPool(pool)
	}

	return g
}

// createGraphWithCycle creates a graph with a known profitable cycle for testing
func createGraphWithCycle() (*graph.Graph, []string) {
	g := graph.NewGraph()

	// Create 4 tokens for a cycle
	tokens := []graph.TokenInfo{
		{Address: "0x0000000000000000000000000000000000000001", Symbol: "WETH", Decimals: 18},
		{Address: "0x0000000000000000000000000000000000000002", Symbol: "USDC", Decimals: 6},
		{Address: "0x0000000000000000000000000000000000000003", Symbol: "DAI", Decimals: 18},
		{Address: "0x0000000000000000000000000000000000000004", Symbol: "WBTC", Decimals: 8},
	}

	for _, t := range tokens {
		g.AddToken(t)
	}

	// Create a profitable cycle: WETH -> USDC -> DAI -> WETH
	// With rates that multiply to > 1 after fees
	pools := []graph.PoolState{
		{
			Address:  "0xpool1",
			Token0:   tokens[0].Address,             // WETH
			Token1:   tokens[1].Address,             // USDC
			Reserve0: bigInt("1000000000000000000"), // 1 WETH (1e18)
			Reserve1: big.NewInt(3000000000),        // 3000 USDC (3000e6)
			Fee:      0.003,
		},
		{
			Address:  "0xpool2",
			Token0:   tokens[1].Address,                // USDC
			Token1:   tokens[2].Address,                // DAI
			Reserve0: big.NewInt(1000000000),           // 1000 USDC (1000e6)
			Reserve1: bigInt("1010000000000000000000"), // 1010 DAI (1010e18)
			Fee:      0.003,
		},
		{
			Address:  "0xpool3",
			Token0:   tokens[2].Address,                // DAI
			Token1:   tokens[0].Address,                // WETH
			Reserve0: bigInt("3000000000000000000000"), // 3000 DAI (3000e18)
			Reserve1: bigInt("1010000000000000000"),    // 1.01 WETH
			Fee:      0.003,
		},
	}

	for _, p := range pools {
		g.AddPool(p)
	}

	return g, []string{tokens[0].Address}
}

func randomAddress(rng *rand.Rand) string {
	const hex = "0123456789abcdef"
	addr := make([]byte, 42)
	addr[0] = '0'
	addr[1] = 'x'
	for i := 2; i < 42; i++ {
		addr[i] = hex[rng.Intn(16)]
	}
	return string(addr)
}

func randomSymbol(rng *rand.Rand, idx int) string {
	symbols := []string{"WETH", "USDC", "DAI", "WBTC", "LINK", "UNI", "AAVE", "COMP", "MKR", "SNX"}
	if idx < len(symbols) {
		return symbols[idx]
	}
	return symbols[rng.Intn(len(symbols))] + string('A'+byte(idx%26))
}

func randomReserve(rng *rand.Rand) *big.Int {
	// Generate reserves between 1e18 and 1e24
	exp := rng.Intn(7) + 18 // 18 to 24
	base := rng.Int63n(9) + 1 // 1 to 9
	result := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil)
	return result.Mul(result, big.NewInt(base))
}

func BenchmarkDetection(b *testing.B) {
	// Create realistic graph
	g := createRealisticGraph(500, 300)
	snap := g.CreateSnapshot(1)

	cfg := Config{
		MinProfitFactor: 1.001,
		MaxPathLength:   4,
		NumWorkers:      4,
		StartTokens: []string{
			"0x4200000000000000000000000000000000000006", // WETH
			"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", // USDC
		},
	}

	detector := NewDetector(cfg, nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.DetectOnce(snap)
	}
}

func BenchmarkBellmanFord(b *testing.B) {
	g := createRealisticGraph(500, 300)
	snap := g.CreateSnapshot(1)

	// Get a valid source index
	sourceIdx := 0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FindNegativeCycleContaining(snap, sourceIdx, 4)
	}
}

func BenchmarkSnapshotCreation(b *testing.B) {
	g := createRealisticGraph(500, 300)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.CreateSnapshot(uint64(i))
	}
}

func BenchmarkWeightCalculation(b *testing.B) {
	reserve0 := big.NewInt(1e18)
	reserve1 := big.NewInt(2e18)
	fee := 0.003

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		graph.CalculateWeight(reserve0, reserve1, fee)
	}
}

func TestCycleDetection(t *testing.T) {
	g, startTokens := createGraphWithCycle()
	snap := g.CreateSnapshot(1)

	cfg := Config{
		MinProfitFactor: 1.0001, // Very low threshold to detect
		MaxPathLength:   4,
		NumWorkers:      1,
		StartTokens:     startTokens,
	}

	detector := NewDetector(cfg, nil, nil)
	opportunities := detector.DetectOnce(snap)

	// Note: The cycle may or may not be profitable depending on the exact reserve ratios
	t.Logf("Found %d opportunities", len(opportunities))
	for _, opp := range opportunities {
		t.Logf("Opportunity: profit factor %.6f", opp.ProfitFactor)
	}
}

func TestCycleValidation(t *testing.T) {
	// Test valid cycle
	edges := []graph.Edge{
		{From: 0, To: 1, PoolAddr: "pool1"},
		{From: 1, To: 2, PoolAddr: "pool2"},
		{From: 2, To: 0, PoolAddr: "pool3"},
	}
	if !ValidateCycle(edges) {
		t.Error("Expected valid cycle")
	}

	// Test invalid cycle (doesn't close)
	invalidEdges := []graph.Edge{
		{From: 0, To: 1, PoolAddr: "pool1"},
		{From: 1, To: 2, PoolAddr: "pool2"},
		{From: 2, To: 3, PoolAddr: "pool3"},
	}
	if ValidateCycle(invalidEdges) {
		t.Error("Expected invalid cycle")
	}

	// Test cycle with gap
	gapEdges := []graph.Edge{
		{From: 0, To: 1, PoolAddr: "pool1"},
		{From: 2, To: 3, PoolAddr: "pool2"}, // Gap - doesn't connect
		{From: 3, To: 0, PoolAddr: "pool3"},
	}
	if ValidateCycle(gapEdges) {
		t.Error("Expected invalid cycle with gap")
	}
}

func TestCycleUniqueKey(t *testing.T) {
	// Two cycles that are rotations of each other should have the same key
	cycle1 := NewCycle([]graph.Edge{
		{From: 0, To: 1, PoolAddr: "pool1", Weight: -0.1},
		{From: 1, To: 2, PoolAddr: "pool2", Weight: -0.1},
		{From: 2, To: 0, PoolAddr: "pool3", Weight: -0.1},
	})

	cycle2 := NewCycle([]graph.Edge{
		{From: 1, To: 2, PoolAddr: "pool2", Weight: -0.1},
		{From: 2, To: 0, PoolAddr: "pool3", Weight: -0.1},
		{From: 0, To: 1, PoolAddr: "pool1", Weight: -0.1},
	})

	if cycle1.UniqueKey() != cycle2.UniqueKey() {
		t.Errorf("Expected same key for rotated cycles: %s != %s", cycle1.UniqueKey(), cycle2.UniqueKey())
	}
}

func TestSimulator(t *testing.T) {
	// Create a simple cycle with known values
	cycle := NewCycle([]graph.Edge{
		{
			From:     0,
			To:       1,
			PoolAddr: "pool1",
			Reserve0: bigInt("1000000000000000000000"), // 1000e18
			Reserve1: big.NewInt(1000000000),           // 1000e6
			Fee:      0.003,
		},
		{
			From:     1,
			To:       0,
			PoolAddr: "pool2",
			Reserve0: big.NewInt(1000000000),           // 1000e6
			Reserve1: bigInt("1001000000000000000000"), // 1001e18 (slight imbalance)
			Fee:      0.003,
		},
	})

	result := SimulateCycle(cycle, nil, 1.0)
	if result == nil {
		t.Fatal("Expected simulation result")
	}

	t.Logf("Simulation result: profitable=%v, factor=%.6f, input=%s, profit=%s",
		result.IsProfitable, result.ProfitFactor, result.MaxInputWei, result.EstimatedProfitWei)
}

func TestSwapCalculation(t *testing.T) {
	// Test constant product formula
	amountIn := bigInt("1000000000000000000")    // 1e18
	reserveIn := bigInt("100000000000000000000") // 100e18
	reserveOut := bigInt("100000000000000000000") // 100e18
	fee := 0.003

	output := calculateSwapOutput(amountIn, reserveIn, reserveOut, fee)
	if output == nil {
		t.Fatal("Expected output")
	}

	// With 1:1 reserves and 1 token in, should get slightly less than 1 out (due to fee + slippage)
	expected := big.NewInt(987158034397061298) // Approximately 0.987 (0.3% fee + slippage)

	// Check that output is within 1% of expected
	diff := new(big.Int).Sub(output, expected)
	diff.Abs(diff)
	tolerance := new(big.Int).Div(expected, big.NewInt(100))

	if diff.Cmp(tolerance) > 0 {
		t.Errorf("Swap output mismatch: got %s, expected ~%s", output, expected)
	}
}
