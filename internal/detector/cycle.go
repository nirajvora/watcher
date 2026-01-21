package detector

import (
	"fmt"
	"sort"
	"strings"

	"watcher/internal/graph"
)

// Cycle represents an arbitrage cycle (negative cycle in the graph).
type Cycle struct {
	// Edges in the cycle, in traversal order
	Edges []graph.Edge

	// Total weight (sum of edge weights)
	// Negative weight means product of rates > 1 (profitable)
	TotalWeight float64

	// Profit factor: exp(-totalWeight)
	// > 1 means profitable
	ProfitFactor float64
}

// NewCycle creates a new cycle from a list of edges.
func NewCycle(edges []graph.Edge) *Cycle {
	if len(edges) == 0 {
		return nil
	}

	c := &Cycle{
		Edges: make([]graph.Edge, len(edges)),
	}
	copy(c.Edges, edges)

	// Calculate total weight
	for _, e := range edges {
		c.TotalWeight += e.Weight
	}

	// Calculate profit factor
	c.ProfitFactor = graph.CycleProfit(c.TotalWeight)

	return c
}

// Length returns the number of hops in the cycle.
func (c *Cycle) Length() int {
	return len(c.Edges)
}

// StartToken returns the starting token index.
func (c *Cycle) StartToken() int {
	if len(c.Edges) == 0 {
		return -1
	}
	return c.Edges[0].From
}

// PoolAddresses returns the pool addresses in the cycle.
func (c *Cycle) PoolAddresses() []string {
	addrs := make([]string, len(c.Edges))
	for i, e := range c.Edges {
		addrs[i] = e.PoolAddr
	}
	return addrs
}

// TokenIndices returns the token indices in order.
func (c *Cycle) TokenIndices() []int {
	if len(c.Edges) == 0 {
		return nil
	}

	indices := make([]int, len(c.Edges)+1)
	for i, e := range c.Edges {
		indices[i] = e.From
	}
	// Last token should be same as first (cycle)
	indices[len(c.Edges)] = c.Edges[len(c.Edges)-1].To

	return indices
}

// IsProfitable returns true if the cycle is profitable.
func (c *Cycle) IsProfitable(minProfitFactor float64) bool {
	return c.ProfitFactor >= minProfitFactor
}

// UniqueKey returns a unique key for deduplication.
// Normalizes the cycle to start with the smallest token index.
func (c *Cycle) UniqueKey() string {
	if len(c.Edges) == 0 {
		return ""
	}

	// Get token indices
	indices := c.TokenIndices()
	if len(indices) == 0 {
		return ""
	}

	// Remove the last element (duplicate of first)
	indices = indices[:len(indices)-1]

	// Find the minimum index position
	minIdx := 0
	for i := 1; i < len(indices); i++ {
		if indices[i] < indices[minIdx] {
			minIdx = i
		}
	}

	// Rotate to start with minimum
	rotated := make([]int, len(indices))
	for i := 0; i < len(indices); i++ {
		rotated[i] = indices[(minIdx+i)%len(indices)]
	}

	// Create key from rotated indices
	parts := make([]string, len(rotated))
	for i, idx := range rotated {
		parts[i] = fmt.Sprintf("%d", idx)
	}

	return strings.Join(parts, "->")
}

// String returns a human-readable string representation.
func (c *Cycle) String() string {
	if len(c.Edges) == 0 {
		return "empty cycle"
	}

	parts := make([]string, len(c.Edges)+1)
	for i, e := range c.Edges {
		parts[i] = fmt.Sprintf("%d", e.From)
	}
	parts[len(c.Edges)] = fmt.Sprintf("%d", c.Edges[len(c.Edges)-1].To)

	return fmt.Sprintf("[%s] profit=%.4f%%", strings.Join(parts, "->"), (c.ProfitFactor-1)*100)
}

// CycleSet manages a deduplicated set of cycles.
type CycleSet struct {
	cycles map[string]*Cycle
}

// NewCycleSet creates a new cycle set.
func NewCycleSet() *CycleSet {
	return &CycleSet{
		cycles: make(map[string]*Cycle),
	}
}

// Add adds a cycle to the set if it's not a duplicate.
// Returns true if the cycle was added.
func (s *CycleSet) Add(c *Cycle) bool {
	if c == nil {
		return false
	}

	key := c.UniqueKey()
	if key == "" {
		return false
	}

	if existing, exists := s.cycles[key]; exists {
		// Keep the one with better profit
		if c.ProfitFactor > existing.ProfitFactor {
			s.cycles[key] = c
		}
		return false
	}

	s.cycles[key] = c
	return true
}

// GetAll returns all cycles in the set.
func (s *CycleSet) GetAll() []*Cycle {
	result := make([]*Cycle, 0, len(s.cycles))
	for _, c := range s.cycles {
		result = append(result, c)
	}
	return result
}

// GetProfitable returns cycles with profit factor >= minProfit, sorted by profit descending.
func (s *CycleSet) GetProfitable(minProfit float64) []*Cycle {
	var result []*Cycle
	for _, c := range s.cycles {
		if c.IsProfitable(minProfit) {
			result = append(result, c)
		}
	}

	// Sort by profit descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].ProfitFactor > result[j].ProfitFactor
	})

	return result
}

// Count returns the number of cycles in the set.
func (s *CycleSet) Count() int {
	return len(s.cycles)
}

// ValidateCycle checks if a cycle is valid:
// 1. Forms a complete loop (edges connect properly)
// 2. Does not reuse any pool (each pool used at most once)
func ValidateCycle(edges []graph.Edge) bool {
	if len(edges) < 2 {
		return false
	}

	// Check each edge connects to the next
	for i := 0; i < len(edges)-1; i++ {
		if edges[i].To != edges[i+1].From {
			return false
		}
	}

	// Check last edge connects back to first
	if edges[len(edges)-1].To != edges[0].From {
		return false
	}

	// Check for duplicate pools - a valid arbitrage cannot reuse a pool
	usedPools := make(map[string]bool)
	for _, e := range edges {
		if usedPools[e.PoolAddr] {
			return false // Pool reused - invalid cycle
		}
		usedPools[e.PoolAddr] = true
	}

	return true
}

// FilterByStartTokens filters cycles to only include those starting from specified tokens.
func FilterByStartTokens(cycles []*Cycle, startIndices map[int]bool) []*Cycle {
	var filtered []*Cycle
	for _, c := range cycles {
		start := c.StartToken()
		if startIndices[start] {
			filtered = append(filtered, c)
		}

		// Also check if cycle passes through any start token
		for _, e := range c.Edges {
			if startIndices[e.From] {
				// Rotate cycle to start from this token
				rotated := rotateCycle(c.Edges, e.From)
				if rotated != nil {
					filtered = append(filtered, NewCycle(rotated))
					break
				}
			}
		}
	}
	return filtered
}

// rotateCycle rotates a cycle to start from the specified token index.
func rotateCycle(edges []graph.Edge, startToken int) []graph.Edge {
	// Find the edge starting from startToken
	startIdx := -1
	for i, e := range edges {
		if e.From == startToken {
			startIdx = i
			break
		}
	}

	if startIdx < 0 {
		return nil
	}

	// Rotate edges
	rotated := make([]graph.Edge, len(edges))
	for i := 0; i < len(edges); i++ {
		rotated[i] = edges[(startIdx+i)%len(edges)]
	}

	return rotated
}
