package detector

import (
	"watcher/internal/graph"
)

const (
	// infinity represents an unreachable distance.
	infinity = 1e18
)

// SPFA implements the Shortest Path Faster Algorithm, an optimized Bellman-Ford variant.
// Returns distance array, predecessor array, predecessor edge array, and whether a negative cycle was found.
func SPFA(snap *graph.Snapshot, sourceIdx int) ([]float64, []int, []graph.Edge, bool) {
	n := snap.NumNodes()
	if n == 0 || sourceIdx < 0 || sourceIdx >= n {
		return nil, nil, nil, false
	}

	// Initialize arrays
	dist := make([]float64, n)
	pred := make([]int, n)       // Predecessor node index
	predEdge := make([]graph.Edge, n) // Predecessor edge
	inQueue := make([]bool, n)
	count := make([]int, n)      // Number of times node entered queue

	for i := 0; i < n; i++ {
		dist[i] = infinity
		pred[i] = -1
	}
	dist[sourceIdx] = 0

	// Queue for nodes to process
	queue := make([]int, 0, n)
	queue = append(queue, sourceIdx)
	inQueue[sourceIdx] = true
	count[sourceIdx] = 1

	hasNegativeCycle := false

	for len(queue) > 0 {
		// Dequeue front
		u := queue[0]
		queue = queue[1:]
		inQueue[u] = false

		// Relax all edges from u
		edges := snap.GetEdgesFrom(u)
		for _, edge := range edges {
			v := edge.To
			newDist := dist[u] + edge.Weight

			if newDist < dist[v] {
				dist[v] = newDist
				pred[v] = u
				predEdge[v] = edge

				if !inQueue[v] {
					queue = append(queue, v)
					inQueue[v] = true
					count[v]++

					// If a node has been added to queue more than n times, negative cycle exists
					if count[v] > n {
						hasNegativeCycle = true
						// Continue to find the actual cycle
					}
				}
			}
		}
	}

	return dist, pred, predEdge, hasNegativeCycle
}

// SPFAWithCycleDetection runs SPFA and explicitly detects negative cycles.
// Returns the node in the negative cycle (if found) and related data.
func SPFAWithCycleDetection(snap *graph.Snapshot, sourceIdx int, maxPathLen int) (cycleNode int, dist []float64, pred []int, predEdge []graph.Edge) {
	n := snap.NumNodes()
	if n == 0 || sourceIdx < 0 || sourceIdx >= n {
		return -1, nil, nil, nil
	}

	dist = make([]float64, n)
	pred = make([]int, n)
	predEdge = make([]graph.Edge, n)
	inQueue := make([]bool, n)
	count := make([]int, n)

	for i := 0; i < n; i++ {
		dist[i] = infinity
		pred[i] = -1
	}
	dist[sourceIdx] = 0

	queue := make([]int, 0, n)
	queue = append(queue, sourceIdx)
	inQueue[sourceIdx] = true
	count[sourceIdx] = 1

	cycleNode = -1
	maxIterations := n * maxPathLen // Limit iterations to prevent infinite loops

	for iter := 0; len(queue) > 0 && iter < maxIterations; iter++ {
		u := queue[0]
		queue = queue[1:]
		inQueue[u] = false

		edges := snap.GetEdgesFrom(u)
		for _, edge := range edges {
			v := edge.To
			newDist := dist[u] + edge.Weight

			if newDist < dist[v] {
				dist[v] = newDist
				pred[v] = u
				predEdge[v] = edge

				if !inQueue[v] {
					queue = append(queue, v)
					inQueue[v] = true
					count[v]++

					if count[v] > n {
						cycleNode = v
						return
					}
				}
			}
		}
	}

	return cycleNode, dist, pred, predEdge
}

// BellmanFordFromSource runs SPFA from a single source and detects negative cycles.
// Returns any detected cycle (path of node indices) or nil if no cycle.
func BellmanFordFromSource(snap *graph.Snapshot, sourceIdx int, maxPathLen int) []graph.Edge {
	cycleNode, _, pred, predEdge := SPFAWithCycleDetection(snap, sourceIdx, maxPathLen)

	if cycleNode < 0 {
		return nil
	}

	// Extract cycle starting from cycleNode
	return extractCycleFromPred(cycleNode, pred, predEdge, snap.NumNodes())
}

// extractCycleFromPred extracts a cycle from predecessor arrays starting from a known cycle node.
func extractCycleFromPred(cycleNode int, pred []int, predEdge []graph.Edge, n int) []graph.Edge {
	// Find a node definitely in the cycle by walking back n times
	nodeInCycle := cycleNode
	for i := 0; i < n; i++ {
		if pred[nodeInCycle] < 0 {
			return nil // No valid predecessor
		}
		nodeInCycle = pred[nodeInCycle]
	}

	// Now walk until we return to this node to extract the cycle
	visited := make(map[int]bool)
	var cycle []graph.Edge
	current := nodeInCycle

	for {
		if visited[current] {
			break
		}
		visited[current] = true

		if pred[current] < 0 {
			return nil
		}

		cycle = append(cycle, predEdge[current])
		current = pred[current]

		if current == nodeInCycle {
			break
		}

		if len(cycle) > n {
			// Safety limit reached
			break
		}
	}

	// Reverse to get correct order (from -> to)
	reversed := make([]graph.Edge, len(cycle))
	for i, e := range cycle {
		reversed[len(cycle)-1-i] = e
	}

	return reversed
}

// FindNegativeCycleContaining searches for a negative cycle containing the source node.
// This is specifically for arbitrage starting and ending at the same token.
func FindNegativeCycleContaining(snap *graph.Snapshot, sourceIdx int, maxPathLen int) []graph.Edge {
	n := snap.NumNodes()
	if n == 0 || sourceIdx < 0 || sourceIdx >= n {
		return nil
	}

	// Use a modified approach: run Bellman-Ford and check if source can be relaxed
	dist := make([]float64, n)
	pred := make([]int, n)
	predEdge := make([]graph.Edge, n)

	for i := 0; i < n; i++ {
		dist[i] = infinity
		pred[i] = -1
	}
	dist[sourceIdx] = 0

	// Run V-1 iterations (or maxPathLen-1 if smaller)
	iterations := n - 1
	if maxPathLen-1 < iterations {
		iterations = maxPathLen - 1
	}

	for i := 0; i < iterations; i++ {
		relaxed := false
		for u := 0; u < n; u++ {
			if dist[u] >= infinity/2 {
				continue
			}
			edges := snap.GetEdgesFrom(u)
			for _, edge := range edges {
				v := edge.To
				if dist[u]+edge.Weight < dist[v] {
					dist[v] = dist[u] + edge.Weight
					pred[v] = u
					predEdge[v] = edge
					relaxed = true
				}
			}
		}
		if !relaxed {
			break
		}
	}

	// Check for negative cycle involving source
	// Look at all edges coming back to source
	for u := 0; u < n; u++ {
		if dist[u] >= infinity/2 {
			continue
		}
		edges := snap.GetEdgesFrom(u)
		for _, edge := range edges {
			if edge.To == sourceIdx {
				totalWeight := dist[u] + edge.Weight
				if totalWeight < 0 {
					// Found negative cycle back to source
					// Reconstruct path
					path := []graph.Edge{edge}
					current := u
					visited := make(map[int]bool)

					for current != sourceIdx && !visited[current] && pred[current] >= 0 {
						visited[current] = true
						path = append(path, predEdge[current])
						current = pred[current]
					}

					if current != sourceIdx {
						continue // Path doesn't lead back properly
					}

					// Reverse path
					reversed := make([]graph.Edge, len(path))
					for i, e := range path {
						reversed[len(path)-1-i] = e
					}

					return reversed
				}
			}
		}
	}

	return nil
}
