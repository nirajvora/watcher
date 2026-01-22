package detector

import (
	"context"
	"math/big"
	"sync"
	"time"

	"watcher/internal/graph"
	"watcher/internal/metrics"

	"github.com/rs/zerolog/log"
)

// Opportunity represents a detected arbitrage opportunity.
type Opportunity struct {
	// Path is the ordered list of tokens in the arbitrage path
	Path []graph.TokenInfo

	// Pools contains the pool addresses for each hop (len = len(Path) - 1)
	Pools []string

	// MaxInputWei is the maximum input amount in wei
	MaxInputWei *big.Int

	// ProfitFactor represents the multiplication factor (>1 means profitable)
	ProfitFactor float64

	// EstimatedProfitWei is the estimated profit in wei of the starting token
	EstimatedProfitWei *big.Int

	// DetectedAtBlock is the block number when this opportunity was detected
	DetectedAtBlock uint64

	// DetectionLatency is the time from snapshot creation to opportunity detection
	DetectionLatency time.Duration

	// Cycle contains the raw cycle data
	Cycle *Cycle
}

// Detector runs arbitrage detection on graph snapshots.
type Detector struct {
	config  Config
	metrics *metrics.Metrics

	// Start tokens (where arbitrage must start and end)
	startTokens   []string
	startTokenIdx map[int]bool

	// Results channel
	opportunitiesCh chan *Opportunity

	// Snapshot processing
	snapshotCh <-chan *graph.Snapshot
}

// Config holds detector configuration.
type Config struct {
	MinProfitFactor float64
	MaxPathLength   int
	NumWorkers      int
	StartTokens     []string
}

// NewDetector creates a new arbitrage detector.
func NewDetector(cfg Config, snapshotCh <-chan *graph.Snapshot, m *metrics.Metrics) *Detector {
	return &Detector{
		config:          cfg,
		metrics:         m,
		startTokens:     cfg.StartTokens,
		startTokenIdx:   make(map[int]bool),
		opportunitiesCh: make(chan *Opportunity, 100),
		snapshotCh:      snapshotCh,
	}
}

// Opportunities returns the channel for detected opportunities.
func (d *Detector) Opportunities() <-chan *Opportunity {
	return d.opportunitiesCh
}

// Run starts the detector, processing snapshots from the channel.
func (d *Detector) Run(ctx context.Context) error {
	log.Info().
		Int("workers", d.config.NumWorkers).
		Float64("min_profit", d.config.MinProfitFactor).
		Int("max_path", d.config.MaxPathLength).
		Strs("start_tokens", d.config.StartTokens).
		Msg("Starting detector")

	for {
		select {
		case <-ctx.Done():
			close(d.opportunitiesCh)
			return ctx.Err()

		case snap, ok := <-d.snapshotCh:
			if !ok {
				close(d.opportunitiesCh)
				return nil
			}

			d.processSnapshot(ctx, snap)
		}
	}
}

// processSnapshot processes a single snapshot for arbitrage opportunities.
func (d *Detector) processSnapshot(ctx context.Context, snap *graph.Snapshot) {
	startTime := time.Now()

	// Update start token indices for this snapshot
	d.updateStartTokenIndices(snap)

	if len(d.startTokenIdx) == 0 {
		log.Warn().Msg("No start tokens found in snapshot")
		return
	}

	// Run detection from each start token in parallel
	var wg sync.WaitGroup
	cycleSet := NewCycleSet()
	var cycleMu sync.Mutex

	// Create worker pool
	workCh := make(chan int, len(d.startTokenIdx))
	for idx := range d.startTokenIdx {
		workCh <- idx
	}
	close(workCh)

	numWorkers := d.config.NumWorkers
	if numWorkers > len(d.startTokenIdx) {
		numWorkers = len(d.startTokenIdx)
	}

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sourceIdx := range workCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Find negative cycles from this source
				cycleEdges := FindNegativeCycleContaining(snap, sourceIdx, d.config.MaxPathLength)
				if len(cycleEdges) > 0 && ValidateCycle(cycleEdges) {
					cycle := NewCycle(cycleEdges)
					if cycle != nil && cycle.IsProfitable(d.config.MinProfitFactor) {
						cycleMu.Lock()
						cycleSet.Add(cycle)
						cycleMu.Unlock()
					}
				}
			}
		}()
	}

	wg.Wait()

	detectionDuration := time.Since(startTime)

	// Record metrics
	if d.metrics != nil {
		d.metrics.RecordDetectionLatency(detectionDuration)
	}

	// Process found cycles
	cycles := cycleSet.GetProfitable(d.config.MinProfitFactor)
	if len(cycles) > 0 {
		if d.metrics != nil {
			for range cycles {
				d.metrics.RecordCycleFound()
			}
		}

		log.Info().
			Uint64("block", snap.BlockNumber).
			Int("cycles_found", len(cycles)).
			Dur("detection_time", detectionDuration).
			Int("nodes", snap.NumNodes()).
			Int("edges", snap.NumEdges()).
			Msg("Detection complete - cycles found, running simulation")

		// Simulate and create opportunities
		simulatedCount := 0
		profitableCount := 0
		for _, cycle := range cycles {
			simulatedCount++
			opp := d.createOpportunity(snap, cycle, detectionDuration)
			if opp != nil {
				profitableCount++
				select {
				case d.opportunitiesCh <- opp:
					if d.metrics != nil {
						d.metrics.RecordProfitableOpportunity()
					}
					d.logOpportunity(opp)
				default:
					log.Warn().Msg("Opportunity channel full")
				}
			}
		}

		// Log simulation summary if some cycles didn't pass simulation
		if simulatedCount > profitableCount {
			log.Info().
				Uint64("block", snap.BlockNumber).
				Int("cycles_simulated", simulatedCount).
				Int("profitable_after_simulation", profitableCount).
				Msg("Simulation filtered out unprofitable cycles")
		}
	} else {
		log.Info().
			Uint64("block", snap.BlockNumber).
			Dur("detection_time", detectionDuration).
			Int("nodes", snap.NumNodes()).
			Int("edges", snap.NumEdges()).
			Int("start_tokens", len(d.startTokenIdx)).
			Msg("Detection complete - no arbitrage found")
	}
}

// updateStartTokenIndices updates the map of start token indices for the current snapshot.
func (d *Detector) updateStartTokenIndices(snap *graph.Snapshot) {
	d.startTokenIdx = make(map[int]bool)

	for _, addr := range d.startTokens {
		if idx, exists := snap.GetTokenIndex(addr); exists {
			d.startTokenIdx[idx] = true
		}
	}
}

// createOpportunity creates an Opportunity from a cycle.
func (d *Detector) createOpportunity(snap *graph.Snapshot, cycle *Cycle, detectionTime time.Duration) *Opportunity {
	// Simulate to get actual profit
	result := SimulateCycle(cycle, snap, d.config.MinProfitFactor)
	if result == nil || !result.IsProfitable {
		return nil
	}

	// Build token path
	tokenIndices := cycle.TokenIndices()
	path := make([]graph.TokenInfo, len(tokenIndices))
	for i, idx := range tokenIndices {
		token, ok := snap.GetToken(idx)
		if !ok {
			return nil
		}
		path[i] = token
	}

	return &Opportunity{
		Path:               path,
		Pools:              cycle.PoolAddresses(),
		MaxInputWei:        result.MaxInputWei,
		ProfitFactor:       result.ProfitFactor,
		EstimatedProfitWei: result.EstimatedProfitWei,
		DetectedAtBlock:    snap.BlockNumber,
		DetectionLatency:   detectionTime,
		Cycle:              cycle,
	}
}

// logOpportunity logs a detected opportunity.
func (d *Detector) logOpportunity(opp *Opportunity) {
	// Build path string with symbols
	pathParts := make([]string, len(opp.Path))
	for i, t := range opp.Path {
		if t.Symbol != "" {
			pathParts[i] = t.Symbol
		} else {
			pathParts[i] = t.Address[:10] + "..."
		}
	}

	// Calculate profit percentage
	profitPercent := (opp.ProfitFactor - 1.0) * 100.0

	// Format max input in human-readable form (divide by 1e18 for ETH-like tokens)
	maxInputEth := new(big.Float).SetInt(opp.MaxInputWei)
	maxInputEth.Quo(maxInputEth, big.NewFloat(1e18))
	maxInputStr, _ := maxInputEth.Float64()

	// Format estimated profit
	profitEth := new(big.Float).SetInt(opp.EstimatedProfitWei)
	profitEth.Quo(profitEth, big.NewFloat(1e18))
	profitStr, _ := profitEth.Float64()

	log.Info().
		Uint64("block", opp.DetectedAtBlock).
		Strs("path", pathParts).
		Strs("pools", opp.Pools).
		Float64("profit_factor", opp.ProfitFactor).
		Float64("profit_percent", profitPercent).
		Float64("max_input", maxInputStr).
		Float64("estimated_profit", profitStr).
		Str("max_input_wei", opp.MaxInputWei.String()).
		Str("profit_wei", opp.EstimatedProfitWei.String()).
		Dur("detection_latency", opp.DetectionLatency).
		Int("path_length", len(opp.Path)-1).
		Msg("🎯 ARBITRAGE OPPORTUNITY DETECTED")
}

// DetectOnce runs detection once on a given snapshot (for testing/benchmarking).
func (d *Detector) DetectOnce(snap *graph.Snapshot) []*Opportunity {
	startTime := time.Now()
	d.updateStartTokenIndices(snap)

	if len(d.startTokenIdx) == 0 {
		return nil
	}

	cycleSet := NewCycleSet()

	for sourceIdx := range d.startTokenIdx {
		cycleEdges := FindNegativeCycleContaining(snap, sourceIdx, d.config.MaxPathLength)
		if len(cycleEdges) > 0 && ValidateCycle(cycleEdges) {
			cycle := NewCycle(cycleEdges)
			if cycle != nil && cycle.IsProfitable(d.config.MinProfitFactor) {
				cycleSet.Add(cycle)
			}
		}
	}

	detectionDuration := time.Since(startTime)
	cycles := cycleSet.GetProfitable(d.config.MinProfitFactor)

	var opportunities []*Opportunity
	for _, cycle := range cycles {
		opp := d.createOpportunity(snap, cycle, detectionDuration)
		if opp != nil {
			opportunities = append(opportunities, opp)
		}
	}

	return opportunities
}
