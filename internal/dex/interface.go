package dex

import (
    "context"
    "watcher/internal/models"
)

type PoolFetcher interface {
    FetchPools(ctx context.Context) ([]models.Pool, error)
    Name() string
}