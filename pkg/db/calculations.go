package db

import (
	"math/big"
	"strings"
)

// CalculateMaxCycleLiquidity calculates the maximum amount of the first source asset
// that can theoretically be traded through the entire arbitrage cycle
func CalculateMaxCycleLiquidity(cycle ArbCycle) string {
	if len(cycle) == 0 {
		return "0"
	}

	// We'll start with the maximum liquidity possible, which is the source liquidity of the first pool
	maxLiquidity := new(big.Float)
	maxLiquidity.SetString(cycle[0].SourceLiquidity)

	// For each pool in the cycle, calculate the maximum input that can go through
	for i, pool := range cycle {
		// Parse the liquidity values
		sourceLiquidity := new(big.Float)
		sourceLiquidity.SetString(pool.SourceLiquidity)

		targetLiquidity := new(big.Float)
		targetLiquidity.SetString(pool.TargetLiquidity)

		// Create a temporary max input for this pool
		maxInput := new(big.Float).Set(sourceLiquidity)

		// If this is not the first pool, we need to convert our running max liquidity
		// to the current pool's source asset
		if i > 0 {
			prevPool := cycle[i-1]
			prevTargetLiquidity := new(big.Float)
			prevTargetLiquidity.SetString(prevPool.TargetLiquidity)

			// Calculate what percentage of the previous pool's target liquidity our current max represents
			ratio := new(big.Float).Quo(maxLiquidity, prevTargetLiquidity)

			// Apply that percentage to this pool's source liquidity
			maxInput.Mul(ratio, sourceLiquidity)
		}

		// Calculate the maximum output from this pool
		// Using the AMM formula: output = (target_liquidity * input) / (source_liquidity + input)
		denom := new(big.Float).Add(sourceLiquidity, maxInput)
		ratio := new(big.Float).Quo(maxInput, denom)
		maxOutput := new(big.Float).Mul(ratio, targetLiquidity)

		// Update our running max liquidity with the output from this pool
		maxLiquidity = maxOutput
	}

	// For the final step, we need to check if the last pool's output (in terms of the original asset)
	// is less than or equal to the input we started with
	firstPool := cycle[0]
	lastPool := cycle[len(cycle)-1]

	// If the last pool's target asset is not the same as the first pool's source asset,
	// the cycle is invalid, and we return 0
	if lastPool.TargetAssetId != firstPool.SourceAssetId {
		return "0"
	}

	// Format the result to a string with high precision
	result := new(big.Float).Set(maxLiquidity)
	resultStr := result.Text('f', 18) // 18 decimal places

	// Trim trailing zeros
	resultStr = strings.TrimRight(strings.TrimRight(resultStr, "0"), ".")

	return resultStr
}
