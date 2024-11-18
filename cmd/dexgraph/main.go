package main

import (
    "context"
    "log"
    "time"
    // "watcher/internal/db"
    "watcher/internal/dex"
    "watcher/internal/dex/tinyman"
)

func main() {
    ctx := context.Background()

    // log.Println("Initializing database connection...")
    // database, err := db.NewGraphDB(db.Neo4jConfig{
    //     URI:      "neo4j://localhost:7687",
    //     Username: "neo4j",
    //     Password: "your-secure-password",
    // })
    // if err != nil {
    //     log.Fatal(err)
    // }
    // defer database.Close(ctx)

    // log.Println("Setting up database schema...")
    // if err := database.SetupSchema(ctx); err != nil {
    //     log.Fatal(err)
    // }

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

    // log.Println("Storing pools in database...")
    // stored := 0
    // for _, pool := range pools {
    //     if err := database.StorePool(ctx, pool); err != nil {
    //         log.Printf("Failed to store pool %s: %v", pool.ID, err)
    //         continue
    //     }
    //     stored++
    //     if stored%100 == 0 {
    //         log.Printf("Stored %d pools so far...", stored)
    //     }
    // }

    // log.Printf("Successfully processed %d pools", stored)
}