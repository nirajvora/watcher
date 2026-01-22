package graph

import (
	"math/big"
	"testing"
)

// bigInt creates a big.Int from a string for test convenience
func bigInt(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}

func TestAddToken(t *testing.T) {
	g := NewGraph()

	token := TokenInfo{
		Address:  "0x0001",
		Symbol:   "TEST",
		Decimals: 18,
	}

	idx := g.AddToken(token)
	if idx != 0 {
		t.Errorf("Expected index 0, got %d", idx)
	}

	// Adding the same token should return the same index
	idx2 := g.AddToken(token)
	if idx2 != 0 {
		t.Errorf("Expected same index 0 for duplicate, got %d", idx2)
	}

	// Check token retrieval
	retrieved, ok := g.GetToken("0x0001")
	if !ok {
		t.Error("Expected to find token")
	}
	if retrieved.Symbol != "TEST" {
		t.Errorf("Expected symbol TEST, got %s", retrieved.Symbol)
	}

	if g.NumNodes() != 1 {
		t.Errorf("Expected 1 node, got %d", g.NumNodes())
	}
}

func TestAddPool(t *testing.T) {
	g := NewGraph()

	// Add tokens first
	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})

	pool := PoolState{
		Address:  "0xpool1",
		Token0:   "0x0001",
		Token1:   "0x0002",
		Reserve0: bigInt("1000000000000000000"),
		Reserve1: bigInt("2000000000000000000"),
		Fee:      0.003,
	}

	g.AddPool(pool)

	// Check pool was added
	if g.NumPools() != 1 {
		t.Errorf("Expected 1 pool, got %d", g.NumPools())
	}

	// Check bidirectional edges were created
	if g.NumEdges() != 2 {
		t.Errorf("Expected 2 edges (bidirectional), got %d", g.NumEdges())
	}

	// Check pool retrieval
	retrieved, ok := g.GetPool("0xpool1")
	if !ok {
		t.Error("Expected to find pool")
	}
	if retrieved.Reserve0.Cmp(pool.Reserve0) != 0 {
		t.Error("Reserve0 mismatch")
	}

	// Check forward edge exists
	edges := g.GetEdgesFrom(0)
	if len(edges) != 1 {
		t.Errorf("Expected 1 outgoing edge from token0, got %d", len(edges))
	}
	if edges[0].To != 1 {
		t.Errorf("Expected edge to token1 (index 1), got %d", edges[0].To)
	}
}

func TestUpdateReserves(t *testing.T) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})

	pool := PoolState{
		Address:  "0xpool1",
		Token0:   "0x0001",
		Token1:   "0x0002",
		Reserve0: bigInt("1000000000000000000"),
		Reserve1: bigInt("2000000000000000000"),
		Fee:      0.003,
	}
	g.AddPool(pool)

	// Get original edge weight
	edges := g.GetEdgesFrom(0)
	originalWeight := edges[0].Weight

	// Update reserves
	newReserve0 := bigInt("2000000000000000000")
	newReserve1 := bigInt("1000000000000000000")
	updated := g.UpdateReserves("0xpool1", newReserve0, newReserve1)

	if !updated {
		t.Error("Expected UpdateReserves to return true")
	}

	// Check that edge weight changed
	edges = g.GetEdgesFrom(0)
	if edges[0].Weight == originalWeight {
		t.Error("Expected edge weight to change after reserve update")
	}

	// Check pool state was updated
	pool2, _ := g.GetPool("0xpool1")
	if pool2.Reserve0.Cmp(newReserve0) != 0 {
		t.Error("Expected pool reserve0 to be updated")
	}
}

func TestUpdateNonExistentPool(t *testing.T) {
	g := NewGraph()

	updated := g.UpdateReserves("0xnonexistent", big.NewInt(1), big.NewInt(1))
	if updated {
		t.Error("Expected UpdateReserves to return false for non-existent pool")
	}
}

func TestSnapshotCreation(t *testing.T) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})

	pool := PoolState{
		Address:  "0xpool1",
		Token0:   "0x0001",
		Token1:   "0x0002",
		Reserve0: bigInt("1000000000000000000"),
		Reserve1: bigInt("2000000000000000000"),
		Fee:      0.003,
	}
	g.AddPool(pool)

	snap := g.CreateSnapshot(100)

	if snap.BlockNumber != 100 {
		t.Errorf("Expected block number 100, got %d", snap.BlockNumber)
	}

	if snap.NumNodes() != 2 {
		t.Errorf("Expected 2 nodes, got %d", snap.NumNodes())
	}

	if snap.NumEdges() != 2 {
		t.Errorf("Expected 2 edges, got %d", snap.NumEdges())
	}
}

func TestSnapshotImmutability(t *testing.T) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})

	pool := PoolState{
		Address:  "0xpool1",
		Token0:   "0x0001",
		Token1:   "0x0002",
		Reserve0: bigInt("1000000000000000000"),
		Reserve1: bigInt("2000000000000000000"),
		Fee:      0.003,
	}
	g.AddPool(pool)

	// Create snapshot
	snap := g.CreateSnapshot(1)
	originalEdgeWeight := snap.GetEdgesFrom(0)[0].Weight

	// Modify the original graph
	g.UpdateReserves("0xpool1", bigInt("5000000000000000000"), bigInt("1000000000000000000"))

	// Snapshot should be unchanged
	newEdgeWeight := snap.GetEdgesFrom(0)[0].Weight
	if newEdgeWeight != originalEdgeWeight {
		t.Errorf("Snapshot was mutated: weight changed from %f to %f", originalEdgeWeight, newEdgeWeight)
	}
}

func TestGraphValidation(t *testing.T) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})

	pool := PoolState{
		Address:  "0xpool1",
		Token0:   "0x0001",
		Token1:   "0x0002",
		Reserve0: bigInt("1000000000000000000"),
		Reserve1: bigInt("2000000000000000000"),
		Fee:      0.003,
	}
	g.AddPool(pool)

	result := g.Validate()
	if !result.Valid {
		t.Errorf("Expected valid graph, got errors: %v", result.Errors)
	}

	if len(result.MissingTokens) > 0 {
		t.Errorf("Expected no missing tokens, got %v", result.MissingTokens)
	}

	if len(result.MissingPoolEdges) > 0 {
		t.Errorf("Expected no missing pool edges, got %v", result.MissingPoolEdges)
	}
}

func TestWeightCalculationVariousRatios(t *testing.T) {
	tests := []struct {
		name      string
		reserve0  *big.Int
		reserve1  *big.Int
		fee       float64
		expectNeg bool
	}{
		{
			name:      "1:1 ratio",
			reserve0:  bigInt("1000000000000000000"),
			reserve1:  bigInt("1000000000000000000"),
			fee:       0.003,
			expectNeg: false, // 1:1 - fee = loss
		},
		{
			name:      "1:2 ratio - favorable",
			reserve0:  bigInt("1000000000000000000"),
			reserve1:  bigInt("2000000000000000000"),
			fee:       0.003,
			expectNeg: true, // 2x output - fee is gain
		},
		{
			name:      "2:1 ratio - unfavorable",
			reserve0:  bigInt("2000000000000000000"),
			reserve1:  bigInt("1000000000000000000"),
			fee:       0.003,
			expectNeg: false, // 0.5x output is loss
		},
		{
			name:      "1:1.5 ratio",
			reserve0:  bigInt("1000000000000000000"),
			reserve1:  bigInt("1500000000000000000"),
			fee:       0.003,
			expectNeg: true, // 1.5x output - fee is gain
		},
		{
			name:      "zero fee",
			reserve0:  bigInt("1000000000000000000"),
			reserve1:  bigInt("1000000000000000000"),
			fee:       0.0,
			expectNeg: false, // 1:1 ratio with no fee = break even
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := CalculateWeight(tt.reserve0, tt.reserve1, tt.fee)

			if tt.expectNeg && weight > 0 {
				t.Errorf("Expected negative or zero weight, got %f", weight)
			}
			if !tt.expectNeg && weight < 0 {
				t.Errorf("Expected positive or zero weight, got %f", weight)
			}
		})
	}
}

func TestHasPool(t *testing.T) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})

	pool := PoolState{
		Address:  "0xpool1",
		Token0:   "0x0001",
		Token1:   "0x0002",
		Reserve0: bigInt("1000000000000000000"),
		Reserve1: bigInt("2000000000000000000"),
		Fee:      0.003,
	}
	g.AddPool(pool)

	if !g.HasPool("0xpool1") {
		t.Error("Expected HasPool to return true for existing pool")
	}

	if g.HasPool("0xnonexistent") {
		t.Error("Expected HasPool to return false for non-existent pool")
	}
}

func TestGetAllPoolAddresses(t *testing.T) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0003", Symbol: "TOK2", Decimals: 18})

	g.AddPool(PoolState{
		Address: "0xpool1", Token0: "0x0001", Token1: "0x0002",
		Reserve0: bigInt("1000000000000000000"), Reserve1: bigInt("2000000000000000000"), Fee: 0.003,
	})
	g.AddPool(PoolState{
		Address: "0xpool2", Token0: "0x0002", Token1: "0x0003",
		Reserve0: bigInt("1000000000000000000"), Reserve1: bigInt("2000000000000000000"), Fee: 0.003,
	})

	addresses := g.GetAllPoolAddresses()
	if len(addresses) != 2 {
		t.Errorf("Expected 2 pool addresses, got %d", len(addresses))
	}

	// Check that both addresses are present
	found := map[string]bool{}
	for _, addr := range addresses {
		found[addr] = true
	}
	if !found["0xpool1"] || !found["0xpool2"] {
		t.Error("Expected to find both pool addresses")
	}
}

func TestGetTokenIndex(t *testing.T) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})

	idx, exists := g.GetTokenIndex("0x0001")
	if !exists {
		t.Error("Expected token to exist")
	}
	if idx != 0 {
		t.Errorf("Expected index 0, got %d", idx)
	}

	_, exists = g.GetTokenIndex("0xnonexistent")
	if exists {
		t.Error("Expected token to not exist")
	}
}

func BenchmarkAddPool(b *testing.B) {
	g := NewGraph()

	// Add initial tokens
	for i := 0; i < 100; i++ {
		g.AddToken(TokenInfo{
			Address:  "0x" + string(rune(i)),
			Symbol:   "TOK" + string(rune(i)),
			Decimals: 18,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.AddPool(PoolState{
			Address:  "0xpool" + string(rune(i)),
			Token0:   "0x" + string(rune(i%100)),
			Token1:   "0x" + string(rune((i+1)%100)),
			Reserve0: big.NewInt(1e18),
			Reserve1: big.NewInt(2e18),
			Fee:      0.003,
		})
	}
}

func BenchmarkUpdateReserves(b *testing.B) {
	g := NewGraph()

	g.AddToken(TokenInfo{Address: "0x0001", Symbol: "TOK0", Decimals: 18})
	g.AddToken(TokenInfo{Address: "0x0002", Symbol: "TOK1", Decimals: 18})
	g.AddPool(PoolState{
		Address: "0xpool1", Token0: "0x0001", Token1: "0x0002",
		Reserve0: big.NewInt(1e18), Reserve1: big.NewInt(2e18), Fee: 0.003,
	})

	newR0 := big.NewInt(3e18)
	newR1 := big.NewInt(4e18)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.UpdateReserves("0xpool1", newR0, newR1)
	}
}

func BenchmarkCreateSnapshot(b *testing.B) {
	g := NewGraph()

	// Create a realistic graph
	for i := 0; i < 300; i++ {
		g.AddToken(TokenInfo{
			Address:  "0x" + string(rune(i)),
			Symbol:   "TOK" + string(rune(i)),
			Decimals: 18,
		})
	}

	for i := 0; i < 500; i++ {
		g.AddPool(PoolState{
			Address:  "0xpool" + string(rune(i)),
			Token0:   "0x" + string(rune(i%300)),
			Token1:   "0x" + string(rune((i+1)%300)),
			Reserve0: big.NewInt(1e18),
			Reserve1: big.NewInt(2e18),
			Fee:      0.003,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.CreateSnapshot(uint64(i))
	}
}
