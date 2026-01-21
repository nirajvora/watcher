package curator

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"watcher/internal/graph"
	"watcher/internal/persistence"
	"watcher/pkg/chain/base"
	"watcher/pkg/dex/aerodrome"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"
)

const (
	poolBatchSize  = 100
	tokenBatchSize = 50
	poolInfoCalls  = 4 // stable, reserves, token0, token1
	poolsPerBatch  = 25
)

// PoolInfo holds pool information during bootstrap.
type PoolInfo struct {
	Address  string
	Token0   string
	Token1   string
	Reserve0 *big.Int
	Reserve1 *big.Int
	IsStable bool
	Fee      float64
}

// TokenInfo holds token information during bootstrap.
type TokenInfo struct {
	Address  string
	Symbol   string
	Decimals int
}

// Bootstrap fetches pool data from the blockchain.
type Bootstrap struct {
	client         *base.Client
	factoryAddress common.Address
	batchSize      int
	startTokens    map[string]struct{} // Lowercase start tokens for quick lookup

	// Token cache
	tokenCache   map[string]*TokenInfo
	tokenCacheMu sync.RWMutex
}

// NewBootstrap creates a new bootstrap instance.
func NewBootstrap(client *base.Client, factoryAddress string, batchSize int, startTokens []string) *Bootstrap {
	// Build start token set with lowercase addresses
	startTokenSet := make(map[string]struct{}, len(startTokens))
	for _, token := range startTokens {
		startTokenSet[strings.ToLower(token)] = struct{}{}
	}

	return &Bootstrap{
		client:         client,
		factoryAddress: common.HexToAddress(factoryAddress),
		batchSize:      batchSize,
		startTokens:    startTokenSet,
		tokenCache:     make(map[string]*TokenInfo),
	}
}

// FetchTopPools fetches the top N pools by TVL.
func (b *Bootstrap) FetchTopPools(ctx context.Context, topN int) ([]PoolInfo, map[string]*TokenInfo, error) {
	startTime := time.Now()

	// Get total pool count
	totalPools, err := b.getPoolCount(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("getting pool count: %w", err)
	}
	log.Info().Int("total", totalPools).Msg("Total pools in factory")

	// Fetch all pool addresses
	poolAddresses, err := b.fetchPoolAddresses(ctx, totalPools)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching pool addresses: %w", err)
	}
	log.Info().Int("fetched", len(poolAddresses)).Dur("elapsed", time.Since(startTime)).Msg("Fetched pool addresses")

	// Fetch pool details in batches
	pools, err := b.fetchPoolDetails(ctx, poolAddresses)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching pool details: %w", err)
	}
	log.Info().Int("valid", len(pools)).Dur("elapsed", time.Since(startTime)).Msg("Fetched pool details")

	// Fetch token info for all unique tokens
	tokens, err := b.fetchTokenInfo(ctx, pools)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching token info: %w", err)
	}
	log.Info().Int("tokens", len(tokens)).Dur("elapsed", time.Since(startTime)).Msg("Fetched token info")

	// Sort by TVL and take top N, ensuring start token pools are included
	sortedPools := b.selectPoolsWithStartTokens(pools, tokens, topN)
	log.Info().
		Int("selected", len(sortedPools)).
		Dur("total_time", time.Since(startTime)).
		Msg("Bootstrap complete")

	// Validate start token presence
	b.validateStartTokenPools(sortedPools, tokens)

	return sortedPools, tokens, nil
}

// getPoolCount returns the total number of pools in the factory.
func (b *Bootstrap) getPoolCount(ctx context.Context) (int, error) {
	callData, err := aerodrome.V2FactoryABI.Pack("allPoolsLength")
	if err != nil {
		return 0, fmt.Errorf("packing call: %w", err)
	}

	calls := []base.ContractCall{{
		Target:   b.factoryAddress,
		CallData: callData,
	}}

	results, err := b.client.BatchCallContract(ctx, calls)
	if err != nil {
		return 0, fmt.Errorf("calling contract: %w", err)
	}

	if len(results) == 0 || !results[0].Success {
		return 0, fmt.Errorf("call failed")
	}

	var length *big.Int
	if err := aerodrome.V2FactoryABI.UnpackIntoInterface(&length, "allPoolsLength", results[0].Data); err != nil {
		return 0, fmt.Errorf("unpacking result: %w", err)
	}

	return int(length.Int64()), nil
}

// fetchPoolAddresses fetches all pool addresses from the factory.
func (b *Bootstrap) fetchPoolAddresses(ctx context.Context, total int) ([]string, error) {
	addresses := make([]string, 0, total)

	for i := 0; i < total; i += poolBatchSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + poolBatchSize
		if end > total {
			end = total
		}

		// Build batch of calls
		calls := make([]base.ContractCall, end-i)
		for j := i; j < end; j++ {
			callData, err := aerodrome.V2FactoryABI.Pack("allPools", big.NewInt(int64(j)))
			if err != nil {
				return nil, fmt.Errorf("packing call for index %d: %w", j, err)
			}
			calls[j-i] = base.ContractCall{
				Target:   b.factoryAddress,
				CallData: callData,
			}
		}

		results, err := b.client.BatchCallContract(ctx, calls)
		if err != nil {
			return nil, fmt.Errorf("batch call failed at offset %d: %w", i, err)
		}

		for j, result := range results {
			if !result.Success {
				continue
			}

			var addr common.Address
			if err := aerodrome.V2FactoryABI.UnpackIntoInterface(&addr, "allPools", result.Data); err != nil {
				log.Warn().Int("index", i+j).Err(err).Msg("Failed to unpack pool address")
				continue
			}

			addresses = append(addresses, strings.ToLower(addr.Hex()))
		}

		if (i+poolBatchSize)%1000 == 0 || end == total {
			log.Debug().Int("progress", end).Int("total", total).Msg("Fetching pool addresses")
		}
	}

	return addresses, nil
}

// fetchPoolDetails fetches details for all pools.
func (b *Bootstrap) fetchPoolDetails(ctx context.Context, addresses []string) ([]PoolInfo, error) {
	pools := make([]PoolInfo, 0, len(addresses))

	for i := 0; i < len(addresses); i += poolsPerBatch {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + poolsPerBatch
		if end > len(addresses) {
			end = len(addresses)
		}

		batch := addresses[i:end]
		batchPools, err := b.fetchPoolBatch(ctx, batch)
		if err != nil {
			log.Warn().Err(err).Int("offset", i).Msg("Batch failed, continuing")
			continue
		}

		pools = append(pools, batchPools...)

		if (i+poolsPerBatch)%500 == 0 || end == len(addresses) {
			log.Debug().Int("progress", end).Int("total", len(addresses)).Int("valid", len(pools)).Msg("Fetching pool details")
		}
	}

	return pools, nil
}

// fetchPoolBatch fetches details for a batch of pools.
func (b *Bootstrap) fetchPoolBatch(ctx context.Context, addresses []string) ([]PoolInfo, error) {
	// Build calls: 4 calls per pool (stable, reserves, token0, token1)
	calls := make([]base.ContractCall, 0, len(addresses)*poolInfoCalls)

	stableData, _ := aerodrome.V2PoolABI.Pack("stable")
	reservesData, _ := aerodrome.V2PoolABI.Pack("getReserves")
	token0Data, _ := aerodrome.V2PoolABI.Pack("token0")
	token1Data, _ := aerodrome.V2PoolABI.Pack("token1")

	for _, addr := range addresses {
		target := common.HexToAddress(addr)
		calls = append(calls,
			base.ContractCall{Target: target, CallData: stableData},
			base.ContractCall{Target: target, CallData: reservesData},
			base.ContractCall{Target: target, CallData: token0Data},
			base.ContractCall{Target: target, CallData: token1Data},
		)
	}

	results, err := b.client.BatchCallContract(ctx, calls)
	if err != nil {
		return nil, err
	}

	var pools []PoolInfo
	for i := 0; i < len(addresses); i++ {
		baseIdx := i * poolInfoCalls
		if baseIdx+poolInfoCalls > len(results) {
			break
		}

		stableResult := results[baseIdx]
		reservesResult := results[baseIdx+1]
		token0Result := results[baseIdx+2]
		token1Result := results[baseIdx+3]

		// Skip if any critical call failed
		if !stableResult.Success || !reservesResult.Success || !token0Result.Success || !token1Result.Success {
			continue
		}

		// Decode stable
		var isStable bool
		if err := aerodrome.V2PoolABI.UnpackIntoInterface(&isStable, "stable", stableResult.Data); err != nil {
			continue
		}

		// Skip stable pools (different AMM curve)
		if isStable {
			continue
		}

		// Decode reserves
		reserves := struct {
			Reserve0 *big.Int
			Reserve1 *big.Int
			BlockTimestampLast *big.Int
		}{}
		if err := aerodrome.V2PoolABI.UnpackIntoInterface(&reserves, "getReserves", reservesResult.Data); err != nil {
			continue
		}

		// Skip zero reserves
		if reserves.Reserve0.Sign() == 0 || reserves.Reserve1.Sign() == 0 {
			continue
		}

		// Skip very low liquidity (both reserves < 1e15 wei, roughly $1-10)
		minReserve := big.NewInt(1e15)
		if reserves.Reserve0.Cmp(minReserve) < 0 && reserves.Reserve1.Cmp(minReserve) < 0 {
			continue
		}

		// Decode tokens
		var token0, token1 common.Address
		if err := aerodrome.V2PoolABI.UnpackIntoInterface(&token0, "token0", token0Result.Data); err != nil {
			continue
		}
		if err := aerodrome.V2PoolABI.UnpackIntoInterface(&token1, "token1", token1Result.Data); err != nil {
			continue
		}

		pools = append(pools, PoolInfo{
			Address:  addresses[i],
			Token0:   strings.ToLower(token0.Hex()),
			Token1:   strings.ToLower(token1.Hex()),
			Reserve0: reserves.Reserve0,
			Reserve1: reserves.Reserve1,
			IsStable: isStable,
			Fee:      0.003, // Aerodrome V2 volatile fee is 0.3%
		})
	}

	return pools, nil
}

// fetchTokenInfo fetches metadata for all unique tokens.
func (b *Bootstrap) fetchTokenInfo(ctx context.Context, pools []PoolInfo) (map[string]*TokenInfo, error) {
	// Collect unique tokens
	uniqueTokens := make(map[string]struct{})
	for _, pool := range pools {
		uniqueTokens[pool.Token0] = struct{}{}
		uniqueTokens[pool.Token1] = struct{}{}
	}

	// Filter out already cached tokens
	var tokensToFetch []string
	b.tokenCacheMu.RLock()
	for token := range uniqueTokens {
		if _, cached := b.tokenCache[token]; !cached {
			tokensToFetch = append(tokensToFetch, token)
		}
	}
	b.tokenCacheMu.RUnlock()

	log.Debug().Int("unique", len(uniqueTokens)).Int("to_fetch", len(tokensToFetch)).Msg("Fetching token info")

	// Fetch in batches
	for i := 0; i < len(tokensToFetch); i += tokenBatchSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + tokenBatchSize
		if end > len(tokensToFetch) {
			end = len(tokensToFetch)
		}

		batch := tokensToFetch[i:end]
		if err := b.fetchTokenBatch(ctx, batch); err != nil {
			log.Warn().Err(err).Msg("Token batch failed, continuing with defaults")
		}
	}

	// Return all tokens (including cached)
	b.tokenCacheMu.RLock()
	defer b.tokenCacheMu.RUnlock()

	result := make(map[string]*TokenInfo, len(uniqueTokens))
	for token := range uniqueTokens {
		if info, ok := b.tokenCache[token]; ok {
			result[token] = info
		} else {
			// Use defaults if fetch failed
			result[token] = &TokenInfo{
				Address:  token,
				Symbol:   "UNKNOWN",
				Decimals: 18,
			}
		}
	}

	return result, nil
}

// fetchTokenBatch fetches metadata for a batch of tokens.
func (b *Bootstrap) fetchTokenBatch(ctx context.Context, addresses []string) error {
	// 2 calls per token (symbol, decimals)
	calls := make([]base.ContractCall, 0, len(addresses)*2)

	symbolData, _ := aerodrome.ERC20ABI.Pack("symbol")
	decimalsData, _ := aerodrome.ERC20ABI.Pack("decimals")

	for _, addr := range addresses {
		target := common.HexToAddress(addr)
		calls = append(calls,
			base.ContractCall{Target: target, CallData: symbolData},
			base.ContractCall{Target: target, CallData: decimalsData},
		)
	}

	results, err := b.client.BatchCallContract(ctx, calls)
	if err != nil {
		return err
	}

	b.tokenCacheMu.Lock()
	defer b.tokenCacheMu.Unlock()

	for i, addr := range addresses {
		baseIdx := i * 2
		if baseIdx+2 > len(results) {
			break
		}

		info := &TokenInfo{
			Address:  addr,
			Symbol:   "UNKNOWN",
			Decimals: 18,
		}

		// Decode symbol
		if results[baseIdx].Success {
			var symbol string
			if err := aerodrome.ERC20ABI.UnpackIntoInterface(&symbol, "symbol", results[baseIdx].Data); err == nil {
				info.Symbol = symbol
			}
		}

		// Decode decimals
		if results[baseIdx+1].Success {
			var decimals uint8
			if err := aerodrome.ERC20ABI.UnpackIntoInterface(&decimals, "decimals", results[baseIdx+1].Data); err == nil {
				info.Decimals = int(decimals)
			}
		}

		b.tokenCache[addr] = info
	}

	return nil
}

// selectPoolsWithStartTokens selects top N pools by TVL, ensuring all pools
// containing start tokens are included regardless of TVL ranking.
func (b *Bootstrap) selectPoolsWithStartTokens(pools []PoolInfo, tokens map[string]*TokenInfo, topN int) []PoolInfo {
	// Separate pools containing start tokens from others
	var startTokenPools []PoolInfo
	var otherPools []PoolInfo

	for _, pool := range pools {
		token0Lower := strings.ToLower(pool.Token0)
		token1Lower := strings.ToLower(pool.Token1)

		_, hasToken0 := b.startTokens[token0Lower]
		_, hasToken1 := b.startTokens[token1Lower]

		if hasToken0 || hasToken1 {
			startTokenPools = append(startTokenPools, pool)
		} else {
			otherPools = append(otherPools, pool)
		}
	}

	log.Info().
		Int("start_token_pools", len(startTokenPools)).
		Int("other_pools", len(otherPools)).
		Int("start_tokens_configured", len(b.startTokens)).
		Msg("Pool selection: separated start token pools")

	// Sort start token pools by TVL
	startTokenPools = sortPoolsByTVL(startTokenPools, tokens)

	// Sort other pools by TVL
	otherPools = sortPoolsByTVL(otherPools, tokens)

	// Build result: all start token pools first, then fill remaining with other pools
	result := make([]PoolInfo, 0, topN)

	// Add all start token pools (these are mandatory)
	result = append(result, startTokenPools...)

	// Fill remaining slots with other pools
	remaining := topN - len(result)
	if remaining > 0 && len(otherPools) > 0 {
		if remaining > len(otherPools) {
			remaining = len(otherPools)
		}
		result = append(result, otherPools[:remaining]...)
	}

	log.Info().
		Int("start_token_pools_included", len(startTokenPools)).
		Int("other_pools_included", len(result)-len(startTokenPools)).
		Int("total_selected", len(result)).
		Int("target", topN).
		Msg("Pool selection complete")

	return result
}

// validateStartTokenPools logs which start tokens have pools and warns if any are missing.
func (b *Bootstrap) validateStartTokenPools(pools []PoolInfo, tokens map[string]*TokenInfo) {
	// Count pools per start token
	poolsPerToken := make(map[string]int)
	for token := range b.startTokens {
		poolsPerToken[token] = 0
	}

	for _, pool := range pools {
		token0Lower := strings.ToLower(pool.Token0)
		token1Lower := strings.ToLower(pool.Token1)

		if _, isStart := b.startTokens[token0Lower]; isStart {
			poolsPerToken[token0Lower]++
		}
		if _, isStart := b.startTokens[token1Lower]; isStart {
			poolsPerToken[token1Lower]++
		}
	}

	// Log results and warn on missing
	for token, count := range poolsPerToken {
		symbol := "UNKNOWN"
		if info, ok := tokens[token]; ok {
			symbol = info.Symbol
		}

		if count == 0 {
			log.Warn().
				Str("token", token).
				Str("symbol", symbol).
				Msg("START TOKEN HAS ZERO POOLS - arbitrage detection will not work for this token!")
		} else {
			log.Info().
				Str("token", token).
				Str("symbol", symbol).
				Int("pool_count", count).
				Msg("Start token pool count validated")
		}
	}
}

// sortPoolsByTVL sorts pools by approximate TVL descending.
func sortPoolsByTVL(pools []PoolInfo, tokens map[string]*TokenInfo) []PoolInfo {
	if len(pools) == 0 {
		return pools
	}

	// Calculate approximate TVL for sorting
	type poolWithTVL struct {
		pool PoolInfo
		tvl  float64
	}

	poolsWithTVL := make([]poolWithTVL, len(pools))
	for i, pool := range pools {
		// Simple TVL estimate: sum of reserves normalized by decimals
		token0Info := tokens[pool.Token0]
		token1Info := tokens[pool.Token1]

		var tvl float64
		if token0Info != nil {
			r0, _ := new(big.Float).SetInt(pool.Reserve0).Float64()
			tvl += r0 / float64(int64(1)<<(token0Info.Decimals*3))
		}
		if token1Info != nil {
			r1, _ := new(big.Float).SetInt(pool.Reserve1).Float64()
			tvl += r1 / float64(int64(1)<<(token1Info.Decimals*3))
		}

		poolsWithTVL[i] = poolWithTVL{pool: pool, tvl: tvl}
	}

	// Sort by TVL descending (simple bubble sort for clarity, can optimize if needed)
	for i := 0; i < len(poolsWithTVL); i++ {
		for j := i + 1; j < len(poolsWithTVL); j++ {
			if poolsWithTVL[j].tvl > poolsWithTVL[i].tvl {
				poolsWithTVL[i], poolsWithTVL[j] = poolsWithTVL[j], poolsWithTVL[i]
			}
		}
	}

	// Extract sorted pools
	result := make([]PoolInfo, len(poolsWithTVL))
	for i, p := range poolsWithTVL {
		result[i] = p.pool
	}

	return result
}

// ConvertToGraphPools converts PoolInfo to graph.PoolState.
func ConvertToGraphPools(pools []PoolInfo) []graph.PoolState {
	result := make([]graph.PoolState, len(pools))
	for i, p := range pools {
		result[i] = graph.PoolState{
			Address:  p.Address,
			Token0:   p.Token0,
			Token1:   p.Token1,
			Reserve0: p.Reserve0,
			Reserve1: p.Reserve1,
			Fee:      p.Fee,
		}
	}
	return result
}

// ConvertToGraphTokens converts TokenInfo map to graph.TokenInfo map.
func ConvertToGraphTokens(tokens map[string]*TokenInfo) map[string]graph.TokenInfo {
	result := make(map[string]graph.TokenInfo, len(tokens))
	for addr, t := range tokens {
		result[addr] = graph.TokenInfo{
			Address:  t.Address,
			Symbol:   t.Symbol,
			Decimals: t.Decimals,
		}
	}
	return result
}

// ConvertToPersistencePools converts PoolInfo to persistence.PoolRecord.
func ConvertToPersistencePools(pools []PoolInfo) []persistence.PoolRecord {
	result := make([]persistence.PoolRecord, len(pools))
	for i, p := range pools {
		result[i] = persistence.PoolRecord{
			Address:  p.Address,
			Token0:   p.Token0,
			Token1:   p.Token1,
			Reserve0: p.Reserve0.String(),
			Reserve1: p.Reserve1.String(),
			Fee:      p.Fee,
			IsStable: p.IsStable,
		}
	}
	return result
}

// ConvertToPersistenceTokens converts TokenInfo map to persistence.TokenRecord slice.
func ConvertToPersistenceTokens(tokens map[string]*TokenInfo) []persistence.TokenRecord {
	result := make([]persistence.TokenRecord, 0, len(tokens))
	for _, t := range tokens {
		result = append(result, persistence.TokenRecord{
			Address:  t.Address,
			Symbol:   t.Symbol,
			Decimals: t.Decimals,
		})
	}
	return result
}
