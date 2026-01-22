package curator

import (
	"context"
	"time"

	"watcher/internal/graph"
	"watcher/internal/ingestion"
	"watcher/internal/metrics"
	"watcher/internal/persistence"
	"watcher/pkg/chain/base"

	"github.com/rs/zerolog/log"
)

// Config holds curator configuration.
type Config struct {
	FactoryAddress       string
	TopPoolsCount        int
	MinTVLUSD            float64
	ReevaluationInterval time.Duration
	BootstrapBatchSize   int
	StartTokens          []string // Start tokens for arbitrage - must always be included
}

// Curator manages the pool lifecycle including bootstrap, tracking, and evaluation.
type Curator struct {
	config       Config
	client       *base.Client
	store        *persistence.Store
	graphManager *graph.Manager
	metrics      *metrics.Metrics
	ingestion    *ingestion.Service

	bootstrap *Bootstrap
	evaluator *Evaluator
}

// NewCurator creates a new curator.
func NewCurator(
	cfg Config,
	client *base.Client,
	store *persistence.Store,
	graphManager *graph.Manager,
	m *metrics.Metrics,
	ingestionSvc *ingestion.Service,
) *Curator {
	return &Curator{
		config:       cfg,
		client:       client,
		store:        store,
		graphManager: graphManager,
		metrics:      m,
		ingestion:    ingestionSvc,
		bootstrap:    NewBootstrap(client, cfg.FactoryAddress, cfg.BootstrapBatchSize, cfg.StartTokens),
		evaluator: NewEvaluator(
			client,
			store,
			graphManager,
			cfg.FactoryAddress,
			cfg.TopPoolsCount,
			cfg.MinTVLUSD,
			cfg.ReevaluationInterval,
			cfg.StartTokens,
		),
	}
}

// Bootstrap performs initial pool loading.
func (c *Curator) Bootstrap(ctx context.Context) error {
	startTime := time.Now()
	log.Info().
		Int("target_pools", c.config.TopPoolsCount).
		Msg("Starting bootstrap")

	// Check if we have cached data
	poolCount, err := c.store.GetPoolCount(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check pool count in database")
		poolCount = 0
	}

	var pools []PoolInfo
	var tokens map[string]*TokenInfo

	if poolCount >= c.config.TopPoolsCount/2 {
		// Load from cache and refresh reserves
		log.Info().Int("cached", poolCount).Msg("Loading pools from cache")
		pools, tokens, err = c.loadFromCacheAndRefresh(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Cache load failed, performing full bootstrap")
			pools, tokens, err = c.bootstrap.FetchTopPools(ctx, c.config.TopPoolsCount)
		}
	} else {
		// Full bootstrap
		pools, tokens, err = c.bootstrap.FetchTopPools(ctx, c.config.TopPoolsCount)
	}

	if err != nil {
		return err
	}

	// Add to graph
	graphPools := ConvertToGraphPools(pools)
	graphTokens := ConvertToGraphTokens(tokens)
	c.graphManager.AddPoolBatch(graphPools, graphTokens)

	// Persist to database
	if err := c.store.BulkUpsertTokens(ctx, ConvertToPersistenceTokens(tokens)); err != nil {
		log.Warn().Err(err).Msg("Failed to persist tokens")
	}

	if err := c.store.BulkUpsertPools(ctx, ConvertToPersistencePools(pools)); err != nil {
		log.Warn().Err(err).Msg("Failed to persist pools")
	}

	// Set tracked pools
	addresses := make([]string, len(pools))
	for i, p := range pools {
		addresses[i] = p.Address
	}
	if err := c.store.SetTrackedPools(ctx, addresses); err != nil {
		log.Warn().Err(err).Msg("Failed to set tracked pools")
	}

	// Update ingestion service with tracked pools
	c.ingestion.SetTrackedPools(addresses)

	// Record metrics
	if c.metrics != nil {
		c.metrics.RecordBootstrapLatency(time.Since(startTime))
		c.metrics.SetPoolsTracked(len(pools))

		nodes, edges, _ := c.graphManager.Stats()
		c.metrics.RecordGraphStats(nodes, edges)
	}

	log.Info().
		Int("pools", len(pools)).
		Int("tokens", len(tokens)).
		Dur("duration", time.Since(startTime)).
		Msg("Bootstrap complete")

	return nil
}

// loadFromCacheAndRefresh loads pools from the database and refreshes reserves.
func (c *Curator) loadFromCacheAndRefresh(ctx context.Context) ([]PoolInfo, map[string]*TokenInfo, error) {
	// Load cached pools
	cachedPools, err := c.store.GetTopPoolsByTVL(ctx, c.config.TopPoolsCount)
	if err != nil {
		return nil, nil, err
	}

	// Load cached tokens
	cachedTokens, err := c.store.GetAllTokens(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Convert to internal types
	poolAddresses := make([]string, len(cachedPools))
	for i, p := range cachedPools {
		poolAddresses[i] = p.Address
	}

	// Refresh reserves via bootstrap
	pools, err := c.bootstrap.fetchPoolDetails(ctx, poolAddresses)
	if err != nil {
		return nil, nil, err
	}

	// Convert tokens
	tokens := make(map[string]*TokenInfo, len(cachedTokens))
	for _, t := range cachedTokens {
		tokens[t.Address] = &TokenInfo{
			Address:  t.Address,
			Symbol:   t.Symbol,
			Decimals: t.Decimals,
		}
	}

	// Fetch any missing token info
	uniqueTokens := make(map[string]struct{})
	for _, p := range pools {
		uniqueTokens[p.Token0] = struct{}{}
		uniqueTokens[p.Token1] = struct{}{}
	}

	var missingTokens []string
	for token := range uniqueTokens {
		if _, ok := tokens[token]; !ok {
			missingTokens = append(missingTokens, token)
		}
	}

	if len(missingTokens) > 0 {
		newTokens, err := c.bootstrap.fetchTokenInfo(ctx, pools)
		if err == nil {
			for addr, t := range newTokens {
				tokens[addr] = t
			}
		}
	}

	return pools, tokens, nil
}

// Run starts the curator's background processes.
func (c *Curator) Run(ctx context.Context) error {
	// Start processing PoolCreated events
	go c.processPoolCreatedEvents(ctx)

	// Start periodic re-evaluation
	return c.evaluator.Run(ctx)
}

// processPoolCreatedEvents processes new pool creation events.
func (c *Curator) processPoolCreatedEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-c.ingestion.PoolCreatedEvents():
			if !ok {
				return
			}

			// Skip stable pools
			if event.IsStable {
				log.Debug().Str("pool", event.PoolAddress).Msg("Skipping stable pool")
				continue
			}

			// Evaluate and potentially add the pool
			added, err := c.evaluator.EvaluateNewPool(ctx, event.PoolAddress, event.Token0, event.Token1)
			if err != nil {
				log.Warn().Err(err).Str("pool", event.PoolAddress).Msg("Failed to evaluate new pool")
				continue
			}

			if added {
				// Add to ingestion tracking
				c.ingestion.AddTrackedPool(event.PoolAddress)
			}
		}
	}
}

// GetTrackedPools returns the list of tracked pool addresses.
func (c *Curator) GetTrackedPools() []string {
	return c.graphManager.GetTrackedPools()
}

// PoolCount returns the number of tracked pools.
func (c *Curator) PoolCount() int {
	_, _, pools := c.graphManager.Stats()
	return pools
}
