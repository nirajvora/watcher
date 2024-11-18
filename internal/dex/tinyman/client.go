package tinyman

import (
    "context"
    "fmt"
	"strconv"
    "strings"
    "sync"
    "time"
    "watcher/internal/client"
    "watcher/internal/models"
    "golang.org/x/time/rate"
)

const (
    baseURL   = "https://mainnet.analytics.tinyman.org/api/v1/pools"
    batchSize = 10
    
    // Rate limiting constants
    requestsPerSecond = 2    // Limit to 2 requests per second
    burstSize        = 1    // No bursting allowed
    retryAttempts    = 3    // Number of retries for rate-limited requests
    retryDelay       = time.Second * 2 // Wait between retries
)

// Response types for Tinyman API
type Asset struct {
    ID                             string  `json:"id"`
    IsLiquidityToken               bool    `json:"is_liquidity_token"`
    Name                           string  `json:"name"`
    UnitName                       string  `json:"unit_name"`
    Decimals                       int     `json:"decimals"`
    TotalAmount                    string  `json:"total_amount"`
    URL                            string  `json:"url"`
    IsVerified                     bool    `json:"is_verified"`
    ClawbackAddress                string  `json:"clawback_address"`
    IsFolksLendingAsset            bool    `json:"is_folks_lending_asset"`
    FolksLendingPairAsset          *string `json:"folks_lending_pair_asset"`
    FolksLendingPoolApplicationID  *int    `json:"folks_lending_pool_application_id"`
}

type PoolsResponse struct {
    Count    int           `json:"count"`
    Next     string        `json:"next"`
    Previous string        `json:"previous"`
    Results  []TinymanPool `json:"results"`
}

type TinymanPool struct {
    Address                            string   `json:"address"`
    Version                            string   `json:"version"`
    Asset1                             Asset    `json:"asset_1"`
    Asset2                             Asset    `json:"asset_2"`
    LiquidityAsset                     Asset    `json:"liquidity_asset"`
    IsVerified                         bool     `json:"is_verified"`
    IsStable                           bool     `json:"is_stable"`
    IsAsset1ClawbackUtilized           bool     `json:"is_asset_1_clawback_utilized"`
    IsAsset2ClawbackUtilized           bool     `json:"is_asset_2_clawback_utilized"`
    CurrentAsset1Reserves              *string  `json:"current_asset_1_reserves"`
    CurrentAsset2Reserves              *string  `json:"current_asset_2_reserves"`
    CurrentIssuedLiquidityAssets       string   `json:"current_issued_liquidity_assets"`
    CurrentUnclaimedProtocolFees       string   `json:"current_unclaimed_protocol_fees"`
    UnclaimedAsset1ProtocolFees        *string  `json:"unclaimed_asset_1_protocol_fees"`
    UnclaimedAsset2ProtocolFees        *string  `json:"unclaimed_asset_2_protocol_fees"`
    CurrentAsset1ReservesInUSD         *string  `json:"current_asset_1_reserves_in_usd"`    // Changed to *string
    CurrentAsset2ReservesInUSD         *string  `json:"current_asset_2_reserves_in_usd"`    // Changed to *string
    V2Address                          *string  `json:"v2_address"`
    CreationRound                      string   `json:"creation_round"`
    CreationDatetime                   string   `json:"creation_datetime"`
    LiquidityInUSD                     *string  `json:"liquidity_in_usd"`                   // Changed to *string
    LastDayVolumeInUSD                 string   `json:"last_day_volume_in_usd"`
    LastWeekVolumeInUSD                string   `json:"last_week_volume_in_usd"`
    LastDayFeesInUSD                   string   `json:"last_day_fees_in_usd"`
    ActiveStakingProgramCount          int      `json:"active_staking_program_count"`
    AnnualPercentageRate               *string  `json:"annual_percentage_rate"`              // Changed to *string
    AnnualPercentageYield              *string  `json:"annual_percentage_yield"`             // Changed to *string
    FolksLendingAnnualPercentageRate   *string  `json:"folks_lending_annual_percentage_rate"`// Changed to *string
    FolksLendingAnnualPercentageYield  *string  `json:"folks_lending_annual_percentage_yield"`// Changed to *string
    StakingTotalAnnualPercentageRate   *string  `json:"staking_total_annual_percentage_rate"`// Changed to *string
    StakingTotalAnnualPercentageYield  *string  `json:"staking_total_annual_percentage_yield"`// Changed to *string
    TotalAnnualPercentageRate          *string  `json:"total_annual_percentage_rate"`        // Changed to *string
    TotalAnnualPercentageYield         *string  `json:"total_annual_percentage_yield"`       // Changed to *string
    IsFolksLendingPool                 bool     `json:"is_folks_lending_pool"`
}

type Client struct {
    httpClient *client.HTTPClient
    rateLimiter *rate.Limiter
}

func NewClient() *Client {
    return &Client{
        httpClient: client.NewHTTPClient(),
        rateLimiter: rate.NewLimiter(rate.Limit(requestsPerSecond), burstSize),
    }
}

func (c *Client) Name() string {
    return "Tinyman"
}

func (c *Client) FetchPools(ctx context.Context) ([]models.Pool, error) {
    // Wait for rate limiter before initial request
    err := c.rateLimiter.Wait(ctx)
    if err != nil {
        return nil, fmt.Errorf("rate limiter wait: %w", err)
    }

    var initial PoolsResponse
    err = c.httpClient.Get(ctx, fmt.Sprintf("%s?limit=%d&offset=0&verified_only=true", baseURL, batchSize), &initial)
    if err != nil {
        return nil, fmt.Errorf("getting initial pool data: %w", err)
    }

    totalPools := initial.Count
    poolCount := (totalPools + batchSize - 1) / batchSize
    
    poolsChan := make(chan []TinymanPool, poolCount)
    errorsChan := make(chan error, poolCount)
    
    var wg sync.WaitGroup
    
    // Semaphore to limit concurrent requests
    sem := make(chan struct{}, 5) // Limit concurrent requests to 5

    for offset := 0; offset < totalPools; offset += batchSize {
        wg.Add(1)
        go func(offset int) {
            defer wg.Done()
            
            // Acquire semaphore
            sem <- struct{}{}
            defer func() { <-sem }()

            // Try multiple times in case of rate limiting
            for attempt := 0; attempt < retryAttempts; attempt++ {
                // Wait for rate limiter
                if err := c.rateLimiter.Wait(ctx); err != nil {
                    errorsChan <- fmt.Errorf("rate limiter wait: %w", err)
                    return
                }

                var resp PoolsResponse
                url := fmt.Sprintf("%s?limit=%d&offset=%d&verified_only=true", baseURL, batchSize, offset)
                
                err := c.httpClient.Get(ctx, url, &resp)
                if err != nil {
                    if attempt < retryAttempts-1 && isRateLimitError(err) {
                        // Wait before retrying
                        select {
                        case <-ctx.Done():
                            errorsChan <- ctx.Err()
                            return
                        case <-time.After(retryDelay):
                            continue
                        }
                    }
                    errorsChan <- fmt.Errorf("fetching pools at offset %d: %w", offset, err)
                    return
                }

                poolsChan <- resp.Results
                return
            }
            
            errorsChan <- fmt.Errorf("max retry attempts reached for offset %d", offset)
        }(offset)
    }
    
    go func() {
        wg.Wait()
        close(poolsChan)
        close(errorsChan)
    }()
    
    // Check for errors
    var lastError error
    for err := range errorsChan {
        if err != nil {
            lastError = err
        }
    }
    if lastError != nil {
        return nil, lastError
    }
    
    var allPools []models.Pool
    for pools := range poolsChan {
        for _, p := range pools {
            // Skip pools with no reserves or null reserves
            if p.CurrentAsset1Reserves == nil || p.CurrentAsset2Reserves == nil {
                continue
            }

            // Convert string reserves to float64
            reserves1, err := strconv.ParseFloat(*p.CurrentAsset1Reserves, 64)
            if err != nil {
                continue
            }
            reserves2, err := strconv.ParseFloat(*p.CurrentAsset2Reserves, 64)
            if err != nil {
                continue
            }

            // Calculate exchange rate if possible
            var exchangeRate float64
            if reserves2 > 0 {
                exchangeRate = reserves1 / reserves2
            }

            pool := models.Pool{
                ID:           p.Address,
                Exchange:     c.Name(),
                Asset1:       p.Asset1.ID,
                Asset2:       p.Asset2.ID,
                Liquidity1:   reserves1,
                Liquidity2:   reserves2,
                ExchangeRate: exchangeRate,
            }
            allPools = append(allPools, pool)
        }
    }

    return allPools, nil
}

// isRateLimitError checks if the error is due to rate limiting
func isRateLimitError(err error) bool {
    return err != nil && strings.Contains(err.Error(), "429")
}