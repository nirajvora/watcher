package graph

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// ValidationResult holds the results of a graph consistency check.
type ValidationResult struct {
	Valid            bool
	Errors           []string
	MissingTokens    []string // Pool tokens that don't exist in token list
	OrphanTokens     []string // Tokens with zero edges
	MissingPoolEdges []string // Pools without bidirectional edges
	EdgePoolMismatch []string // Edges referencing non-existent pools
}

// Validate performs a comprehensive consistency check on the graph.
// Returns a ValidationResult with details about any inconsistencies.
func (g *Graph) Validate() *ValidationResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := &ValidationResult{
		Valid:            true,
		Errors:           make([]string, 0),
		MissingTokens:    make([]string, 0),
		OrphanTokens:     make([]string, 0),
		MissingPoolEdges: make([]string, 0),
		EdgePoolMismatch: make([]string, 0),
	}

	// Check 1: Every pool's tokens exist in token node list
	for addr, pool := range g.pools {
		if _, exists := g.tokenIndex[pool.Token0]; !exists {
			result.Valid = false
			result.MissingTokens = append(result.MissingTokens, pool.Token0)
			result.Errors = append(result.Errors,
				fmt.Sprintf("pool %s references non-existent token0: %s", addr, pool.Token0))
		}
		if _, exists := g.tokenIndex[pool.Token1]; !exists {
			result.Valid = false
			result.MissingTokens = append(result.MissingTokens, pool.Token1)
			result.Errors = append(result.Errors,
				fmt.Sprintf("pool %s references non-existent token1: %s", addr, pool.Token1))
		}
	}

	// Check 2: Every edge has matching pool in pool list
	edgePools := make(map[string]bool)
	for fromIdx, edges := range g.adjacency {
		for _, edge := range edges {
			edgePools[edge.PoolAddr] = true
			if _, exists := g.pools[edge.PoolAddr]; !exists {
				result.Valid = false
				result.EdgePoolMismatch = append(result.EdgePoolMismatch, edge.PoolAddr)
				result.Errors = append(result.Errors,
					fmt.Sprintf("edge from token %d to %d references non-existent pool: %s",
						fromIdx, edge.To, edge.PoolAddr))
			}
		}
	}

	// Check 3: No orphan tokens (tokens with zero edges)
	for idx, token := range g.tokens {
		hasOutgoing := len(g.adjacency[idx]) > 0
		hasIncoming := false

		// Check if any other node has an edge to this token
		for _, edges := range g.adjacency {
			for _, edge := range edges {
				if edge.To == idx {
					hasIncoming = true
					break
				}
			}
			if hasIncoming {
				break
			}
		}

		if !hasOutgoing && !hasIncoming {
			result.OrphanTokens = append(result.OrphanTokens, token.Address)
			// Note: orphan tokens are a warning, not an error that makes the graph invalid
		}
	}

	// Check 4: Bidirectional edges exist for every pool
	for addr, pool := range g.pools {
		idx0, exists0 := g.tokenIndex[pool.Token0]
		idx1, exists1 := g.tokenIndex[pool.Token1]

		if !exists0 || !exists1 {
			continue // Already reported as missing token
		}

		// Check for forward edge (token0 -> token1)
		hasForward := false
		for _, edge := range g.adjacency[idx0] {
			if edge.PoolAddr == addr && edge.To == idx1 {
				hasForward = true
				break
			}
		}

		// Check for reverse edge (token1 -> token0)
		hasReverse := false
		for _, edge := range g.adjacency[idx1] {
			if edge.PoolAddr == addr && edge.To == idx0 {
				hasReverse = true
				break
			}
		}

		if !hasForward || !hasReverse {
			result.Valid = false
			result.MissingPoolEdges = append(result.MissingPoolEdges, addr)
			if !hasForward {
				result.Errors = append(result.Errors,
					fmt.Sprintf("pool %s missing forward edge (token0 -> token1)", addr))
			}
			if !hasReverse {
				result.Errors = append(result.Errors,
					fmt.Sprintf("pool %s missing reverse edge (token1 -> token0)", addr))
			}
		}
	}

	return result
}

// ValidateAndLog performs validation and logs the results.
// Returns true if the graph is valid, false otherwise.
func (g *Graph) ValidateAndLog() bool {
	result := g.Validate()

	if result.Valid {
		log.Info().
			Int("tokens", g.NumNodes()).
			Int("edges", g.NumEdges()).
			Int("pools", g.NumPools()).
			Msg("Graph validation passed")
		return true
	}

	// Log errors
	for _, err := range result.Errors {
		log.Error().Msg("Graph validation error: " + err)
	}

	// Log warnings for orphan tokens
	if len(result.OrphanTokens) > 0 {
		log.Warn().
			Int("count", len(result.OrphanTokens)).
			Strs("tokens", truncateSlice(result.OrphanTokens, 5)).
			Msg("Graph has orphan tokens (no edges)")
	}

	log.Error().
		Int("error_count", len(result.Errors)).
		Int("missing_tokens", len(result.MissingTokens)).
		Int("edge_pool_mismatch", len(result.EdgePoolMismatch)).
		Int("missing_pool_edges", len(result.MissingPoolEdges)).
		Msg("Graph validation FAILED")

	return false
}

// truncateSlice returns at most n elements from the slice for logging.
func truncateSlice(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ValidateSnapshot performs validation on a snapshot.
func ValidateSnapshot(snap *Snapshot) *ValidationResult {
	result := &ValidationResult{
		Valid:            true,
		Errors:           make([]string, 0),
		MissingTokens:    make([]string, 0),
		OrphanTokens:     make([]string, 0),
		MissingPoolEdges: make([]string, 0),
		EdgePoolMismatch: make([]string, 0),
	}

	// Build edge lookup per pool
	type edgePair struct {
		hasForward bool
		hasReverse bool
	}
	poolEdges := make(map[string]*edgePair)

	for _, edges := range snap.Adjacency {
		for _, edge := range edges {
			pair, exists := poolEdges[edge.PoolAddr]
			if !exists {
				pair = &edgePair{}
				poolEdges[edge.PoolAddr] = pair
			}
			if edge.IsReversed {
				pair.hasReverse = true
			} else {
				pair.hasForward = true
			}
		}
	}

	// Check each pool has both directions
	for poolAddr, pair := range poolEdges {
		if !pair.hasForward || !pair.hasReverse {
			result.Valid = false
			result.MissingPoolEdges = append(result.MissingPoolEdges, poolAddr)
			if !pair.hasForward {
				result.Errors = append(result.Errors,
					fmt.Sprintf("snapshot: pool %s missing forward edge", poolAddr))
			}
			if !pair.hasReverse {
				result.Errors = append(result.Errors,
					fmt.Sprintf("snapshot: pool %s missing reverse edge", poolAddr))
			}
		}
	}

	// Check for orphan tokens
	for idx := 0; idx < snap.NumNodes(); idx++ {
		hasEdges := len(snap.Adjacency[idx]) > 0

		// Check incoming edges
		if !hasEdges {
			for _, edges := range snap.Adjacency {
				for _, edge := range edges {
					if edge.To == idx {
						hasEdges = true
						break
					}
				}
				if hasEdges {
					break
				}
			}
		}

		if !hasEdges {
			if token, ok := snap.GetToken(idx); ok {
				result.OrphanTokens = append(result.OrphanTokens, token.Address)
			}
		}
	}

	return result
}
