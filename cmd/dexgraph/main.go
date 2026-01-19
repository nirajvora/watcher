package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"watcher/pkg/chain/base"
	"watcher/pkg/config"
	"watcher/pkg/db"
	"watcher/pkg/dex"
	"watcher/pkg/dex/aerodrome"
)

// Base chain token addresses for filtering
var (
	// WETH on Base
	WETHAddress = "0x4200000000000000000000000000000000000006"
	// USDC on Base (native)
	USDCAddress = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	// USDbC on Base (bridged)
	USDbCAddress = "0xd9aaec86b65d86f6a7b5b1b0c42ffa531710b6ca"
)

func main() {
	// Setup context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	log.Println("Loading configuration...")
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Initializing database connection...")
	database, err := db.NewGraphDB(db.Neo4jConfig{
		URI:      cfg.Neo4jURI,
		Username: cfg.Neo4jUsername,
		Password: cfg.Neo4jPassword,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close(ctx)

	log.Println("Setting up database schema...")
	if err := database.SetupSchema(ctx); err != nil {
		log.Fatal(err)
	}

	log.Println("Connecting to Base chain...")
	baseClient, err := base.NewClient(cfg.BaseRPCURL)
	if err != nil {
		log.Fatal(err)
	}
	defer baseClient.Close()

	log.Println("Initializing DEX service with Aerodrome...")
	aerodromeClient := aerodrome.NewClient(baseClient)
	dexService := dex.NewService(aerodromeClient)

	log.Println("Fetching pools from Aerodrome V2...")
	startTime := time.Now()
	pools, err := dexService.FetchAllPools(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Fetched %d pools in %v", len(pools), time.Since(startTime))

	log.Println("Storing pools in database...")
	stored := 0
	for _, pool := range pools {
		if err := database.StorePool(ctx, pool); err != nil {
			log.Printf("Failed to store pool %s: %v", pool.Address, err)
			continue
		}
		stored++
		if stored%100 == 0 {
			log.Printf("Stored %d pools so far...", stored)
		}
	}
	log.Printf("Successfully processed %d pools", stored)

	log.Println("Finding Arbitrage Opportunities")
	limit := 5
	uniqueCycles, err := database.FindArbPaths(ctx, limit)
	if err != nil {
		log.Printf("Failed to find arbs: %v", err)
	}

	log.Println("Filtering for desired Opportunities -- Starting with WETH, USDC or USDbC")
	filteredCycles := db.FilterArbPathsByStartAssetIds(uniqueCycles, []string{
		strings.ToLower(WETHAddress),
		strings.ToLower(USDCAddress),
		strings.ToLower(USDbCAddress),
	})

	for cycleKey, cycle := range filteredCycles {
		profitFactor := strings.Split(cycleKey, ":")[0]
		startLiquidity := db.CalculateMaxCycleLiquidity(cycle)
		log.Printf("Found desired cycle with profit factor: %s and start liquidity: %s", profitFactor, startLiquidity)
		cycleText := ""
		for _, pool := range cycle {
			cycleText += fmt.Sprintf("%s -> ", pool.SourceAssetName)
		}
		cycleText += cycle[len(cycle)-1].TargetAssetName
		log.Printf("%s\n", cycleText)
	}
}
