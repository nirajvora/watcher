package graph

import (
	"math/big"
	"sync"
)

// TokenInfo represents a token in the graph.
type TokenInfo struct {
	Address  string
	Symbol   string
	Decimals int
}

// Edge represents a directed edge in the graph (one direction of a pool).
type Edge struct {
	From       int      // Index of source token
	To         int      // Index of target token
	Weight     float64  // -log(effectiveRate) for Bellman-Ford
	PoolAddr   string   // Pool address
	Reserve0   *big.Int // Source reserve
	Reserve1   *big.Int // Target reserve
	Fee        float64  // Fee rate (e.g., 0.003 for 0.3%)
	IsReversed bool     // True if this is token1->token0 direction
}

// Graph represents the in-memory arbitrage graph.
// Tokens are nodes (indexed 0 to N-1), pools create bidirectional edges.
type Graph struct {
	mu sync.RWMutex

	// Token storage
	tokens      []TokenInfo        // Indexed token list
	tokenIndex  map[string]int     // Address -> index mapping

	// Adjacency list: adjacency[fromIdx] = list of edges from that node
	adjacency [][]Edge

	// Pool tracking
	pools     map[string]*PoolState // Pool address -> state
}

// PoolState represents the current state of a pool.
type PoolState struct {
	Address  string
	Token0   string
	Token1   string
	Reserve0 *big.Int
	Reserve1 *big.Int
	Fee      float64
}

// NewGraph creates a new empty graph.
func NewGraph() *Graph {
	return &Graph{
		tokens:     make([]TokenInfo, 0),
		tokenIndex: make(map[string]int),
		adjacency:  make([][]Edge, 0),
		pools:      make(map[string]*PoolState),
	}
}

// AddToken adds a token to the graph if it doesn't exist.
// Returns the index of the token.
func (g *Graph) AddToken(token TokenInfo) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.addTokenLocked(token)
}

// addTokenLocked adds a token without acquiring the lock.
func (g *Graph) addTokenLocked(token TokenInfo) int {
	if idx, exists := g.tokenIndex[token.Address]; exists {
		return idx
	}

	idx := len(g.tokens)
	g.tokens = append(g.tokens, token)
	g.tokenIndex[token.Address] = idx
	g.adjacency = append(g.adjacency, make([]Edge, 0))

	return idx
}

// AddPool adds or updates a pool in the graph.
// Creates bidirectional edges between the two tokens.
func (g *Graph) AddPool(pool PoolState) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.addPoolLocked(pool)
}

// addPoolLocked adds a pool without acquiring the lock.
func (g *Graph) addPoolLocked(pool PoolState) {
	// Ensure both tokens exist
	idx0, exists0 := g.tokenIndex[pool.Token0]
	if !exists0 {
		idx0 = g.addTokenLocked(TokenInfo{Address: pool.Token0})
	}

	idx1, exists1 := g.tokenIndex[pool.Token1]
	if !exists1 {
		idx1 = g.addTokenLocked(TokenInfo{Address: pool.Token1})
	}

	// Store pool state
	g.pools[pool.Address] = &PoolState{
		Address:  pool.Address,
		Token0:   pool.Token0,
		Token1:   pool.Token1,
		Reserve0: new(big.Int).Set(pool.Reserve0),
		Reserve1: new(big.Int).Set(pool.Reserve1),
		Fee:      pool.Fee,
	}

	// Calculate weights and create edges
	// Forward: token0 -> token1 (swap token0 for token1)
	weight0to1 := CalculateWeight(pool.Reserve0, pool.Reserve1, pool.Fee)

	// Reverse: token1 -> token0 (swap token1 for token0)
	weight1to0 := CalculateWeight(pool.Reserve1, pool.Reserve0, pool.Fee)

	// Update or add edges
	g.updateEdge(idx0, idx1, Edge{
		From:       idx0,
		To:         idx1,
		Weight:     weight0to1,
		PoolAddr:   pool.Address,
		Reserve0:   pool.Reserve0,
		Reserve1:   pool.Reserve1,
		Fee:        pool.Fee,
		IsReversed: false,
	})

	g.updateEdge(idx1, idx0, Edge{
		From:       idx1,
		To:         idx0,
		Weight:     weight1to0,
		PoolAddr:   pool.Address,
		Reserve0:   pool.Reserve1,
		Reserve1:   pool.Reserve0,
		Fee:        pool.Fee,
		IsReversed: true,
	})
}

// updateEdge updates an existing edge or adds a new one.
func (g *Graph) updateEdge(from, to int, edge Edge) {
	// Look for existing edge from the same pool
	for i, e := range g.adjacency[from] {
		if e.PoolAddr == edge.PoolAddr && e.To == to {
			g.adjacency[from][i] = edge
			return
		}
	}

	// Add new edge
	g.adjacency[from] = append(g.adjacency[from], edge)
}

// UpdateReserves updates the reserves for a pool and recalculates edge weights.
func (g *Graph) UpdateReserves(poolAddr string, reserve0, reserve1 *big.Int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	pool, exists := g.pools[poolAddr]
	if !exists {
		return false
	}

	// Update pool state
	pool.Reserve0 = new(big.Int).Set(reserve0)
	pool.Reserve1 = new(big.Int).Set(reserve1)

	// Get token indices
	idx0 := g.tokenIndex[pool.Token0]
	idx1 := g.tokenIndex[pool.Token1]

	// Recalculate weights
	weight0to1 := CalculateWeight(reserve0, reserve1, pool.Fee)
	weight1to0 := CalculateWeight(reserve1, reserve0, pool.Fee)

	// Update edges
	for i, e := range g.adjacency[idx0] {
		if e.PoolAddr == poolAddr && e.To == idx1 {
			g.adjacency[idx0][i].Weight = weight0to1
			g.adjacency[idx0][i].Reserve0 = reserve0
			g.adjacency[idx0][i].Reserve1 = reserve1
			break
		}
	}

	for i, e := range g.adjacency[idx1] {
		if e.PoolAddr == poolAddr && e.To == idx0 {
			g.adjacency[idx1][i].Weight = weight1to0
			g.adjacency[idx1][i].Reserve0 = reserve1
			g.adjacency[idx1][i].Reserve1 = reserve0
			break
		}
	}

	return true
}

// GetToken returns token info by address.
func (g *Graph) GetToken(address string) (TokenInfo, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	idx, exists := g.tokenIndex[address]
	if !exists {
		return TokenInfo{}, false
	}
	return g.tokens[idx], true
}

// GetTokenByIndex returns token info by index.
func (g *Graph) GetTokenByIndex(idx int) (TokenInfo, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if idx < 0 || idx >= len(g.tokens) {
		return TokenInfo{}, false
	}
	return g.tokens[idx], true
}

// GetTokenIndex returns the index for a token address.
func (g *Graph) GetTokenIndex(address string) (int, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	idx, exists := g.tokenIndex[address]
	return idx, exists
}

// GetPool returns pool state by address.
func (g *Graph) GetPool(address string) (*PoolState, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	pool, exists := g.pools[address]
	if !exists {
		return nil, false
	}
	return pool, true
}

// NumNodes returns the number of tokens (nodes) in the graph.
func (g *Graph) NumNodes() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.tokens)
}

// NumEdges returns the total number of directed edges.
func (g *Graph) NumEdges() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	count := 0
	for _, edges := range g.adjacency {
		count += len(edges)
	}
	return count
}

// NumPools returns the number of pools in the graph.
func (g *Graph) NumPools() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.pools)
}

// GetEdgesFrom returns all edges from a given node index.
func (g *Graph) GetEdgesFrom(nodeIdx int) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if nodeIdx < 0 || nodeIdx >= len(g.adjacency) {
		return nil
	}

	// Return a copy to avoid race conditions
	edges := make([]Edge, len(g.adjacency[nodeIdx]))
	copy(edges, g.adjacency[nodeIdx])
	return edges
}

// HasPool checks if a pool exists in the graph.
func (g *Graph) HasPool(address string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, exists := g.pools[address]
	return exists
}

// GetAllPoolAddresses returns all pool addresses in the graph.
func (g *Graph) GetAllPoolAddresses() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	addresses := make([]string, 0, len(g.pools))
	for addr := range g.pools {
		addresses = append(addresses, addr)
	}
	return addresses
}
