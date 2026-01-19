package aerodrome

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"
	"watcher/pkg/chain/base"
	"watcher/pkg/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/shopspring/decimal"
)

const (
	// Aerodrome V2 volatile pool fee is 0.3%
	volatileFeeMultiplier = 0.997

	// Multicall batch size (max calls per multicall request)
	multicallBatchSize = 100

	// Maximum concurrent workers for processing results
	maxWorkers = 20

	// Minimum reserve threshold in wei (1e15 = 0.001 ETH or ~$1-10 worth)
	// Pools with BOTH reserves below this are skipped
	minReserveThreshold = 1e15
)

type Client struct {
	baseClient *base.Client
	tokenCache sync.Map // map[common.Address]*TokenInfo
}

func NewClient(baseClient *base.Client) *Client {
	return &Client{
		baseClient: baseClient,
	}
}

func (c *Client) Name() string {
	return "Aerodrome"
}

func (c *Client) FetchPools(ctx context.Context) ([]models.Pool, error) {
	startTime := time.Now()
	log.Println("Fetching pool count from Aerodrome V2 Factory...")

	// Get total number of pools
	poolCount, err := c.getAllPoolsLength(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool count: %w", err)
	}

	log.Printf("Found %d pools in Aerodrome V2 Factory", poolCount)

	// Fetch all pool addresses using multicall batching
	poolAddresses, err := c.fetchPoolAddressesBatched(ctx, poolCount)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pool addresses: %w", err)
	}

	log.Printf("Fetched %d pool addresses in %v", len(poolAddresses), time.Since(startTime))

	// Fetch pool details using multicall batching
	pools, stats, err := c.fetchPoolDetailsBatched(ctx, poolAddresses)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pool details: %w", err)
	}

	log.Printf("Pool fetching complete in %v", time.Since(startTime))
	log.Printf("Stats: %d valid pools, %d stable (skipped), %d low liquidity (skipped), %d errors",
		stats.valid, stats.stable, stats.lowLiquidity, stats.errors)

	return pools, nil
}

type fetchStats struct {
	valid        int
	stable       int
	lowLiquidity int
	errors       int
}

func (c *Client) getAllPoolsLength(ctx context.Context) (uint64, error) {
	data, err := V2FactoryABI.Pack("allPoolsLength")
	if err != nil {
		return 0, fmt.Errorf("failed to pack allPoolsLength call: %w", err)
	}

	result, err := c.baseClient.CallContract(ctx, V2FactoryAddress, data)
	if err != nil {
		return 0, err
	}

	var length *big.Int
	err = V2FactoryABI.UnpackIntoInterface(&length, "allPoolsLength", result)
	if err != nil {
		return 0, fmt.Errorf("failed to unpack allPoolsLength result: %w", err)
	}

	return length.Uint64(), nil
}

// fetchPoolAddressesBatched fetches all pool addresses using multicall batching
func (c *Client) fetchPoolAddressesBatched(ctx context.Context, count uint64) ([]common.Address, error) {
	addresses := make([]common.Address, 0, count)

	// Process in batches
	for start := uint64(0); start < count; start += multicallBatchSize {
		end := start + multicallBatchSize
		if end > count {
			end = count
		}

		// Build batch of calls
		calls := make([]base.ContractCall, end-start)
		for i := start; i < end; i++ {
			data, err := V2FactoryABI.Pack("allPools", new(big.Int).SetUint64(i))
			if err != nil {
				return nil, fmt.Errorf("failed to pack allPools call: %w", err)
			}
			calls[i-start] = base.ContractCall{
				Target:   V2FactoryAddress,
				CallData: data,
			}
		}

		// Execute batch
		results, err := c.baseClient.BatchCallContract(ctx, calls)
		if err != nil {
			return nil, fmt.Errorf("batch call failed at index %d: %w", start, err)
		}

		// Process results
		for _, result := range results {
			if !result.Success || len(result.Data) == 0 {
				continue
			}
			var addr common.Address
			err := V2FactoryABI.UnpackIntoInterface(&addr, "allPools", result.Data)
			if err != nil {
				continue
			}
			addresses = append(addresses, addr)
		}

		// Progress logging every 1000 pools
		if (start+multicallBatchSize)%1000 < multicallBatchSize {
			log.Printf("Fetched %d/%d pool addresses...", len(addresses), count)
		}
	}

	return addresses, nil
}

// poolBasicInfo holds the basic info fetched in the first multicall batch
type poolBasicInfo struct {
	address  common.Address
	stable   bool
	reserve0 *big.Int
	reserve1 *big.Int
	token0   common.Address
	token1   common.Address
}

// fetchPoolDetailsBatched fetches pool details using multicall batching
func (c *Client) fetchPoolDetailsBatched(ctx context.Context, addresses []common.Address) ([]models.Pool, fetchStats, error) {
	var stats fetchStats

	// First pass: fetch basic pool info (stable, reserves, tokens) in batches
	basicInfos, err := c.fetchBasicPoolInfo(ctx, addresses, &stats)
	if err != nil {
		return nil, stats, err
	}

	// Collect unique tokens that need info
	tokenSet := make(map[common.Address]bool)
	for _, info := range basicInfos {
		tokenSet[info.token0] = true
		tokenSet[info.token1] = true
	}

	// Fetch token info for all unique tokens
	tokens := make([]common.Address, 0, len(tokenSet))
	for token := range tokenSet {
		tokens = append(tokens, token)
	}
	err = c.fetchTokenInfoBatched(ctx, tokens)
	if err != nil {
		log.Printf("Warning: some token info failed to fetch: %v", err)
	}

	// Build final pools
	pools := make([]models.Pool, 0, len(basicInfos))
	for _, info := range basicInfos {
		pool := c.buildPool(info)
		if pool != nil {
			pools = append(pools, *pool)
			stats.valid++
		}
	}

	return pools, stats, nil
}

// fetchBasicPoolInfo fetches stable, reserves, token0, token1 for all pools
func (c *Client) fetchBasicPoolInfo(ctx context.Context, addresses []common.Address, stats *fetchStats) ([]poolBasicInfo, error) {
	validPools := make([]poolBasicInfo, 0, len(addresses))

	// Each pool needs 4 calls: stable, getReserves, token0, token1
	callsPerPool := 4

	// Process pools in batches
	poolsPerBatch := multicallBatchSize / callsPerPool
	if poolsPerBatch < 1 {
		poolsPerBatch = 1
	}

	processed := 0
	for start := 0; start < len(addresses); start += poolsPerBatch {
		end := start + poolsPerBatch
		if end > len(addresses) {
			end = len(addresses)
		}

		batchAddresses := addresses[start:end]
		calls := make([]base.ContractCall, 0, len(batchAddresses)*callsPerPool)

		// Pack calls for each pool: stable, getReserves, token0, token1
		stableData, _ := V2PoolABI.Pack("stable")
		reservesData, _ := V2PoolABI.Pack("getReserves")
		token0Data, _ := V2PoolABI.Pack("token0")
		token1Data, _ := V2PoolABI.Pack("token1")

		for _, addr := range batchAddresses {
			calls = append(calls,
				base.ContractCall{Target: addr, CallData: stableData},
				base.ContractCall{Target: addr, CallData: reservesData},
				base.ContractCall{Target: addr, CallData: token0Data},
				base.ContractCall{Target: addr, CallData: token1Data},
			)
		}

		// Execute batch
		results, err := c.baseClient.BatchCallContract(ctx, calls)
		if err != nil {
			return nil, fmt.Errorf("batch call failed: %w", err)
		}

		// Process results (4 results per pool)
		for i, addr := range batchAddresses {
			baseIdx := i * callsPerPool
			if baseIdx+3 >= len(results) {
				stats.errors++
				continue
			}

			stableResult := results[baseIdx]
			reservesResult := results[baseIdx+1]
			token0Result := results[baseIdx+2]
			token1Result := results[baseIdx+3]

			// Check if any call failed
			if !stableResult.Success || !reservesResult.Success ||
				!token0Result.Success || !token1Result.Success {
				stats.errors++
				continue
			}

			// Parse stable
			var stable bool
			if err := V2PoolABI.UnpackIntoInterface(&stable, "stable", stableResult.Data); err != nil {
				stats.errors++
				continue
			}
			if stable {
				stats.stable++
				continue
			}

			// Parse reserves
			type Reserves struct {
				Reserve0           *big.Int
				Reserve1           *big.Int
				BlockTimestampLast *big.Int
			}
			var reserves Reserves
			if err := V2PoolABI.UnpackIntoInterface(&reserves, "getReserves", reservesResult.Data); err != nil {
				stats.errors++
				continue
			}

			// Skip pools with zero or very low reserves
			if reserves.Reserve0 == nil || reserves.Reserve1 == nil {
				stats.lowLiquidity++
				continue
			}
			if reserves.Reserve0.Sign() == 0 || reserves.Reserve1.Sign() == 0 {
				stats.lowLiquidity++
				continue
			}

			// Check minimum reserve threshold - skip if BOTH are below threshold
			minThreshold := new(big.Int).SetUint64(uint64(minReserveThreshold))
			if reserves.Reserve0.Cmp(minThreshold) < 0 && reserves.Reserve1.Cmp(minThreshold) < 0 {
				stats.lowLiquidity++
				continue
			}

			// Parse token0
			var token0 common.Address
			if err := V2PoolABI.UnpackIntoInterface(&token0, "token0", token0Result.Data); err != nil {
				stats.errors++
				continue
			}

			// Parse token1
			var token1 common.Address
			if err := V2PoolABI.UnpackIntoInterface(&token1, "token1", token1Result.Data); err != nil {
				stats.errors++
				continue
			}

			validPools = append(validPools, poolBasicInfo{
				address:  addr,
				stable:   stable,
				reserve0: reserves.Reserve0,
				reserve1: reserves.Reserve1,
				token0:   token0,
				token1:   token1,
			})
		}

		processed += len(batchAddresses)

		// Progress logging every 500 pools
		if processed%500 < poolsPerBatch {
			log.Printf("Processed %d/%d pools, %d valid so far...", processed, len(addresses), len(validPools))
		}
	}

	return validPools, nil
}

// fetchTokenInfoBatched fetches decimals and symbol for all tokens using multicall
func (c *Client) fetchTokenInfoBatched(ctx context.Context, tokens []common.Address) error {
	// Filter out tokens already in cache
	uncached := make([]common.Address, 0)
	for _, token := range tokens {
		if _, ok := c.tokenCache.Load(token); !ok {
			uncached = append(uncached, token)
		}
	}

	if len(uncached) == 0 {
		return nil
	}

	log.Printf("Fetching info for %d unique tokens...", len(uncached))

	// Each token needs 2 calls: decimals, symbol
	callsPerToken := 2

	// Process in batches
	tokensPerBatch := multicallBatchSize / callsPerToken
	if tokensPerBatch < 1 {
		tokensPerBatch = 1
	}

	decimalsData, _ := ERC20ABI.Pack("decimals")
	symbolData, _ := ERC20ABI.Pack("symbol")

	for start := 0; start < len(uncached); start += tokensPerBatch {
		end := start + tokensPerBatch
		if end > len(uncached) {
			end = len(uncached)
		}

		batchTokens := uncached[start:end]
		calls := make([]base.ContractCall, 0, len(batchTokens)*callsPerToken)

		for _, token := range batchTokens {
			calls = append(calls,
				base.ContractCall{Target: token, CallData: decimalsData},
				base.ContractCall{Target: token, CallData: symbolData},
			)
		}

		results, err := c.baseClient.BatchCallContract(ctx, calls)
		if err != nil {
			// Log but continue - we can use defaults
			log.Printf("Warning: token batch call failed: %v", err)
			continue
		}

		// Process results
		for i, token := range batchTokens {
			baseIdx := i * callsPerToken
			if baseIdx+1 >= len(results) {
				continue
			}

			decimalsResult := results[baseIdx]
			symbolResult := results[baseIdx+1]

			var decimals uint8 = 18 // default
			var symbol string = token.Hex()[:10] + "..." // default

			if decimalsResult.Success && len(decimalsResult.Data) > 0 {
				if err := ERC20ABI.UnpackIntoInterface(&decimals, "decimals", decimalsResult.Data); err != nil {
					decimals = 18
				}
			}

			if symbolResult.Success && len(symbolResult.Data) > 0 {
				if err := ERC20ABI.UnpackIntoInterface(&symbol, "symbol", symbolResult.Data); err != nil {
					symbol = token.Hex()[:10] + "..."
				}
			}

			c.tokenCache.Store(token, &TokenInfo{
				Address:  token,
				Symbol:   symbol,
				Decimals: decimals,
			})
		}
	}

	return nil
}

// buildPool creates a Pool from basic info, using cached token info
func (c *Client) buildPool(info poolBasicInfo) *models.Pool {
	// Get token info from cache
	token0InfoRaw, ok := c.tokenCache.Load(info.token0)
	if !ok {
		return nil
	}
	token0Info := token0InfoRaw.(*TokenInfo)

	token1InfoRaw, ok := c.tokenCache.Load(info.token1)
	if !ok {
		return nil
	}
	token1Info := token1InfoRaw.(*TokenInfo)

	// Calculate exchange rates with decimal normalization and fee
	liquidity0 := decimalFromBigInt(info.reserve0, token0Info.Decimals)
	liquidity1 := decimalFromBigInt(info.reserve1, token1Info.Decimals)

	// Double-check for zero liquidity to prevent division by zero
	if liquidity0.IsZero() || liquidity1.IsZero() {
		return nil
	}

	// Exchange rate = liquidity1 / liquidity0, adjusted for fee
	feeMultiplier := decimal.NewFromFloat(volatileFeeMultiplier)
	exchangeRate := liquidity1.Div(liquidity0).Mul(feeMultiplier)
	reciprocalRate := liquidity0.Div(liquidity1).Mul(feeMultiplier)

	// Validate exchange rates are positive and finite
	rate, _ := exchangeRate.Float64()
	recipRate, _ := reciprocalRate.Float64()
	if rate <= 0 || recipRate <= 0 {
		return nil
	}

	return &models.Pool{
		Address:                info.address.Hex(),
		Exchange:               "Aerodrome",
		Chain:                  "Base",
		Asset1ID:               strings.ToLower(info.token0.Hex()),
		Asset1Name:             token0Info.Symbol,
		Asset1Decimals:         token0Info.Decimals,
		Asset2ID:               strings.ToLower(info.token1.Hex()),
		Asset2Name:             token1Info.Symbol,
		Asset2Decimals:         token1Info.Decimals,
		Liquidity1:             liquidity0,
		Liquidity2:             liquidity1,
		Fee:                    feeMultiplier,
		ExchangeRate:           exchangeRate,
		ReciprocalExchangeRate: reciprocalRate,
	}
}

// decimalFromBigInt converts a big.Int value to a decimal, adjusting for token decimals
func decimalFromBigInt(value *big.Int, decimals uint8) decimal.Decimal {
	if value == nil {
		return decimal.Zero
	}
	d := decimal.NewFromBigInt(value, 0)
	divisor := decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(decimals)))
	if divisor.IsZero() {
		return decimal.Zero
	}
	return d.Div(divisor)
}
