package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"watcher/internal/graph"
	"watcher/internal/metrics"

	"github.com/rs/zerolog/log"
)

const (
	maxReconnectAttempts = 10
	initialBackoff       = 1 * time.Second
	maxBackoff           = 30 * time.Second
)

// Service handles event ingestion from the blockchain.
type Service struct {
	wsURL   string
	client  *WSClient
	decoder *Decoder

	graphManager *graph.Manager
	metrics      *metrics.Metrics

	// Tracked pool addresses (all lowercase)
	mu             sync.RWMutex
	trackedPools   map[string]struct{}
	factoryAddress string

	// Event channels
	syncEvents        chan *SyncEvent
	poolCreatedEvents chan *PoolCreatedEvent

	// State
	lastBlockNumber uint64
}

// NewService creates a new ingestion service.
func NewService(
	wsURL string,
	factoryAddress string,
	graphManager *graph.Manager,
	m *metrics.Metrics,
) *Service {
	return &Service{
		wsURL:             wsURL,
		decoder:           NewDecoder(),
		graphManager:      graphManager,
		metrics:           m,
		trackedPools:      make(map[string]struct{}),
		factoryAddress:    strings.ToLower(factoryAddress),
		syncEvents:        make(chan *SyncEvent, 1000),
		poolCreatedEvents: make(chan *PoolCreatedEvent, 100),
	}
}

// SyncEvents returns the channel for receiving Sync events.
func (s *Service) SyncEvents() <-chan *SyncEvent {
	return s.syncEvents
}

// PoolCreatedEvents returns the channel for receiving PoolCreated events.
func (s *Service) PoolCreatedEvents() <-chan *PoolCreatedEvent {
	return s.poolCreatedEvents
}

// SetTrackedPools sets the list of pool addresses to track.
func (s *Service) SetTrackedPools(addresses []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.trackedPools = make(map[string]struct{}, len(addresses))
	for _, addr := range addresses {
		s.trackedPools[strings.ToLower(addr)] = struct{}{}
	}

	log.Info().Int("count", len(addresses)).Msg("Updated tracked pools")
}

// AddTrackedPool adds a pool address to track.
func (s *Service) AddTrackedPool(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.trackedPools[strings.ToLower(address)] = struct{}{}
}

// IsTracked returns true if the pool is being tracked.
func (s *Service) IsTracked(address string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.trackedPools[strings.ToLower(address)]
	return exists
}

// TrackedPoolCount returns the number of tracked pools.
func (s *Service) TrackedPoolCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.trackedPools)
}

// Run starts the ingestion service with automatic reconnection.
func (s *Service) Run(ctx context.Context) error {
	for attempt := 0; attempt < maxReconnectAttempts; attempt++ {
		if attempt > 0 {
			backoff := calculateBackoff(attempt)
			log.Info().
				Int("attempt", attempt).
				Dur("backoff", backoff).
				Msg("Reconnecting to WebSocket")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := s.runOnce(ctx)
		if err == nil || ctx.Err() != nil {
			return err
		}

		log.Error().Err(err).Msg("WebSocket connection error")

		if s.metrics != nil {
			s.metrics.SetWebSocketConnected(false)
		}
	}

	return fmt.Errorf("max reconnection attempts reached")
}

// runOnce runs the ingestion service until an error occurs or context is canceled.
func (s *Service) runOnce(ctx context.Context) error {
	s.client = NewWSClient(s.wsURL)

	if err := s.client.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to websocket: %w", err)
	}
	defer s.client.Close()

	if s.metrics != nil {
		s.metrics.SetWebSocketConnected(true)
	}

	// Subscribe to events
	if err := s.subscribe(ctx); err != nil {
		return fmt.Errorf("subscribing to events: %w", err)
	}

	// Start ping loop
	go s.client.StartPingLoop(ctx)

	// Start message reader
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.client.ReadMessages(ctx)
	}()

	// Process messages
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errCh:
			return err

		case msg := <-s.client.Messages():
			s.processMessage(msg)
		}
	}
}

// subscribe subscribes to Sync and PoolCreated events.
func (s *Service) subscribe(ctx context.Context) error {
	s.mu.RLock()
	addresses := make([]string, 0, len(s.trackedPools)+1)
	for addr := range s.trackedPools {
		addresses = append(addresses, addr)
	}
	s.mu.RUnlock()

	// Add factory address for PoolCreated events
	if s.factoryAddress != "" {
		addresses = append(addresses, s.factoryAddress)
	}

	// Subscribe to both Sync and PoolCreated events
	topics := []string{
		SyncEventTopic.Hex(),
		PoolCreatedEventTopic.Hex(),
	}

	return s.client.Subscribe(ctx, addresses, topics)
}

// Resubscribe updates the subscription with new addresses.
func (s *Service) Resubscribe(ctx context.Context) error {
	if s.client == nil || !s.client.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Unsubscribe first
	if err := s.client.Unsubscribe(ctx); err != nil {
		log.Warn().Err(err).Msg("Failed to unsubscribe")
	}

	// Resubscribe with new addresses
	return s.subscribe(ctx)
}

// processMessage processes a raw WebSocket message.
func (s *Service) processMessage(raw json.RawMessage) {
	log.Debug().RawJSON("message", raw).Msg("Received WebSocket message")

	// Parse subscription notification
	var notification struct {
		Subscription string   `json:"subscription"`
		Result       LogEntry `json:"result"`
	}

	if err := json.Unmarshal(raw, &notification); err != nil {
		log.Warn().Err(err).Msg("Failed to parse notification")
		return
	}

	logEntry := &notification.Result

	// Skip removed logs (chain reorg)
	if logEntry.Removed {
		log.Debug().
			Str("tx", logEntry.TransactionHash).
			Msg("Skipping removed log")
		return
	}

	// Log what type of event we received
	if IsSyncEvent(logEntry) {
		log.Info().
			Str("address", logEntry.Address).
			Str("block", logEntry.BlockNumber).
			Msg("Received Sync event from WebSocket")
		s.processSyncEvent(logEntry)
	} else if IsPoolCreatedEvent(logEntry) {
		log.Info().
			Str("address", logEntry.Address).
			Str("block", logEntry.BlockNumber).
			Msg("Received PoolCreated event from WebSocket")
		s.processPoolCreatedEvent(logEntry)
	} else {
		log.Debug().
			Str("address", logEntry.Address).
			Int("topics", len(logEntry.Topics)).
			Msg("Received unknown event type")
	}
}

// processSyncEvent decodes and processes a Sync event.
func (s *Service) processSyncEvent(logEntry *LogEntry) {
	// Normalize address for comparison
	normalizedAddr := strings.ToLower(logEntry.Address)

	// Check if we're tracking this pool
	if !s.IsTracked(normalizedAddr) {
		log.Debug().
			Str("pool", normalizedAddr).
			Msg("Sync event for untracked pool, skipping")
		return
	}

	event, err := s.decoder.DecodeSyncEvent(logEntry)
	if err != nil {
		log.Warn().Err(err).Str("pool", normalizedAddr).Msg("Failed to decode Sync event")
		return
	}

	log.Info().
		Str("pool", event.PoolAddress).
		Uint64("block", event.BlockNumber).
		Str("reserve0", event.Reserve0.String()).
		Str("reserve1", event.Reserve1.String()).
		Msg("Decoded Sync event, sending to graph manager")

	// Update metrics
	if s.metrics != nil {
		s.metrics.RecordEventReceived("sync")
		s.metrics.RecordEventLatency(event.Timestamp)
	}

	// Update graph
	update := graph.ReserveUpdate{
		PoolAddress: event.PoolAddress,
		Reserve0:    event.Reserve0,
		Reserve1:    event.Reserve1,
		BlockNumber: event.BlockNumber,
		LogIndex:    event.LogIndex,
		Timestamp:   event.Timestamp,
	}
	s.graphManager.ProcessUpdate(update)

	// Track block number
	if event.BlockNumber > s.lastBlockNumber {
		s.lastBlockNumber = event.BlockNumber
	}

	// Send to channel for additional processing if needed
	select {
	case s.syncEvents <- event:
	default:
		// Channel full, skip
	}
}

// processPoolCreatedEvent decodes and processes a PoolCreated event.
func (s *Service) processPoolCreatedEvent(logEntry *LogEntry) {
	event, err := s.decoder.DecodePoolCreatedEvent(logEntry)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to decode PoolCreated event")
		return
	}

	// Update metrics
	if s.metrics != nil {
		s.metrics.RecordEventReceived("pool_created")
	}

	// Send to channel for curator to handle
	select {
	case s.poolCreatedEvents <- event:
		log.Info().
			Str("pool", event.PoolAddress).
			Str("token0", event.Token0).
			Str("token1", event.Token1).
			Bool("stable", event.IsStable).
			Msg("New pool created")
	default:
		log.Warn().Str("pool", event.PoolAddress).Msg("PoolCreated channel full")
	}
}

// LastBlockNumber returns the last block number seen.
func (s *Service) LastBlockNumber() uint64 {
	return s.lastBlockNumber
}

func calculateBackoff(attempt int) time.Duration {
	backoff := initialBackoff * (1 << uint(attempt))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}
