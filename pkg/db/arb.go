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

type LiquidityPool struct {
	SourceAssetName string
	SourceAssetId   string
	SourceLiquidity string
	TargetAssetName string
	TargetAssetId   string
	TargetLiquidity string
	PoolAddress     string
	PoolExchange    string
	Chain           string
}

type ArbCycle struct {
	Pools        []LiquidityPool
	ProfitFactor float64
}

func (db *GraphDB) FindArbPaths(ctx context.Context, limit int, startTokens []string) error {
	session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	assets, err := fetchStartAssets(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to fetch initial asset info: %w", err)
	}

	// Create transformed graph projection
	graphName := "arbitrage_network"
	err = projectArbNetwork(ctx, session, graphName)
	if err != nil {
		return fmt.Errorf("Error projcting arbitrage network:%w", err)
	}

	// Use a map to track unique cycles
	uniqueCycles := make(map[string]ArbCycle)
	totalOpportunities := 0

	// Check each asset for arbitrage opportunities
	for _, asset := range assets {
		// fmt.Printf("\nChecking arbitrage opportunities starting from asset %s...\n", asset.AssetId)

		result, err := session.Run(ctx, `
            CALL gds.bellmanFord.stream($graphName, {
                sourceNode: $startNodeId,
                relationshipWeightProperty: 'negLogRate'
            })
            YIELD index, sourceNode, targetNode, totalCost, nodeIds, costs, route, isNegativeCycle
            WHERE exp(-totalCost) > 1.001
            RETURN DISTINCT
				exp(-totalCost) as profitFactor, 
				[nodeId IN nodeIds | gds.util.asNode(nodeId).name] AS assetNames, 
				[nodeId IN nodeIds | gds.util.asNode(nodeId).id] AS assetIds,
				[i IN range(0, size(costs)-2) | 
					toString(
						apoc.number.exact.sub(
							apoc.number.format(costs[i+1], '#.##################'),
							apoc.number.format(costs[i], '#.##################')
						)
					)
				] AS edgeWeights
            ORDER BY profitFactor DESC
            LIMIT $limit
        `, map[string]interface{}{
			"limit":       limit,
			"startNodeId": asset.NodeId,
			"graphName":   graphName,
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
			edgeWeights, ok3 := record.Get("edgeWeights")
			profitFactor, ok4 := record.Get("profitFactor")

			if !ok1 || !ok2 || !ok3 || !ok4 {
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

			ids := assetIds.([]interface{})
			var individualrates = edgeWeights.([]interface{})
			pools := make([]LiquidityPool, len(ids)-1) // -1 because the last asset is the same as first

			// query nodepool info based on returned assetIds and negLogRates
			for i := 0; i < len(ids)-1; i++ {
				pool, err := fetchPool(ctx, session,
					ids[i].(string),
					ids[i+1].(string),
					individualrates[i].(string),
				)
				if err != nil {
					fmt.Printf("Error fetching pool: %v\n", err)
					continue
				}
				pools[i] = pool
			}
			
			// Create ArbCycle with pools and profit factor
			cycle := ArbCycle{
				Pools:        pools,
				ProfitFactor: profitFactor.(float64),
			}

			uniqueCycles[cycleKey] = cycle
			names := make([]string, len(assetNames.([]interface{})))
			for i, v := range assetNames.([]interface{}) {
				names[i] = v.(string)
			}
			fmt.Printf("\n\nFound arbitrage opportunity along the following route with profit factor: %v\n %v\n ",
				profitFactor, strings.Join(names, "->"))
			totalOpportunities++
		}
	}

	// Filter arbitrage opportunities based on startTokens if provided
	if len(startTokens) > 0 {
		// Create a map for faster lookup
		startTokenMap := make(map[string]bool)
		for _, token := range startTokens {
			startTokenMap[token] = true
		}

		// Filter cycles that start with one of the specified tokens
		filteredCycles := make(map[string]ArbCycle)
		filteredOpportunities := 0

		for key, cycle := range uniqueCycles {
			if len(cycle.Pools) > 0 {
				// Check if the first asset in the cycle is in our startTokens list
				if startTokenMap[cycle.Pools[0].SourceAssetName] {
					filteredCycles[key] = cycle
					filteredOpportunities++
				}
			}
		}

		// Print filtered results
		fmt.Printf("\nFiltered arbitrage opportunities (starting with %v): %d out of %d total\n", 
			startTokens, filteredOpportunities, totalOpportunities)

		// If we filtered, print the filtered opportunities again for clarity
		if filteredOpportunities > 0 {
			fmt.Println("\nFiltered arbitrage paths:")
			for _, cycle := range filteredCycles {
				if len(cycle.Pools) > 0 {
					// Build path string
					pathNames := make([]string, len(cycle.Pools)+1)
					pathNames[0] = cycle.Pools[0].SourceAssetName
					for i, pool := range cycle.Pools {
						pathNames[i+1] = pool.TargetAssetName
					}

					// Print the arbitrage path with profit factor
					pathString := strings.Join(pathNames, "->")
					fmt.Printf("\nFound filtered arbitrage opportunity along the following route with profit factor: %v\n %s\n", 
						cycle.ProfitFactor, pathString)
				}
			}
		}
	} else {
		fmt.Printf("\nTotal arbitrage opportunities found: %d\n", totalOpportunities)
	}

	// Clean up graph projection
	err = dropArbNetwork(ctx, session, graphName)
	if err != nil {
		return fmt.Errorf("failed to clean up arbitrage network projection: %w", err)
	}

	return nil
}

// getAssetName fetches the name of an asset by its ID
func getAssetName(ctx context.Context, session neo4j.SessionWithContext, assetId string) (string, error) {
	result, err := session.Run(ctx, `
		MATCH (a:Asset) 
		WHERE a.id = $assetId
		RETURN a.name as assetName
	`, map[string]interface{}{
		"assetId": assetId,
	})
	
	if err != nil {
		return "", fmt.Errorf("failed to get asset name: %w", err)
	}
	
	if result.Next(ctx) {
		record := result.Record()
		assetName, ok := record.Get("assetName")
		if !ok {
			return "", fmt.Errorf("asset name not found in record")
		}
		
		name, ok := assetName.(string)
		if !ok {
			return "", fmt.Errorf("asset name is not a string")
		}
		
		return name, nil
	}
	
	return "", fmt.Errorf("no asset found with ID: %s", assetId)
}

func fetchStartAssets(ctx context.Context, session neo4j.SessionWithContext) ([]Asset, error) {
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

func fetchPool(ctx context.Context, session neo4j.SessionWithContext, sourceId string, targetId string, negLogRate string) (LiquidityPool, error) {
	const tolerance = 1

	result, err := session.Run(ctx, `
		MATCH (a1:Asset)-[r:PROVIDES_SWAP]->(a2:Asset)
		WHERE a1.id = $sourceId 
		AND a2.id = $targetId
		// AND abs(toFloat(apoc.number.exact.sub(r.negLogRate, $negLogRate))) < $tolerance
		WITH a1, a2, r, 
			abs(toFloat(apoc.number.exact.sub(r.negLogRate, $negLogRate))) as rateDiff
		RETURN DISTINCT
			a1.name as sourceName,
			a1.id as sourceId,
			a2.name as targetName,
			a2.id as targetId,
			toFloat(r.exchangeRate) as exchangeRate,
			r.address as poolAddress,
			r.exchange as exchange,
			r.chain as chain,
			r.negLogRate as actualRate,
			r.sourceLiquidity as sourceLiquidity,
			r.targetLiquidity as targetLiquidity,
			rateDiff as rateDiff
		ORDER BY exchangeRate DESC
		LIMIT 1
    `, map[string]interface{}{
		"sourceId":   sourceId,
		"negLogRate": negLogRate,
		"targetId":   targetId,
		"tolerance":  tolerance,
	})

	if err != nil {
		return LiquidityPool{}, fmt.Errorf("error querying pool details: %w", err)
	}

	if !result.Next(ctx) {
		return LiquidityPool{}, fmt.Errorf("no pool found for source: %s, target: %s with negLogRate %v", sourceId, targetId, negLogRate)
	}

	record := result.Record()
	sourceName, ok1 := record.Get("sourceName")
	sourceAssetId, ok2 := record.Get("sourceId")
	targetName, ok3 := record.Get("targetName")
	targetAssetId, ok4 := record.Get("targetId")
	poolAddress, ok5 := record.Get("poolAddress")
	exchange, ok6 := record.Get("exchange")
	sourceLiquidity, ok7 := record.Get("sourceLiquidity")
	targetLiquidity, ok8 := record.Get("targetLiquidity")

	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 || !ok8 {
		return LiquidityPool{}, fmt.Errorf("missing required fields in pool record")
	}

	pool := LiquidityPool{
		SourceAssetName: sourceName.(string),
		SourceAssetId:   sourceAssetId.(string),
		SourceLiquidity: sourceLiquidity.(string),
		TargetAssetName: targetName.(string),
		TargetAssetId:   targetAssetId.(string),
		TargetLiquidity: targetLiquidity.(string),
		PoolAddress:     poolAddress.(string),
		PoolExchange:    exchange.(string),
	}

	return pool, nil
}

func projectArbNetwork(ctx context.Context, session neo4j.SessionWithContext, graphName string) error {
	_, err := session.Run(ctx, `
		CALL gds.graph.project.cypher(
			$graphName,
			'MATCH (n:Asset) RETURN id(n) AS id',
			'MATCH (s:Asset)-[r:PROVIDES_SWAP]->(t:Asset) 
			RETURN id(s) AS source, 
					id(t) AS target, 
					toFloat(r.negLogRate) AS negLogRate'
		)
    `, map[string]interface{}{
		"graphName": graphName,
	})
	if err != nil {
		return fmt.Errorf("failed to create gds graph projection against DB: %w", err)
	}

	return nil
}

func dropArbNetwork(ctx context.Context, session neo4j.SessionWithContext, graphName string) error {
	_, err := session.Run(ctx, `
        CALL gds.graph.drop($graphName)
    `, map[string]interface{}{
		"graphName": graphName,
	})
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
