package tinyman

import (
    "context"
    "fmt"
    "sync"
    "watcher/internal/client"
    "watcher/internal/models"
)

const (
    baseURL = "https://mainnet.analytics.tinyman.org/api/v1/pools"
    batchSize = 10
)

type Client struct {
    httpClient *client.HTTPClient
}

type PoolsResponse struct {
    Count   int           `json:"count"`
    Results []TinymanPool `json:"results"`
}

type TinymanPool struct {
    ID            string  `json:"id"`
    Asset1ID      string  `json:"asset_1_id"`
    Asset2ID      string  `json:"asset_2_id"`
    Asset1Amount  float64 `json:"asset_1_reserves"`
    Asset2Amount  float64 `json:"asset_2_reserves"`
    ExchangeRate  float64 `json:"current_price"`
}

func NewClient() *Client {
    return &Client{
        httpClient: client.NewHTTPClient(),
    }
}

func (c *Client) Name() string {
    return "Tinyman"
}

func (c *Client) FetchPools(ctx context.Context) ([]models.Pool, error) {
    var initial PoolsResponse
    err := c.httpClient.Get(ctx, fmt.Sprintf("%s?limit=%d&offset=0", baseURL, batchSize), &initial)
    if err != nil {
        return nil, fmt.Errorf("getting initial pool data: %w", err)
    }

    totalPools := initial.Count
    poolCount := (totalPools + batchSize - 1) / batchSize
    
    poolsChan := make(chan []TinymanPool, poolCount)
    errorsChan := make(chan error, poolCount)
    
    var wg sync.WaitGroup
    
    for offset := 0; offset < totalPools; offset += batchSize {
        wg.Add(1)
        go func(offset int) {
            defer wg.Done()
            
            var resp PoolsResponse
            url := fmt.Sprintf("%s?limit=%d&offset=%d", baseURL, batchSize, offset)
            
            if err := c.httpClient.Get(ctx, url, &resp); err != nil {
                errorsChan <- fmt.Errorf("fetching pools at offset %d: %w", offset, err)
                return
            }
            
            poolsChan <- resp.Results
        }(offset)
    }
    
    go func() {
        wg.Wait()
        close(poolsChan)
        close(errorsChan)
    }()
    
    for err := range errorsChan {
        if err != nil {
            return nil, err
        }
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
