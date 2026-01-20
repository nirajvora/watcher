package graph

import (
	"context"
	"math/big"
	"sync"
	"time"

	"watcher/internal/metrics"

	"github.com/rs/zerolog/log"
)

// ReserveUpdate represents a reserve update from a Sync event.
type ReserveUpdate struct {
	PoolAddress string
	Reserve0    *big.Int
	Reserve1    *big.Int
	BlockNumber uint64
	LogIndex    uint
	Timestamp   time.Time
}

// Manager handles graph state updates and snapshot creation.
// It accumulates updates within a block and applies them atomically.
type Manager struct {
	mu sync.Mutex

	graph        *Graph
	metrics      *metrics.Metrics

	// Pending updates for current block
	pendingBlock   uint64
	pendingUpdates []ReserveUpdate

	// Snapshot channel for detector
	snapshotCh chan *Snapshot

	// Last snapshot info
	lastSnapshotBlock uint64
}

// NewManager creates a new graph manager.
func NewManager(m *metrics.Metrics) *Manager {
	return &Manager{
		graph:      NewGraph(),
		metrics:    m,
		snapshotCh: make(chan *Snapshot, 10),
	}
}

// Graph returns the underlying graph for direct manipulation during bootstrap.
func (m *Manager) Graph() *Graph {
	return m.graph
}

// SnapshotCh returns the channel for receiving new snapshots.
func (m *Manager) SnapshotCh() <-chan *Snapshot {
	return m.snapshotCh
}

// ProcessUpdate handles a reserve update from a Sync event.
// Updates are batched per block and applied atomically.
func (m *Manager) ProcessUpdate(update ReserveUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If this is a new block, apply pending updates from the previous block
	if update.BlockNumber > m.pendingBlock && len(m.pendingUpdates) > 0 {
		m.applyPendingUpdatesLocked()
	}

	// Set current block
	m.pendingBlock = update.BlockNumber

	// Add to pending updates
	m.pendingUpdates = append(m.pendingUpdates, update)
}

// applyPendingUpdatesLocked applies all pending updates and creates a snapshot.
// Must be called with m.mu held.
func (m *Manager) applyPendingUpdatesLocked() {
	if len(m.pendingUpdates) == 0 {
		return
	}

	startTime := time.Now()
	blockNum := m.pendingUpdates[0].BlockNumber
	updatedCount := 0

	// Apply all updates to the graph
	for _, update := range m.pendingUpdates {
		if m.graph.UpdateReserves(update.PoolAddress, update.Reserve0, update.Reserve1) {
			updatedCount++
		}
	}

	// Clear pending updates
	m.pendingUpdates = m.pendingUpdates[:0]

	// Create snapshot
	snapshotStart := time.Now()
	snapshot := m.graph.CreateSnapshot(blockNum)
	snapshotDuration := time.Since(snapshotStart)

	// Update metrics
	if m.metrics != nil {
		m.metrics.RecordSnapshotLatency(snapshotDuration)
		m.metrics.RecordGraphStats(m.graph.NumNodes(), m.graph.NumEdges())
		m.metrics.SetLastBlockSeen(blockNum)
	}

	// Send snapshot to detector (non-blocking)
	select {
	case m.snapshotCh <- snapshot:
		m.lastSnapshotBlock = blockNum
		log.Debug().
			Uint64("block", blockNum).
			Int("updates", updatedCount).
			Dur("apply_time", time.Since(startTime)).
			Dur("snapshot_time", snapshotDuration).
			Int("nodes", snapshot.NumNodes()).
			Int("edges", snapshot.NumEdges()).
			Msg("Applied updates and created snapshot")
	default:
		log.Warn().
			Uint64("block", blockNum).
			Msg("Snapshot channel full, discarding snapshot")
	}
}

// Flush forces application of any pending updates and creates a snapshot.
func (m *Manager) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.applyPendingUpdatesLocked()
}

// AddPool adds a pool to the graph with initial state.
func (m *Manager) AddPool(pool PoolState, token0Info, token1Info TokenInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Add tokens first
	m.graph.addTokenLocked(token0Info)
	m.graph.addTokenLocked(token1Info)

	// Add pool
	m.graph.addPoolLocked(pool)
}

// AddPoolBatch adds multiple pools efficiently.
func (m *Manager) AddPoolBatch(pools []PoolState, tokenInfos map[string]TokenInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Add all tokens first
	for _, info := range tokenInfos {
		m.graph.addTokenLocked(info)
	}

	// Add all pools
	for _, pool := range pools {
		m.graph.addPoolLocked(pool)
	}

	// Update metrics
	if m.metrics != nil {
		m.metrics.RecordGraphStats(m.graph.NumNodes(), m.graph.NumEdges())
		m.metrics.SetPoolsTracked(m.graph.NumPools())
	}

	log.Info().
		Int("pools", len(pools)).
		Int("tokens", len(tokenInfos)).
		Int("total_nodes", m.graph.NumNodes()).
		Int("total_edges", m.graph.NumEdges()).
		Msg("Added pool batch to graph")
}

// GetCurrentSnapshot creates and returns a snapshot without going through the channel.
func (m *Manager) GetCurrentSnapshot(blockNumber uint64) *Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Apply any pending updates first
	if len(m.pendingUpdates) > 0 {
		m.applyPendingUpdatesLocked()
	}

	return m.graph.CreateSnapshot(blockNumber)
}

// Stats returns current graph statistics.
func (m *Manager) Stats() (nodes, edges, pools int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.graph.NumNodes(), m.graph.NumEdges(), m.graph.NumPools()
}

// HasPool checks if a pool exists in the graph.
func (m *Manager) HasPool(address string) bool {
	return m.graph.HasPool(address)
}

// GetTrackedPools returns all pool addresses currently in the graph.
func (m *Manager) GetTrackedPools() []string {
	return m.graph.GetAllPoolAddresses()
}

// Close closes the snapshot channel.
func (m *Manager) Close() {
	close(m.snapshotCh)
}

// Run starts the manager's background processing.
// This is a no-op for now but could be used for periodic flushing.
func (m *Manager) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
