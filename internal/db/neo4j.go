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
        "CREATE CONSTRAINT unique_exchange_asset_pair IF NOT EXISTS FOR ()-[r:PROVIDES_SWAP]->() REQUIRE (r.exchange, startNode(r).id, endNode(r).id) IS UNIQUE;"
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