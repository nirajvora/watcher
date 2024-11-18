package db

import (
    "context"
    "fmt"
    "log"
    "math"
    "watcher/pkg/models"
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

type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Link struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Type   string  `json:"type"`
	Rate   float64 `json:"exchangeRate"`
}

type GraphData struct {
	Nodes []Node `json:"nodes"`
	Links []Link `json:"links"`
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

func (db *GraphDB) FetchGraphData(ctx context.Context) (GraphData, error) {
    session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
    defer session.Close(ctx)

    query := `
    MATCH (a1:Asset)-[r:PROVIDES_SWAP]->(a2:Asset)
    RETURN DISTINCT a1.id, a2.id, r.exchange, r.exchangeRate
    `

    result, err := session.Run(ctx, query, nil)
    if err != nil {
        return GraphData{}, err
    }

    nodesMap := make(map[string]Node)
    var links []Link
    var invalidRates int

    for result.Next(ctx) {
        record := result.Record()
        source, _ := record.Get("a1.id")
        target, _ := record.Get("a2.id")
        exchange, _ := record.Get("r.exchange")
        rate, _ := record.Get("r.exchangeRate")

        sourceID := source.(string)
        targetID := target.(string)
        exchangeRate := rate.(float64)

        // Log problematic rates but include them in the output
        if math.IsInf(exchangeRate, 0) || math.IsNaN(exchangeRate) {
            log.Printf("WARNING: Invalid exchange rate detected between %s and %s: %v", 
                sourceID, targetID, exchangeRate)
            invalidRates++
            // Set to a value that JSON can handle
            exchangeRate = -1
        }

        nodesMap[sourceID] = Node{ID: sourceID, Label: sourceID}
        nodesMap[targetID] = Node{ID: targetID, Label: targetID}

        links = append(links, Link{
            Source: sourceID,
            Target: targetID,
            Type:   exchange.(string),
            Rate:   exchangeRate,
        })
    }

    var nodes []Node
    for _, node := range nodesMap {
        nodes = append(nodes, node)
    }

    log.Printf("Fetched %d nodes and %d links (including %d invalid rates)", 
        len(nodes), len(links), invalidRates)

    return GraphData{Nodes: nodes, Links: links}, nil
}

func (db *GraphDB) FindArbPaths(ctx context.Context, limit int) error {
    session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
    defer session.Close(ctx)

    // Get all assets and their node IDs
    result, err := session.Run(ctx, `
        MATCH (a:Asset) 
        RETURN id(a) as nodeId, a.id as assetId
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to get assets: %w", err)
    }

    // Store asset information
    type AssetInfo struct {
        NodeId  int64
        AssetId string
    }
    var assets []AssetInfo

    for result.Next(ctx) {
        record := result.Record()
        nodeId, ok := record.Get("nodeId")
        if !ok {
            continue
        }
        
        nId, ok := nodeId.(int64)
        if !ok {
            continue
        }

        assetId, ok := record.Get("assetId")
        if !ok {
            continue
        }
        
        aId, ok := assetId.(string)
        if !ok {
            continue
        }

        assets = append(assets, AssetInfo{
            NodeId:  nId,
            AssetId: aId,
        })
    }

    fmt.Printf("Found %d assets to check for arbitrage\n", len(assets))

    // Create initial graph projection
    _, err = session.Run(ctx, `
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

    // Check each asset for arbitrage opportunities
    totalOpportunities := 0
    for _, asset := range assets {
        fmt.Printf("\nChecking arbitrage opportunities starting from asset %s...\n", asset.AssetId)
        
        result, err = session.Run(ctx, `
            CALL gds.bellmanFord.stream('arbitrage_network', {
                sourceNode: $startNodeId,
                relationshipWeightProperty: 'negLogWeight'
            })
            YIELD targetNode, totalCost
            WITH targetNode, totalCost WHERE totalCost < 0
            MATCH (source:Asset), (target:Asset)
            WHERE id(source) = $startNodeId AND id(target) = targetNode
            RETURN source.id as sourceAsset, target.id as targetAsset, exp(-totalCost) as profitFactor
            ORDER BY profitFactor DESC
            LIMIT $limit
        `, map[string]interface{}{
            "limit": limit,
            "startNodeId": asset.NodeId,
        })
        if err != nil {
            // Log error but continue with next asset
            fmt.Printf("Error checking asset %s: %v\n", asset.AssetId, err)
            continue
        }

        // Process results for this asset
        opportunities := 0
        for result.Next(ctx) {
            record := result.Record()
            sourceAsset, ok1 := record.Get("sourceAsset")
            targetAsset, ok2 := record.Get("targetAsset")
            profitFactor, ok3 := record.Get("profitFactor")
            if !ok1 || !ok2 || !ok3 {
                continue
            }
            fmt.Printf("Found arbitrage opportunity from %v to %v with profit factor: %v\n", 
                sourceAsset, targetAsset, profitFactor)
            opportunities++
            totalOpportunities++
        }

        if opportunities == 0 {
            fmt.Printf("No arbitrage opportunities found starting from asset %s\n", asset.AssetId)
        }
    }

    fmt.Printf("\nTotal arbitrage opportunities found: %d\n", totalOpportunities)

    // Clean up graph projections - separate calls
    _, err = session.Run(ctx, `
        CALL gds.graph.drop('swap_network')
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to clean up swap_network projection: %w", err)
    }

    _, err = session.Run(ctx, `
        CALL gds.graph.drop('arbitrage_network')
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to clean up arbitrage_network projection: %w", err)
    }

    return nil
}