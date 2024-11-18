package db

import (
    "context"
    "fmt"
    "watcher/internal/models"
    "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Neo4jConfig struct {
    URI      string
    Username string
    Password string
}

type GraphDB struct {
    driver neo4j.DriverWithContext
}

func NewGraphDB(config Neo4jConfig) (*GraphDB, error) {
    driver, err := neo4j.NewDriverWithContext(
        config.URI,
        neo4j.BasicAuth(config.Username, config.Password, ""),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create driver: %w", err)
    }
    
    return &GraphDB{driver: driver}, nil
}

func (db *GraphDB) Close(ctx context.Context) error {
    return db.driver.Close(ctx)
}

func (db *GraphDB) SetupSchema(ctx context.Context) error {
    session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
    defer session.Close(ctx)

    constraints := []string{
        "CREATE CONSTRAINT asset_id IF NOT EXISTS FOR (a:Asset) REQUIRE a.id IS UNIQUE",
        "CREATE CONSTRAINT pool_address IF NOT EXISTS FOR (p:Pool) REQUIRE p.address IS UNIQUE",
    }
    

    for _, constraint := range constraints {
        _, err := session.Run(ctx, constraint, nil)
        if err != nil {
            return fmt.Errorf("failed to create constraint: %w", err)
        }
    }

    indexes := []string{
        "CREATE INDEX asset_symbol IF NOT EXISTS FOR (a:Asset) ON (a.id)",
        "CREATE INDEX pool_exchange IF NOT EXISTS FOR (p:Pool) ON (p.exchange)",
        "CREATE INDEX pool_exchange_rate IF NOT EXISTS FOR ()-[r:PROVIDES_SWAP]-() ON (r.exchangeRate)",
        "CREATE INDEX exchange_asset_pair_unique IF NOT EXISTS FOR ()-[r:PROVIDES_SWAP]-() ON (r.exchange, r.poolAddress)",
    }

    for _, index := range indexes {
        _, err := session.Run(ctx, index, nil)
        if err != nil {
            return fmt.Errorf("failed to create index: %w", err)
        }
    }

    return nil
}

func (db *GraphDB) StorePool(ctx context.Context, pool models.Pool) error {
    session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
    defer session.Close(ctx)

    query := `
    MERGE (a1:Asset {id: $asset1})
    MERGE (a2:Asset {id: $asset2})
    MERGE (p:Pool {
        address: $poolId
    })
    ON CREATE SET
        p.exchange = $exchange,
        p.liquidity1 = $liquidity1,
        p.liquidity2 = $liquidity2
    ON MATCH SET
        p.liquidity1 = $liquidity1,
        p.liquidity2 = $liquidity2

    WITH a1, a2, p

    MERGE (a1)-[s1:PROVIDES_SWAP {
        exchange: $exchange,
        poolAddress: $poolId
    }]->(a2)
    ON CREATE SET
        s1.exchangeRate = $exchangeRate,
        s1.liquidity = $liquidity1
    ON MATCH SET
        s1.exchangeRate = $exchangeRate,
        s1.liquidity = $liquidity1

    MERGE (a2)-[s2:PROVIDES_SWAP {
        exchange: $exchange,
        poolAddress: $poolId
    }]->(a1)
    ON CREATE SET
        s2.exchangeRate = 1.0 / $exchangeRate,
        s2.liquidity = $liquidity2
    ON MATCH SET
        s2.exchangeRate = 1.0 / $exchangeRate,
        s2.liquidity = $liquidity2

    RETURN p, a1, a2, s1, s2
    `

    params := map[string]interface{}{
        "poolId": pool.ID,
        "exchange": pool.Exchange,
        "asset1": pool.Asset1,
        "asset2": pool.Asset2,
        "liquidity1": pool.Liquidity1,
        "liquidity2": pool.Liquidity2,
        "exchangeRate": pool.ExchangeRate,
    }

    _, err := session.Run(ctx, query, params)
    if err != nil {
        return fmt.Errorf("failed to store pool: %w", err)
    }

    return nil
}

func (db *GraphDB) FindArbPaths(ctx context.Context, limit string) error {
    session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
    defer session.Close(ctx)

    // Create initial graph projection
    _, err := session.Run(ctx, `
        CALL gds.graph.project(
            'swap_network',
            'Asset',
            {
                PROVIDES_SWAP: {
                    orientation: 'NATURAL',
                    properties: {
                        weight: {
                            property: 'exchangeRate',
                            defaultValue: 1.0
                        }
                    }
                }
            }
        )
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to create initial graph projection: %w", err)
    }

    // Create transformed graph projection
    _, err = session.Run(ctx, `
        CALL gds.graph.project(
            'arbitrage_network',
            'Asset',
            {
                PROVIDES_SWAP: {
                    orientation: 'NATURAL',
                    properties: {
                        negLogWeight: {
                            property: 'exchangeRate',
                            defaultValue: 1.0,
                            mapping: { 
                                type: 'LOGARITHM',
                                negation: true
                            }
                        }
                    }
                }
            }
        )
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to create arbitrage graph projection: %w", err)
    }

    // Run Bellman-Ford and get results
    result, err := session.Run(ctx, `
        CALL gds.bellmanFord.stream('arbitrage_network', {
            sourceNode: gds.util.asNode('Asset', {id: 'ALGO'}).id,
            relationshipWeightProperty: 'negLogWeight',
            relationshipTypes: ['PROVIDES_SWAP']
        })
        YIELD path, totalCost
        WHERE totalCost < 0
        RETURN path, exp(-totalCost) as profitFactor
        ORDER BY profitFactor DESC
        LIMIT $limit
    `, map[string]interface{}{
        "limit": limit,
    })
    if err != nil {
        return fmt.Errorf("failed to run bellman-ford: %w", err)
    }

    // Process results
    for result.Next(ctx) {
        record := result.Record()
        path, _ := record.Get("path")
        profitFactor, _ := record.Get("profitFactor")
        fmt.Printf("Found arbitrage path with profit factor: %v\nPath: %v\n", profitFactor, path)
    }

    // Clean up graph projections
    _, err = session.Run(ctx, `
        CALL gds.graph.drop('swap_network');
        CALL gds.graph.drop('arbitrage_network');
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to clean up graph projections: %w", err)
    }

    return nil
}