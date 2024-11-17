package client

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

type HTTPClient struct {
    client *http.Client
}

func NewHTTPClient() *HTTPClient {
    return &HTTPClient{
        client: &http.Client{
            Timeout: 10 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 100,
                IdleConnTimeout:     90 * time.Second,
            },
        },
    }
}

func (c *HTTPClient) Get(ctx context.Context, url string, response interface{}) error {
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return fmt.Errorf("creating request: %w", err)
    }

    resp, err := c.client.Do(req)
    if err != nil {
        return fmt.Errorf("executing request: %w", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("reading response body: %w", err)
    }

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
    }

    if err := json.Unmarshal(body, response); err != nil {
        return fmt.Errorf("unmarshaling response: %w", err)
    }

    return nil
}