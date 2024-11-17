package tinyman

import (
    "context"
    "fmt"
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
    err = c.httpClient.Get(ctx, fmt.Sprintf("%s?limit=%d&offset=0", baseURL, batchSize), &initial)
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
                url := fmt.Sprintf("%s?limit=%d&offset=%d", baseURL, batchSize, offset)
                
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
            pool := models.Pool{
                ID:           p.ID,
                Exchange:     c.Name(),
                Asset1:       p.Asset1ID,
                Asset2:       p.Asset2ID,
                Liquidity1:   p.Asset1Amount,
                Liquidity2:   p.Asset2Amount,
                ExchangeRate: p.ExchangeRate,
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