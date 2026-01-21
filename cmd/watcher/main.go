package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"watcher/internal/config"
	"watcher/internal/curator"
	"watcher/internal/detector"
	"watcher/internal/graph"
	"watcher/internal/ingestion"
	"watcher/internal/metrics"
	"watcher/internal/persistence"
	"watcher/pkg/chain/base"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "configs/config.yaml", "Path to configuration file")
	flag.Parse()

	// Load .env file
	if err := godotenv.Load(); err != nil {
		// .env file is optional
		log.Debug().Msg("No .env file found, using environment variables")
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Setup logging
	setupLogging(cfg.Logging)
	log.Info().Msg("Starting Watcher - Real-time Arbitrage Detection System")

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
	}()

	// Initialize components
	if err := run(ctx, cfg); err != nil && err != context.Canceled {
		log.Fatal().Err(err).Msg("Application error")
	}

	log.Info().Msg("Watcher shutdown complete")
}

func run(ctx context.Context, cfg *config.Config) error {
	// Initialize metrics
	m := metrics.New()
	if cfg.Metrics.Enabled {
		if err := m.StartServer(cfg.Metrics.Port, cfg.Metrics.Path); err != nil {
			return err
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m.Shutdown(shutdownCtx)
		}()
		log.Info().Int("port", cfg.Metrics.Port).Msg("Metrics server started")
	}

	// Initialize persistence
	store, err := persistence.NewStore(cfg.Persistence.SQLitePath)
	if err != nil {
		return err
	}
	defer store.Close()
	log.Info().Str("path", cfg.Persistence.SQLitePath).Msg("SQLite initialized")

	// Initialize RPC client
	rpcClient, err := base.NewClient(cfg.Chain.RPCURL)
	if err != nil {
		return err
	}
	log.Info().Msg("RPC client connected")

	// Initialize graph manager
	graphManager := graph.NewManager(m)
	defer graphManager.Close()

	// Initialize ingestion service
	ingestionSvc := ingestion.NewService(
		cfg.Chain.WSURL,
		cfg.Contracts.AerodromeFactory,
		graphManager,
		m,
	)

	// Initialize curator
	curatorSvc := curator.NewCurator(
		curator.Config{
			FactoryAddress:       cfg.Contracts.AerodromeFactory,
			TopPoolsCount:        cfg.Curator.TopPoolsCount,
			MinTVLUSD:            cfg.Curator.MinTVLUSD,
			ReevaluationInterval: cfg.Curator.ReevaluationInterval,
			BootstrapBatchSize:   cfg.Curator.BootstrapBatchSize,
			StartTokens:          cfg.Detector.StartTokens, // Ensure pools with start tokens are always included
		},
		rpcClient,
		store,
		graphManager,
		m,
		ingestionSvc,
	)

	// Initialize detector
	detectorSvc := detector.NewDetector(
		detector.Config{
			MinProfitFactor: cfg.Detector.MinProfitFactor,
			MaxPathLength:   cfg.Detector.MaxPathLength,
			NumWorkers:      cfg.Detector.NumWorkers,
			StartTokens:     cfg.Detector.StartTokens,
		},
		graphManager.SnapshotCh(),
		m,
	)

	// Bootstrap pools
	log.Info().Msg("Starting bootstrap...")
	bootstrapCtx, bootstrapCancel := context.WithTimeout(ctx, 10*time.Minute)
	if err := curatorSvc.Bootstrap(bootstrapCtx); err != nil {
		bootstrapCancel()
		return err
	}
	bootstrapCancel()

	nodes, edges, pools := graphManager.Stats()
	log.Info().
		Int("nodes", nodes).
		Int("edges", edges).
		Int("pools", pools).
		Msg("Graph initialized")

	// Validate graph consistency
	if !graphManager.Graph().ValidateAndLog() {
		log.Warn().Msg("Graph validation failed - continuing but some cycles may be missed")
	}

	// Create initial snapshot and run detection once
	log.Info().Msg("Running initial detection...")
	initialSnap := graphManager.GetCurrentSnapshot(0)
	opportunities := detectorSvc.DetectOnce(initialSnap)
	if len(opportunities) > 0 {
		log.Info().Int("count", len(opportunities)).Msg("Initial detection found opportunities")
	} else {
		log.Info().Msg("No arbitrage opportunities found in initial scan")
	}

	// Start all services
	g, gCtx := errgroup.WithContext(ctx)

	// Start ingestion service
	g.Go(func() error {
		log.Info().Msg("Starting ingestion service...")
		return ingestionSvc.Run(gCtx)
	})

	// Start detector
	g.Go(func() error {
		log.Info().Msg("Starting detector...")
		return detectorSvc.Run(gCtx)
	})

	// Start curator (background re-evaluation)
	g.Go(func() error {
		log.Info().Msg("Starting curator...")
		return curatorSvc.Run(gCtx)
	})

	// Start opportunity logger
	g.Go(func() error {
		return logOpportunities(gCtx, detectorSvc.Opportunities(), m)
	})

	// Wait for all goroutines
	if err := g.Wait(); err != nil && err != context.Canceled {
		return err
	}

	return nil
}

func setupLogging(cfg config.LoggingConfig) {
	// Set log level
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Set output format
	if cfg.Format == "json" {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	} else {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}).With().Timestamp().Logger()
	}
}

func logOpportunities(ctx context.Context, ch <-chan *detector.Opportunity, m *metrics.Metrics) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case opp, ok := <-ch:
			if !ok {
				return nil
			}

			// Log detailed opportunity information
			pathSymbols := make([]string, len(opp.Path))
			for i, t := range opp.Path {
				if t.Symbol != "" {
					pathSymbols[i] = t.Symbol
				} else {
					pathSymbols[i] = t.Address[:10] + "..."
				}
			}

			log.Info().
				Strs("path", pathSymbols).
				Strs("pools", opp.Pools).
				Float64("profit_factor", opp.ProfitFactor).
				Str("max_input", opp.MaxInputWei.String()).
				Str("estimated_profit", opp.EstimatedProfitWei.String()).
				Uint64("block", opp.DetectedAtBlock).
				Dur("detection_latency", opp.DetectionLatency).
				Msg("ARBITRAGE OPPORTUNITY DETECTED")

			// Record pipeline latency
			if m != nil {
				m.RecordPipelineLatency(opp.DetectionLatency)
			}
		}
	}
}
