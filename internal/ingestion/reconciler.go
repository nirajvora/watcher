package ingestion

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"watcher/internal/graph"
	"watcher/pkg/chain/base"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"
)

const (
	// maxBlockRange limits the number of blocks to query in a single getLogs call
	// to avoid RPC timeouts on large ranges
	maxBlockRange = 1000
)

// Reconciler fetches historical events to fill gaps between bootstrap and streaming.
type Reconciler struct {
	client       *base.Client
	decoder      *Decoder
	graphManager *graph.Manager
	trackedPools map[string]struct{}
}

// NewReconciler creates a new reconciler.
func NewReconciler(client *base.Client, graphManager *graph.Manager) *Reconciler {
	return &Reconciler{
		client:       client,
		decoder:      NewDecoder(),
		graphManager: graphManager,
		trackedPools: make(map[string]struct{}),
	}
}

// SetTrackedPools sets the pools to reconcile.
func (r *Reconciler) SetTrackedPools(addresses []string) {
	r.trackedPools = make(map[string]struct{}, len(addresses))
	for _, addr := range addresses {
		r.trackedPools[strings.ToLower(addr)] = struct{}{}
	}
}

// ReconcileResult contains statistics from reconciliation.
type ReconcileResult struct {
	FromBlock      uint64
	ToBlock        uint64
	EventsFound    int
	EventsApplied  int
	PoolsUpdated   int
	Duration       time.Duration
}

// Reconcile fetches and applies historical Sync events from fromBlock to toBlock.
// This fills the gap between bootstrap (which fetches reserves at a point in time)
// and WebSocket streaming (which only receives future events).
func (r *Reconciler) Reconcile(ctx context.Context, fromBlock, toBlock uint64) (*ReconcileResult, error) {
	if fromBlock > toBlock {
		return &ReconcileResult{FromBlock: fromBlock, ToBlock: toBlock}, nil
	}

	startTime := time.Now()
	result := &ReconcileResult{
		FromBlock: fromBlock,
		ToBlock:   toBlock,
	}

	log.Info().
		Uint64("from_block", fromBlock).
		Uint64("to_block", toBlock).
		Int("tracked_pools", len(r.trackedPools)).
		Msg("Starting reconciliation")

	// Build list of pool addresses to query
	poolAddresses := make([]common.Address, 0, len(r.trackedPools))
	for addr := range r.trackedPools {
		poolAddresses = append(poolAddresses, common.HexToAddress(addr))
	}

	if len(poolAddresses) == 0 {
		log.Warn().Msg("No tracked pools for reconciliation")
		return result, nil
	}

	// Track which pools received updates
	poolsUpdated := make(map[string]struct{})

	// Query in chunks to avoid RPC limits
	for chunkStart := fromBlock; chunkStart <= toBlock; chunkStart += maxBlockRange {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		chunkEnd := chunkStart + maxBlockRange - 1
		if chunkEnd > toBlock {
			chunkEnd = toBlock
		}

		events, err := r.fetchSyncEvents(ctx, poolAddresses, chunkStart, chunkEnd)
		if err != nil {
			log.Warn().
				Err(err).
				Uint64("from", chunkStart).
				Uint64("to", chunkEnd).
				Msg("Failed to fetch events for block range, continuing")
			continue
		}

		result.EventsFound += len(events)

		// Apply events to graph
		for _, event := range events {
			poolAddr := strings.ToLower(event.PoolAddress)

			// Only apply if pool is tracked
			if _, tracked := r.trackedPools[poolAddr]; !tracked {
				continue
			}

			update := graph.ReserveUpdate{
				PoolAddress: event.PoolAddress,
				Reserve0:    event.Reserve0,
				Reserve1:    event.Reserve1,
				BlockNumber: event.BlockNumber,
				LogIndex:    event.LogIndex,
				Timestamp:   event.Timestamp,
			}
			r.graphManager.ProcessUpdate(update)
			result.EventsApplied++
			poolsUpdated[poolAddr] = struct{}{}
		}

		if chunkEnd < toBlock {
			log.Debug().
				Uint64("chunk_end", chunkEnd).
				Int("events_so_far", result.EventsFound).
				Msg("Reconciliation progress")
		}
	}

	result.PoolsUpdated = len(poolsUpdated)
	result.Duration = time.Since(startTime)

	log.Info().
		Uint64("from_block", fromBlock).
		Uint64("to_block", toBlock).
		Int("events_found", result.EventsFound).
		Int("events_applied", result.EventsApplied).
		Int("pools_updated", result.PoolsUpdated).
		Dur("duration", result.Duration).
		Msg("Reconciliation complete")

	return result, nil
}

// fetchSyncEvents fetches Sync events from the blockchain for the given block range.
func (r *Reconciler) fetchSyncEvents(ctx context.Context, addresses []common.Address, fromBlock, toBlock uint64) ([]*SyncEvent, error) {
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Addresses: addresses,
		Topics:    [][]common.Hash{{SyncEventTopic}},
	}

	logs, err := r.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("filtering logs: %w", err)
	}

	events := make([]*SyncEvent, 0, len(logs))
	for _, ethLog := range logs {
		// Skip removed logs (reorgs)
		if ethLog.Removed {
			continue
		}

		// Convert to LogEntry for decoder
		logEntry := &LogEntry{
			Address:         strings.ToLower(ethLog.Address.Hex()),
			Topics:          make([]string, len(ethLog.Topics)),
			Data:            fmt.Sprintf("0x%x", ethLog.Data),
			BlockNumber:     fmt.Sprintf("0x%x", ethLog.BlockNumber),
			TransactionHash: ethLog.TxHash.Hex(),
			LogIndex:        fmt.Sprintf("0x%x", ethLog.Index),
			Removed:         ethLog.Removed,
		}
		for i, topic := range ethLog.Topics {
			logEntry.Topics[i] = topic.Hex()
		}

		event, err := r.decoder.DecodeSyncEvent(logEntry)
		if err != nil {
			log.Debug().
				Err(err).
				Str("pool", logEntry.Address).
				Uint64("block", ethLog.BlockNumber).
				Msg("Failed to decode Sync event during reconciliation")
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// GetCurrentBlock returns the current block number from the RPC.
func (r *Reconciler) GetCurrentBlock(ctx context.Context) (uint64, error) {
	return r.client.BlockNumber(ctx)
}
