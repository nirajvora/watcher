package db

import (
    "context"
    "fmt"
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

	for result.Next(ctx) {
		record := result.Record()
		source, _ := record.Get("a1.id")
		target, _ := record.Get("a2.id")
		exchange, _ := record.Get("r.exchange")
		rate, _ := record.Get("r.exchangeRate")

		sourceID := source.(string)
		targetID := target.(string)

		nodesMap[sourceID] = Node{ID: sourceID, Label: sourceID}
		nodesMap[targetID] = Node{ID: targetID, Label: targetID}

		links = append(links, Link{
			Source: sourceID,
			Target: targetID,
			Type:   exchange.(string),
			Rate:   rate.(float64),
		})
	}

	var nodes []Node
	for _, node := range nodesMap {
		nodes = append(nodes, node)
	}

	return GraphData{Nodes: nodes, Links: links}, nil
}

func (db *GraphDB) FindArbPaths(ctx context.Context, limit int) error {
    session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
    defer session.Close(ctx)

    // First, check what assets we have
    result, err := session.Run(ctx, `
        MATCH (a:Asset) 
        RETURN a.id as assetId 
        LIMIT 5
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to check assets: %w", err)
    }

    fmt.Println("Available assets:")
    for result.Next(ctx) {
        record := result.Record()
        assetId, ok := record.Get("assetId")
        if !ok {
            continue
        }
        fmt.Printf("- %v\n", assetId)
    }

    // Now find the node ID of our starting asset (using the first asset we find)
    result, err = session.Run(ctx, `
        MATCH (a:Asset)
        RETURN id(a) as nodeId, a.id as assetId
        LIMIT 1
    `, nil)
    if err != nil {
        return fmt.Errorf("failed to find starting node ID: %w", err)
    }

    var startNodeId int64
    var startAssetId string
    if result.Next(ctx) {
        record := result.Record()
        nodeId, ok := record.Get("nodeId")
        if !ok {
            return fmt.Errorf("nodeId not found in record")
        }
        
        startNodeId, ok = nodeId.(int64)
        if !ok {
            return fmt.Errorf("failed to convert nodeId to int64")
        }

        assetId, ok := record.Get("assetId")
        if !ok {
            return fmt.Errorf("assetId not found in record")
        }
        
        startAssetId, ok = assetId.(string)
        if !ok {
            return fmt.Errorf("failed to convert assetId to string")
        }
        
        fmt.Printf("Starting with asset: %s (node ID: %d)\n", startAssetId, startNodeId)
    } else {
        return fmt.Errorf("no assets found in database")
    }

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

    // Run Bellman-Ford and get results using the found nodeId
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
        "startNodeId": startNodeId,
    })
    if err != nil {
        return fmt.Errorf("failed to run bellman-ford: %w", err)
    }

    // Process results
    foundArbs := false
    for result.Next(ctx) {
        foundArbs = true
        record := result.Record()
        sourceAsset, ok1 := record.Get("sourceAsset")
        targetAsset, ok2 := record.Get("targetAsset")
        profitFactor, ok3 := record.Get("profitFactor")
        if !ok1 || !ok2 || !ok3 {
            continue
        }
        fmt.Printf("Found arbitrage opportunity from %v to %v with profit factor: %v\n", 
            sourceAsset, targetAsset, profitFactor)
    }

    if !foundArbs {
        fmt.Println("No arbitrage opportunities found")
    }

    // Clean up graph projections
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