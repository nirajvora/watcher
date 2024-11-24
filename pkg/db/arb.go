package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Asset struct {
	NodeId  int64
	AssetId string
}

type CycleKey struct {
	AssetIds []string  // Sorted list of asset IDs in the cycle
	Profit   float64   // Profit factor of the cycle
}

func (db *GraphDB) FindArbPaths(ctx context.Context, limit int) error {
	session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	assets, err := db.fetchStartAssets(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to fetch initial asset info: %w", err)
	}

	// Create transformed graph projection
	err = db.projectArbNetwork(ctx, session)
	if err != nil {
		return fmt.Errorf("Error projcting arbitrage network:%w", err)
	}

	// Use a map to track unique cycles
	uniqueCycles := make(map[string]struct{})
	totalOpportunities := 0

	// Check each asset for arbitrage opportunities
	for _, asset := range assets {
		fmt.Printf("\nChecking arbitrage opportunities starting from asset %s...\n", asset.AssetId)

		result, err := session.Run(ctx, `
            CALL gds.bellmanFord.stream('arbitrage_network', {
                sourceNode: $startNodeId,
                relationshipWeightProperty: 'negLogRate'
            })
            YIELD index, sourceNode, targetNode, totalCost, nodeIds, costs, route, isNegativeCycle
            WHERE exp(-totalCost) > 1
            RETURN DISTINCT exp(-totalCost) as profitFactor, [nodeId IN nodeIds | gds.util.asNode(nodeId).name] AS assetNames, [nodeId IN nodeIds | gds.util.asNode(nodeId).id] AS assetIds, costs, nodes(route) as route
            ORDER BY profitFactor DESC
            LIMIT $limit
        `, map[string]interface{}{
            "limit":       limit,
            "startNodeId": asset.NodeId,
        })
        if err != nil {
            fmt.Printf("Error checking asset %s: %v\n", asset.AssetId, err)
            continue
        }

		// Process results for this asset
		for result.Next(ctx) {
			record := result.Record()
			assetNames, ok1 := record.Get("assetNames")
			assetIds, ok2 := record.Get("assetIds")
			costs, ok3 := record.Get("costs")
			route, ok4 := record.Get("route")
			profitFactor, ok5 := record.Get("profitFactor")
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
				continue
			}

			// Create a normalized cycle key
			cycleKey, err := createCycleKey(assetIds.([]interface{}), profitFactor.(float64))
			if err != nil {
				continue
			}

			// Skip if we've seen this cycle before
			if _, exists := uniqueCycles[cycleKey]; exists {
				continue
			}
			uniqueCycles[cycleKey] = struct{}{}

			names := make([]string, len(assetNames.([]interface{})))
			for i, v := range assetNames.([]interface{}) {
				names[i] = v.(string)
			}
			fmt.Printf("\n\nFound arbitrage opportunity along the following route with profit factor: %v\n %v",
				profitFactor, strings.Join(names, "->"))
			fmt.Printf("\nDetailed information about each Node on the route:\n%v", route)
			fmt.Printf("\nnegLogRate for each liquidity pool necessary for facilitating arb:\n%v\n\n", costs)
			totalOpportunities++
		}
	}

	fmt.Printf("\nTotal arbitrage opportunities found: %d\n", totalOpportunities)

	// Clean up graph projection
	err = db.dropArbNetwork(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to clean up arbitrage_network projection: %w", err)
	}

	return nil
}

func (db *GraphDB) fetchStartAssets(ctx context.Context, session neo4j.SessionWithContext) ([]Asset, error) {
	// Get all assets and their node IDs
	result, err := session.Run(ctx, `
        MATCH (a:Asset) 
        RETURN id(a) as nodeId, a.id as assetId
    `, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get assets: %w", err)
	}

	// Store asset information
	var assets []Asset

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

		assets = append(assets, Asset{
			NodeId:  nId,
			AssetId: aId,
		})
	}

	return assets, nil
}

func (db *GraphDB) projectArbNetwork(ctx context.Context, session neo4j.SessionWithContext) error {
	_, err := session.Run(ctx, `
        CALL gds.graph.project(
            'arbitrage_network',
            'Asset',
            {
                PROVIDES_SWAP: {
                    orientation: 'NATURAL',
                    properties: {
                        negLogRate: {
                            property: 'negLogRate',
                            defaultValue: 1.0
                        }
                    }
                }
            }
        )
    `, nil)
	if err != nil {
		return fmt.Errorf("failed to create gds graph projection against DB: %w", err)
	}

	return nil
}

func (db *GraphDB) dropArbNetwork(ctx context.Context, session neo4j.SessionWithContext) error {
	_, err := session.Run(ctx, `
        CALL gds.graph.drop('arbitrage_network')
    `, nil)
	if err != nil {
		return fmt.Errorf("failed to drop gds graph projection from DB: %w", err)
	}

	return nil
}

func createCycleKey(assetIds []interface{}, profitFactor float64) (string, error) {
	// Convert assetIds to strings and find the smallest ID
	ids := make([]string, len(assetIds))
	smallestIdx := 0
	for i, id := range assetIds {
		strId, ok := id.(string)
		if !ok {
			return "", fmt.Errorf("invalid asset ID type")
		}
		ids[i] = strId
		if strId < ids[smallestIdx] {
			smallestIdx = i
		}
	}

	// Rotate the slice so that the smallest ID is first
	rotated := make([]string, len(ids))
	for i := 0; i < len(ids); i++ {
		rotated[i] = ids[(i+smallestIdx)%len(ids)]
	}

	// Create a unique key combining the rotated IDs and profit factor
	return fmt.Sprintf("%.10f:%s", profitFactor, strings.Join(rotated, ",")), nil
}