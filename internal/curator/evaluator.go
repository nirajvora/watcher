package curator

import (
	"context"
	"math/big"
	"time"

	"watcher/internal/graph"
	"watcher/internal/persistence"
	"watcher/pkg/chain/base"

	"github.com/rs/zerolog/log"
)

// Evaluator periodically re-evaluates pool TVL and updates tracked pools.
type Evaluator struct {
	client         *base.Client
	store          *persistence.Store
	graphManager   *graph.Manager
	topPoolsCount  int
	minTVL         float64
	interval       time.Duration
	factoryAddress string
	startTokens    []string
}

// NewEvaluator creates a new pool evaluator.
func NewEvaluator(
	client *base.Client,
	store *persistence.Store,
	graphManager *graph.Manager,
	factoryAddress string,
	topPoolsCount int,
	minTVL float64,
	interval time.Duration,
	startTokens []string,
) *Evaluator {
	return &Evaluator{
		client:         client,
		store:          store,
		graphManager:   graphManager,
		topPoolsCount:  topPoolsCount,
		minTVL:         minTVL,
		interval:       interval,
		factoryAddress: factoryAddress,
		startTokens:    startTokens,
	}
}

// Run starts the periodic evaluation loop.
func (e *Evaluator) Run(ctx context.Context) error {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	log.Info().
		Dur("interval", e.interval).
		Int("top_pools", e.topPoolsCount).
		Msg("Starting pool evaluator")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := e.evaluate(ctx); err != nil {
				log.Error().Err(err).Msg("Evaluation failed")
			}
		}
	}
}

// evaluate performs a single evaluation cycle.
func (e *Evaluator) evaluate(ctx context.Context) error {
	startTime := time.Now()
	log.Info().Msg("Starting pool re-evaluation")

	// Fetch fresh pool data
	bootstrap := NewBootstrap(e.client, e.factoryAddress, 100, e.startTokens)
	pools, tokens, err := bootstrap.FetchTopPools(ctx, e.topPoolsCount)
	if err != nil {
		return err
	}

	// Update persistence
	if err := e.store.BulkUpsertTokens(ctx, ConvertToPersistenceTokens(tokens)); err != nil {
		log.Warn().Err(err).Msg("Failed to update tokens in database")
	}

	if err := e.store.BulkUpsertPools(ctx, ConvertToPersistencePools(pools)); err != nil {
		log.Warn().Err(err).Msg("Failed to update pools in database")
	}

	// Update graph
	graphPools := ConvertToGraphPools(pools)
	graphTokens := ConvertToGraphTokens(tokens)
	e.graphManager.AddPoolBatch(graphPools, graphTokens)

	// Update tracked pool list
	addresses := make([]string, len(pools))
	for i, p := range pools {
		addresses[i] = p.Address
	}
	if err := e.store.SetTrackedPools(ctx, addresses); err != nil {
		log.Warn().Err(err).Msg("Failed to update tracked pools")
	}

	log.Info().
		Int("pools", len(pools)).
		Int("tokens", len(tokens)).
		Dur("duration", time.Since(startTime)).
		Msg("Pool re-evaluation complete")

	return nil
}

// EvaluateNewPool evaluates a newly created pool.
func (e *Evaluator) EvaluateNewPool(ctx context.Context, poolAddr, token0, token1 string) (bool, error) {
	// Fetch pool details (no start token filtering needed for single pool fetch)
	bootstrap := NewBootstrap(e.client, e.factoryAddress, 100, nil)

	poolInfos, err := bootstrap.fetchPoolDetails(ctx, []string{poolAddr})
	if err != nil || len(poolInfos) == 0 {
		return false, err
	}

	pool := poolInfos[0]

	// Check if pool meets minimum TVL
	// For simplicity, we check if reserves are above a threshold
	minReserve := big.NewInt(1e17) // ~$100 minimum per side
	if pool.Reserve0.Cmp(minReserve) < 0 || pool.Reserve1.Cmp(minReserve) < 0 {
		log.Debug().Str("pool", poolAddr).Msg("New pool below TVL threshold")
		return false, nil
	}

	// Fetch token info
	tokensMap, err := bootstrap.fetchTokenInfo(ctx, poolInfos)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to fetch token info for new pool")
		tokensMap = map[string]*TokenInfo{
			token0: {Address: token0, Symbol: "UNKNOWN", Decimals: 18},
			token1: {Address: token1, Symbol: "UNKNOWN", Decimals: 18},
		}
	}

	// Add to graph
	graphPool := graph.PoolState{
		Address:  pool.Address,
		Token0:   pool.Token0,
		Token1:   pool.Token1,
		Reserve0: pool.Reserve0,
		Reserve1: pool.Reserve1,
		Fee:      pool.Fee,
	}

	token0Info := graph.TokenInfo{
		Address:  token0,
		Symbol:   tokensMap[token0].Symbol,
		Decimals: tokensMap[token0].Decimals,
	}
	token1Info := graph.TokenInfo{
		Address:  token1,
		Symbol:   tokensMap[token1].Symbol,
		Decimals: tokensMap[token1].Decimals,
	}

	e.graphManager.AddPool(graphPool, token0Info, token1Info)

	// Persist
	if err := e.store.UpsertPool(ctx, persistence.PoolRecord{
		Address:  pool.Address,
		Token0:   pool.Token0,
		Token1:   pool.Token1,
		Reserve0: pool.Reserve0.String(),
		Reserve1: pool.Reserve1.String(),
		Fee:      pool.Fee,
		IsStable: pool.IsStable,
	}); err != nil {
		log.Warn().Err(err).Msg("Failed to persist new pool")
	}

	log.Info().
		Str("pool", poolAddr).
		Str("token0", token0Info.Symbol).
		Str("token1", token1Info.Symbol).
		Msg("Added new pool to tracking")

	return true, nil
}
