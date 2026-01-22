package graph

import (
	"math/big"
	"time"
)

// Snapshot represents an immutable point-in-time view of the graph.
// Used for concurrent detection while new events are being processed.
type Snapshot struct {
	// Token data
	Tokens     []TokenInfo
	TokenIndex map[string]int

	// Adjacency list (immutable copy)
	Adjacency [][]Edge

	// Pool states (immutable copies)
	Pools map[string]PoolState

	// Metadata
	BlockNumber uint64
	CreatedAt   time.Time
}

// CreateSnapshot creates an immutable snapshot of the current graph state.
// This is a shallow copy for tokens and deep copy for edges and pools.
func (g *Graph) CreateSnapshot(blockNumber uint64) *Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()

	snap := &Snapshot{
		Tokens:      make([]TokenInfo, len(g.tokens)),
		TokenIndex:  make(map[string]int, len(g.tokenIndex)),
		Adjacency:   make([][]Edge, len(g.adjacency)),
		Pools:       make(map[string]PoolState, len(g.pools)),
		BlockNumber: blockNumber,
		CreatedAt:   time.Now(),
	}

	// Copy tokens (shallow copy is fine - they're immutable)
	copy(snap.Tokens, g.tokens)

	// Copy token index
	for k, v := range g.tokenIndex {
		snap.TokenIndex[k] = v
	}

	// Deep copy adjacency list
	for i, edges := range g.adjacency {
		snap.Adjacency[i] = make([]Edge, len(edges))
		for j, edge := range edges {
			snap.Adjacency[i][j] = Edge{
				From:       edge.From,
				To:         edge.To,
				Weight:     edge.Weight,
				PoolAddr:   edge.PoolAddr,
				Reserve0:   new(big.Int).Set(edge.Reserve0),
				Reserve1:   new(big.Int).Set(edge.Reserve1),
				Fee:        edge.Fee,
				IsReversed: edge.IsReversed,
			}
		}
	}

	// Deep copy pool states
	for addr, pool := range g.pools {
		snap.Pools[addr] = PoolState{
			Address:  pool.Address,
			Token0:   pool.Token0,
			Token1:   pool.Token1,
			Reserve0: new(big.Int).Set(pool.Reserve0),
			Reserve1: new(big.Int).Set(pool.Reserve1),
			Fee:      pool.Fee,
		}
	}

	return snap
}

// NumNodes returns the number of nodes in the snapshot.
func (s *Snapshot) NumNodes() int {
	return len(s.Tokens)
}

// NumEdges returns the number of directed edges in the snapshot.
func (s *Snapshot) NumEdges() int {
	count := 0
	for _, edges := range s.Adjacency {
		count += len(edges)
	}
	return count
}

// NumPools returns the number of pools in the snapshot.
func (s *Snapshot) NumPools() int {
	return len(s.Pools)
}

// GetToken returns token info by index.
func (s *Snapshot) GetToken(idx int) (TokenInfo, bool) {
	if idx < 0 || idx >= len(s.Tokens) {
		return TokenInfo{}, false
	}
	return s.Tokens[idx], true
}

// GetTokenIndex returns the index for a token address.
func (s *Snapshot) GetTokenIndex(address string) (int, bool) {
	idx, exists := s.TokenIndex[address]
	return idx, exists
}

// GetEdgesFrom returns all edges from a given node.
func (s *Snapshot) GetEdgesFrom(nodeIdx int) []Edge {
	if nodeIdx < 0 || nodeIdx >= len(s.Adjacency) {
		return nil
	}
	return s.Adjacency[nodeIdx]
}

// GetPool returns pool state by address.
func (s *Snapshot) GetPool(address string) (PoolState, bool) {
	pool, exists := s.Pools[address]
	return pool, exists
}

// GetAllEdges returns all edges in the snapshot as a flat slice.
func (s *Snapshot) GetAllEdges() []Edge {
	var all []Edge
	for _, edges := range s.Adjacency {
		all = append(all, edges...)
	}
	return all
}
