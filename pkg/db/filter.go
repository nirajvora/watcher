package db

func FilterArbPathsByStartAssetIds(arbPaths map[string]ArbCycle, startAssetIds []string) map[string]ArbCycle {
	filteredPaths := make(map[string]ArbCycle)

	// Create a map for start assets
	startAssetsMap := make(map[string]struct{})
	for _, assetId := range startAssetIds {
		startAssetsMap[assetId] = struct{}{}
	}

	for cycleKey, cycle := range arbPaths {
		firstAsset := cycle[0].SourceAssetId
		if _, exists := startAssetsMap[firstAsset]; exists {
			filteredPaths[cycleKey] = cycle
		}
	}

	return filteredPaths
}
