package graph

import (
	"math"
	"math/big"
)

const (
	// maxWeight is used when the effective rate is effectively zero or invalid.
	maxWeight = 230.0

	// minWeight is used when the effective rate would cause -log to be extremely negative.
	minWeight = -230.0
)

// CalculateWeight computes the edge weight for Bellman-Ford.
// Weight = -log(effectiveRate) where effectiveRate = (reserveOut / reserveIn) * (1 - fee)
//
// For arbitrage detection:
// - A negative cycle (sum of weights < 0) means product of rates > 1 (profit)
// - We use -log so that multiplying rates becomes addition of weights
func CalculateWeight(reserveIn, reserveOut *big.Int, fee float64) float64 {
	// Handle zero reserves
	if reserveIn == nil || reserveOut == nil || reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return maxWeight
	}

	// Convert to float64 for calculation
	// For very large reserves, this may lose precision but is acceptable for weight calculation
	in := new(big.Float).SetInt(reserveIn)
	out := new(big.Float).SetInt(reserveOut)

	// effectiveRate = (out / in) * (1 - fee)
	rate := new(big.Float).Quo(out, in)
	feeMultiplier := new(big.Float).SetFloat64(1 - fee)
	rate.Mul(rate, feeMultiplier)

	effectiveRate, _ := rate.Float64()

	// Handle edge cases
	if effectiveRate <= 0 || math.IsNaN(effectiveRate) {
		return maxWeight
	}
	if math.IsInf(effectiveRate, 1) {
		return minWeight
	}

	// Weight = -log(rate)
	weight := -math.Log(effectiveRate)

	// Clamp to reasonable range
	if weight > maxWeight {
		return maxWeight
	}
	if weight < minWeight {
		return minWeight
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) {
		return maxWeight
	}

	return weight
}

// CalculateEffectiveRate computes the effective exchange rate including fees.
func CalculateEffectiveRate(reserveIn, reserveOut *big.Int, fee float64) float64 {
	if reserveIn == nil || reserveOut == nil || reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return 0
	}

	in := new(big.Float).SetInt(reserveIn)
	out := new(big.Float).SetInt(reserveOut)

	rate := new(big.Float).Quo(out, in)
	feeMultiplier := new(big.Float).SetFloat64(1 - fee)
	rate.Mul(rate, feeMultiplier)

	effectiveRate, _ := rate.Float64()
	return effectiveRate
}

// WeightToRate converts a weight back to an effective rate.
func WeightToRate(weight float64) float64 {
	return math.Exp(-weight)
}

// CycleProfit calculates the profit factor from a cycle's total weight.
// If the sum of weights in a cycle is negative, the profit factor > 1.
func CycleProfit(totalWeight float64) float64 {
	return math.Exp(-totalWeight)
}

// IsProfitable returns true if the cycle weight indicates a profitable arbitrage.
func IsProfitable(totalWeight float64, minProfitFactor float64) bool {
	return CycleProfit(totalWeight) >= minProfitFactor
}
