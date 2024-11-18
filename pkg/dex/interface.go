package dex

import (
    "context"
    "watcher/pkg/models"
)

type PoolFetcher interface {
    FetchPools(ctx context.Context) ([]models.Pool, error)
    Name() string
}