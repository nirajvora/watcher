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
        "CREATE CONSTRAINT pool_id IF NOT EXISTS FOR (p:Pool) REQUIRE p.id IS UNIQUE",
        "CREATE CONSTRAINT exchange_id IF NOT EXISTS FOR (e:Exchange) REQUIRE e.id IS UNIQUE",
    }

    for _, constraint := range constraints {
        _, err := session.Run(ctx, constraint, nil)
        if err != nil {
            return fmt.Errorf("failed to create constraint: %w", err)
        }
    }

    indexes := []string{
        "CREATE INDEX pool_exchange_idx IF NOT EXISTS FOR (p:Pool) ON (p.exchange)",
        "CREATE INDEX asset_symbol_idx IF NOT EXISTS FOR (a:Asset) ON (a.symbol)",
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
    MERGE (e:Exchange {id: $exchange})
    MERGE (a1:Asset {id: $asset1})
    MERGE (a2:Asset {id: $asset2})
    MERGE (p:Pool {id: $poolId})
    SET p.liquidity1 = $liq1,
        p.liquidity2 = $liq2,
        p.exchangeRate = $rate,
        p.exchange = $exchange
    MERGE (p)-[:ON_EXCHANGE]->(e)
    MERGE (p)-[:HAS_TOKEN_A]->(a1)
    MERGE (p)-[:HAS_TOKEN_B]->(a2)
    `

    params := map[string]interface{}{
        "poolId":   pool.ID,
        "exchange": pool.Exchange,
        "asset1":   pool.Asset1,
        "asset2":   pool.Asset2,
        "liq1":     pool.Liquidity1,
        "liq2":     pool.Liquidity2,
        "rate":     pool.ExchangeRate,
    }

    _, err := session.Run(ctx, query, params)
    if err != nil {
        return fmt.Errorf("failed to store pool: %w", err)
    }

    return nil
}