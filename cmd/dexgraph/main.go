package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"watcher/pkg/db"
	"watcher/pkg/dex"
	"watcher/pkg/dex/tinyman"
)

func main() {
	ctx := context.Background()
	username, password, uri := db.SetConnectionParams()
	log.Println("Initializing database connection...")
	database, err := db.NewGraphDB(db.Neo4jConfig{
		URI:      uri,
		Username: username,
		Password: password,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close(ctx)

	log.Println("Setting up database schema...")
	if err := database.SetupSchema(ctx); err != nil {
		log.Fatal(err)
	}

	log.Println("Initializing DEX service...")
	dexService := dex.NewService(
		tinyman.NewClient(),
	)

	log.Println("Fetching pools from all DEXs...")
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

	log.Println("Filtering for desired Opporunities -- Starting with ALGO, USDC or USDT")
	filteredCycles := db.FilterArbPathsByStartAssetIds(uniqueCycles, []string{"0", "31566704", "312769"})
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
