package db

import (
	"context"
	"fmt"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"math/big"
	"strings"
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

type ArbCycle []LiquidityPool

func (db *GraphDB) FindArbPaths(ctx context.Context, limit int) error {
	session := db.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	assets, err := fetchStartAssets(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to fetch initial asset info: %w", err)
	}

	// Create transformed graph projection
	err = projectArbNetwork(ctx, session)
	if err != nil {
		return fmt.Errorf("Error projcting arbitrage network:%w", err)
	}

	// Use a map to track unique cycles
	uniqueCycles := make(map[string]ArbCycle)
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
            RETURN DISTINCT
				totalCost as totalCost,
				exp(-totalCost) as profitFactor, 
				[nodeId IN nodeIds | gds.util.asNode(nodeId).name] AS assetNames, 
				[nodeId IN nodeIds | gds.util.asNode(nodeId).id] AS assetIds, 
				costs, 
				nodes(route) as route, 
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
			edgeWeights, ok4 := record.Get("edgeWeights")
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

			ids := assetIds.([]interface{})
			costsList := costs.([]interface{})

			// Calculate individual negLogRates
			individualRates := make([]*big.Float, len(costsList))
			for i := 0; i < len(costsList); i++ {
				if i == 0 {
					individualRates[i], _ = new(big.Float).SetString(fmt.Sprintf("%v", costsList[i]))
				} else {
					curr, _ := new(big.Float).SetString(fmt.Sprintf("%v", costsList[i]))
					prev, _ := new(big.Float).SetString(fmt.Sprintf("%v", costsList[i-1]))
					individualRates[i] = new(big.Float).Sub(curr, prev)
				}
			}

			cycle := make(ArbCycle, len(ids)-1) // -1 because the last asset is the same as first

			// query nodepool info based on returned assetIds and negLogRates
			for i := 0; i < len(ids)-1; i++ {
				pool, err := fetchPool(ctx, session,
					ids[i].(string),
					ids[i+1].(string),
					individualRates[i+1],
				)
				if err != nil {
					fmt.Printf("Error fetching pool: %v\n", err)
					continue
				}
				cycle[i] = pool
			}

			uniqueCycles[cycleKey] = cycle
			names := make([]string, len(assetNames.([]interface{})))
			for i, v := range assetNames.([]interface{}) {
				names[i] = v.(string)
			}
			fmt.Printf("\n\nFound arbitrage opportunity along the following route with profit factor: %v\n %v\n ",
				profitFactor, strings.Join(names, "->"))
			fmt.Printf("\nEdgeweights: %v\n", edgeWeights)
			fmt.Printf("\nindividualRates: %v\n", individualRates)
			totalOpportunities++
		}
	}

	fmt.Printf("\nTotal arbitrage opportunities found: %d\n", totalOpportunities)

	// Clean up graph projection
	err = dropArbNetwork(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to clean up arbitrage_network projection: %w", err)
	}

	return nil
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

func fetchPool(ctx context.Context, session neo4j.SessionWithContext, sourceId string, targetId string, negLogRate *big.Float) (LiquidityPool, error) {
	const tolerance = 1

	result, err := session.Run(ctx, `
		MATCH (a1:Asset)-[r:PROVIDES_SWAP]->(a2:Asset)
		WHERE a1.id = $sourceId 
		AND a2.id = $targetId
		// AND abs(toFloat(apoc.number.exact.sub(toString(r.negLogRate), $negLogRate))) < $tolerance
		WITH a1, a2, r, 
			abs(toFloat(apoc.number.exact.sub(toString(r.negLogRate), $negLogRate))) as rateDiff
		RETURN DISTINCT
			a1.name as sourceName,
			a1.id as sourceId,
			a2.name as targetName,
			a2.id as targetId,
			r.exchangeRate as exchangeRate,
			r.address as poolAddress,
			r.exchange as exchange,
			r.chain as chain,
			toString(r.negLogRate) as actualRate,
			toString(r.sourceLiquidity) as sourceLiquidity,
			toString(r.targetLiquidity) as targetLiquidity,
			toString(rateDiff) as rateDiff
		ORDER BY exchangeRate DESC
		LIMIT 1
    `, map[string]interface{}{
		"sourceId":   sourceId,
		"negLogRate": negLogRate.String(),
		"targetId":   targetId,
		"tolerance":  tolerance,
	})

	if err != nil {
		return LiquidityPool{}, fmt.Errorf("error querying pool details: %w", err)
	}

	if !result.Next(ctx) {
		return LiquidityPool{}, fmt.Errorf("no pool found for source: %s, target: %s with negLogRate %v", sourceId, targetId, negLogRate.String())
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

func projectArbNetwork(ctx context.Context, session neo4j.SessionWithContext) error {
	_, err := session.Run(ctx, `
		MATCH (source:Asset)-[r:PROVIDES_SWAP]->(target:Asset)
		RETURN gds.graph.project(
			'arbitrage_network',
			source,
			target,
			{ 
				relationshipProperties: r { 
					.negLogRate 
				} 
			}
		)
    `, nil)
	if err != nil {
		return fmt.Errorf("failed to create gds graph projection against DB: %w", err)
	}

	return nil
}

func dropArbNetwork(ctx context.Context, session neo4j.SessionWithContext) error {
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
