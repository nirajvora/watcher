package db

import (
	"context"
	"fmt"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"log"
	"watcher/pkg/models"
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
	Name  string `json:"name"`
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
		"CREATE INDEX exchange_asset_pair_unique IF NOT EXISTS FOR ()-[r:PROVIDES_SWAP]-() ON (r.exchange, r.address)",
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
    MERGE (a1:Asset {
        id: $asset1Id,
        name: $asset1Name
    })
    MERGE (a2:Asset {
        id: $asset2Id,
        name: $asset2Name
    })
    MERGE (p:Pool {
        address: $address
    })
    ON CREATE SET
        p.exchange = $exchange,
        p.liquidity1 = $liquidity1,
        p.liquidity2 = $liquidity2,
        p.chain = $chain
    ON MATCH SET
        p.liquidity1 = $liquidity1,
        p.liquidity2 = $liquidity2

    WITH a1, a2, p

    MERGE (a1)-[s1:PROVIDES_SWAP {
        exchange: $exchange,
        address: $address
    }]->(a2)
    SET 
        s1.exchangeRate = $exchangeRate,
        s1.sourceLiquidity = $liquidity1,
        s1.targetLiquidity = $liquidity2,
        s1.sourceName = $asset1Name,
        s1.targetName = $asset2Name

    MERGE (a2)-[s2:PROVIDES_SWAP {
        exchange: $exchange,
        address: $address
    }]->(a1)
    SET
        s2.exchangeRate = $reciprocalExchangeRate,
        s2.sourceLiquidity = $liquidity2,
        s2.targetLiquidity = $liquidity1,
        s2.sourceName = $asset2Name,
        s2.targetName = $asset1Name

    RETURN p, a1, a2, s1, s2
    `

	params := map[string]interface{}{
		"address":                pool.Address,
		"exchange":               pool.Exchange,
		"chain":                  pool.Chain,
		"asset1Id":               pool.Asset1ID,
		"asset1Name":             pool.Asset1Name,
		"asset2Id":               pool.Asset2ID,
		"asset2Name":             pool.Asset2Name,
		"liquidity1":             pool.Liquidity1,
		"liquidity2":             pool.Liquidity2,
		"exchangeRate":           pool.ExchangeRate,
		"reciprocalExchangeRate": pool.ReciprocalExchangeRate,
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

	// Update the query to include name fields
	query := `
    MATCH (a1:Asset)-[r:PROVIDES_SWAP]->(a2:Asset)
    RETURN DISTINCT 
        a1.id as source_id,
        a1.name as source_name,
        a2.id as target_id,
        a2.name as target_name,
        r.exchange,
        r.exchangeRate
    `

	result, err := session.Run(ctx, query, nil)
	if err != nil {
		return GraphData{}, err
	}

	nodesMap := make(map[string]Node)
	var links []Link

	for result.Next(ctx) {
		record := result.Record()
		sourceID, _ := record.Get("source_id")
		targetID, _ := record.Get("target_id")
		exchange, _ := record.Get("r.exchange")
		rate, _ := record.Get("r.exchangeRate")

		sourceName, ok := record.Get("source_name")
		if !ok || sourceName == nil {
			sourceName = sourceID
		}

		targetName, ok := record.Get("target_name")
		if !ok || targetName == nil {
			targetName = targetID
		}

		sID := sourceID.(string)
		tID := targetID.(string)
		sName := sourceName.(string)
		tName := targetName.(string)
		exchangeRate := rate.(float64)
		exchangeName := exchange.(string)

		if _, exists := nodesMap[sID]; !exists {
			nodesMap[sID] = Node{
				ID:    sID,
				Label: sID,
				Name:  sName,
			}
		}
		if _, exists := nodesMap[tID]; !exists {
			nodesMap[tID] = Node{
				ID:    tID,
				Label: tID,
				Name:  tName,
			}
		}

		links = append(links, Link{
			Source: sID,
			Target: tID,
			Type:   exchangeName,
			Rate:   exchangeRate,
		})
	}

	var nodes []Node
	for _, node := range nodesMap {
		nodes = append(nodes, node)
	}

	log.Printf("\nFetched %d nodes and %d links", len(nodes), len(links))

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
			"limit":       limit,
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
