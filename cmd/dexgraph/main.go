package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"
	"watcher/pkg/db"
	"watcher/pkg/dex"
	"watcher/pkg/dex/tinyman"
)

func main() {
	ctx := context.Background()

	log.Println("Initializing database connection...")
	database, err := db.NewGraphDB(db.Neo4jConfig{
		URI:      "neo4j://localhost:7687",
		Username: "neo4j",
		Password: "your-secure-password",
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
	
	// Parse command line flags
	limitPtr := flag.Int("limit", 5, "Limit the number of arbitrage paths to find per asset")
	tokensPtr := flag.String("tokens", "", "Comma-separated list of tokens to filter by (e.g., 'USDC,ALGO')")
	flag.Parse()
	
	// Parse the tokens from the command line flag
	var filterTokens []string
	if *tokensPtr != "" {
		filterTokens = strings.Split(*tokensPtr, ",")
		// Trim whitespace from each token
		for i := range filterTokens {
			filterTokens[i] = strings.TrimSpace(filterTokens[i])
		}
	} else {
		// Default tokens if none specified
		filterTokens = []string{"USDC", "ALGO"}
	}
	
	if len(filterTokens) > 0 {
		log.Printf("Filtering arbitrage paths to only show those starting with: %v", filterTokens)
	} else {
		log.Println("Showing all arbitrage paths (no filter applied)")
	}
	
	if err := database.FindArbPaths(ctx, *limitPtr, filterTokens); err != nil {
		log.Printf("Failed to find arbs: %v", err)
	}
}
