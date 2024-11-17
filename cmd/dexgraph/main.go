package main

import (
    "context"
    "log"
    "watcher/internal/db"
    "watcher/internal/dex"
    "watcher/internal/dex/tinyman"
)

func main() {
    ctx := context.Background()

    // Initialize database
    database, err := db.NewGraphDB(db.Neo4jConfig{
        URI:      "neo4j://localhost:7687",
        Username: "neo4j",
        Password: "your-secure-password",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer database.Close(ctx)

    // Setup schema
    if err := database.SetupSchema(ctx); err != nil {
        log.Fatal(err)
    }

    // Initialize DEX service with fetchers
    dexService := dex.NewService(
        tinyman.NewClient(),
        // Add other DEX clients here
    )

    // Fetch pools from all DEXs
    pools, err := dexService.FetchAllPools(ctx)
    if err != nil {
        log.Fatal(err)
    }

    // Store pools in database
    for _, pool := range pools {
        if err := database.StorePool(ctx, pool); err != nil {
            log.Printf("Failed to store pool %s: %v", pool.ID, err)
            continue
        }
    }

    log.Printf("Successfully processed %d pools", len(pools))
}