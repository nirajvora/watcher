package dex

import (
	"context"
	"sync"
	"watcher/pkg/models"
)

type Service struct {
	fetchers []PoolFetcher
}

func NewService(fetchers ...PoolFetcher) *Service {
	return &Service{
		fetchers: fetchers,
	}
}

func (s *Service) FetchAllPools(ctx context.Context) ([]models.Pool, error) {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		allPools []models.Pool
	)

	errorsChan := make(chan error, len(s.fetchers))

	for _, fetcher := range s.fetchers {
		wg.Add(1)
		go func(f PoolFetcher) {
			defer wg.Done()

			pools, err := f.FetchPools(ctx)
			if err != nil {
				errorsChan <- err
				return
			}

			mu.Lock()
			allPools = append(allPools, pools...)
			mu.Unlock()
		}(fetcher)
	}

	wg.Wait()
	close(errorsChan)

	for err := range errorsChan {
		if err != nil {
			return nil, err
		}
	}

	return allPools, nil
}
