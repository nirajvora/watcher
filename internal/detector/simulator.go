package detector

import (
	"math/big"

	"watcher/internal/graph"
)

// SimulationResult contains the results of simulating an arbitrage opportunity.
type SimulationResult struct {
	// MaxInput is the maximum input amount that can traverse the entire cycle
	MaxInputWei *big.Int

	// EstimatedOutput is the expected output after completing the cycle
	EstimatedOutputWei *big.Int

	// EstimatedProfit is EstimatedOutput - MaxInput
	EstimatedProfitWei *big.Int

	// ProfitFactor is EstimatedOutput / MaxInput (should be > 1 for profit)
	ProfitFactor float64

	// IsProfitable indicates if the simulation shows profit
	IsProfitable bool

	// LimitingPoolIndex is the index of the pool that limits the input amount
	LimitingPoolIndex int

	// IntermediateAmounts are the amounts at each step
	IntermediateAmounts []*big.Int
}

// SimulateCycle simulates executing an arbitrage cycle with actual AMM math.
// It finds the maximum input amount and calculates the expected profit.
func SimulateCycle(cycle *Cycle, snap *graph.Snapshot, minProfitFactor float64) *SimulationResult {
	if cycle == nil || len(cycle.Edges) < 2 {
		return nil
	}

	// Get maximum input that doesn't exceed any pool's liquidity
	maxInput := calculateMaxInput(cycle, snap)
	if maxInput == nil || maxInput.Sign() <= 0 {
		return &SimulationResult{IsProfitable: false}
	}

	// Simulate the swaps
	amounts, output := simulateSwaps(cycle, maxInput)
	if output == nil || output.Sign() <= 0 {
		return &SimulationResult{IsProfitable: false}
	}

	// Calculate profit
	profit := new(big.Int).Sub(output, maxInput)

	// Calculate profit factor using big.Float for precision
	inputFloat := new(big.Float).SetInt(maxInput)
	outputFloat := new(big.Float).SetInt(output)
	profitFactorFloat := new(big.Float).Quo(outputFloat, inputFloat)
	profitFactor, _ := profitFactorFloat.Float64()

	return &SimulationResult{
		MaxInputWei:         maxInput,
		EstimatedOutputWei:  output,
		EstimatedProfitWei:  profit,
		ProfitFactor:        profitFactor,
		IsProfitable:        profitFactor >= minProfitFactor,
		IntermediateAmounts: amounts,
	}
}

// calculateMaxInput determines the maximum input amount for a cycle.
// We limit by a fraction of the smallest reserve to avoid excessive price impact.
func calculateMaxInput(cycle *Cycle, snap *graph.Snapshot) *big.Int {
	if len(cycle.Edges) == 0 {
		return nil
	}

	// Start with a reasonable fraction of the first pool's input reserve
	// (typically 1% to 10% of reserves)
	firstEdge := cycle.Edges[0]
	maxInput := new(big.Int).Div(firstEdge.Reserve0, big.NewInt(100)) // 1% of input reserve

	// Iterate through cycle to find limiting factor
	currentAmount := new(big.Int).Set(maxInput)

	for i, edge := range cycle.Edges {
		// Calculate output for current amount
		output := calculateSwapOutput(currentAmount, edge.Reserve0, edge.Reserve1, edge.Fee)
		if output == nil || output.Sign() <= 0 {
			// This input is too large, reduce it
			maxInput = reduceInput(maxInput, i)
			if maxInput.Sign() <= 0 {
				return nil
			}
			// Restart simulation with reduced input
			currentAmount = new(big.Int).Set(maxInput)
			i = -1 // Will be incremented to 0
			continue
		}

		// Check if output exceeds a reasonable fraction of the output reserve
		// (to avoid excessive slippage)
		maxOutput := new(big.Int).Div(edge.Reserve1, big.NewInt(10)) // 10% of output reserve
		if output.Cmp(maxOutput) > 0 {
			// Scale down input proportionally
			scaleFactor := new(big.Float).Quo(
				new(big.Float).SetInt(maxOutput),
				new(big.Float).SetInt(output),
			)
			scaledInput := new(big.Float).Mul(new(big.Float).SetInt(maxInput), scaleFactor)
			scaledInput.Int(maxInput)

			// Restart simulation
			currentAmount = new(big.Int).Set(maxInput)
			i = -1
			continue
		}

		currentAmount = output
	}

	return maxInput
}

// reduceInput reduces the input by a factor based on which pool caused the limit.
func reduceInput(input *big.Int, poolIdx int) *big.Int {
	// Reduce by 50% each time
	return new(big.Int).Div(input, big.NewInt(2))
}

// simulateSwaps simulates swaps through the cycle and returns intermediate amounts and final output.
func simulateSwaps(cycle *Cycle, inputAmount *big.Int) ([]*big.Int, *big.Int) {
	amounts := make([]*big.Int, len(cycle.Edges)+1)
	amounts[0] = new(big.Int).Set(inputAmount)

	current := new(big.Int).Set(inputAmount)

	for i, edge := range cycle.Edges {
		output := calculateSwapOutput(current, edge.Reserve0, edge.Reserve1, edge.Fee)
		if output == nil || output.Sign() <= 0 {
			return nil, nil
		}
		amounts[i+1] = output
		current = output
	}

	return amounts, current
}

// calculateSwapOutput calculates the output amount for a constant product AMM swap.
// Formula: amountOut = (reserveOut * amountIn * (1-fee)) / (reserveIn + amountIn * (1-fee))
func calculateSwapOutput(amountIn, reserveIn, reserveOut *big.Int, feeRate float64) *big.Int {
	if amountIn == nil || reserveIn == nil || reserveOut == nil {
		return nil
	}
	if amountIn.Sign() <= 0 || reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return nil
	}

	// Calculate fee-adjusted amount: amountInWithFee = amountIn * (1 - feeRate) * 1000
	// We use 1000 as a multiplier for precision
	feeMultiplier := int64((1 - feeRate) * 10000)
	amountInWithFee := new(big.Int).Mul(amountIn, big.NewInt(feeMultiplier))

	// numerator = reserveOut * amountInWithFee
	numerator := new(big.Int).Mul(reserveOut, amountInWithFee)

	// denominator = reserveIn * 10000 + amountInWithFee
	denominator := new(big.Int).Mul(reserveIn, big.NewInt(10000))
	denominator.Add(denominator, amountInWithFee)

	if denominator.Sign() <= 0 {
		return nil
	}

	// amountOut = numerator / denominator
	amountOut := new(big.Int).Div(numerator, denominator)

	return amountOut
}

// CalculateOptimalInput finds the optimal input amount for maximum profit.
// Uses binary search to find the sweet spot between too little (low absolute profit)
// and too much (excessive slippage).
func CalculateOptimalInput(cycle *Cycle, snap *graph.Snapshot, minProfitFactor float64) *big.Int {
	if cycle == nil || len(cycle.Edges) == 0 {
		return nil
	}

	// Get the first edge's input reserve as upper bound
	upperBound := new(big.Int).Div(cycle.Edges[0].Reserve0, big.NewInt(10)) // 10% max
	lowerBound := big.NewInt(1000) // Minimum reasonable amount

	if upperBound.Cmp(lowerBound) <= 0 {
		return nil
	}

	var bestInput *big.Int
	var bestProfit *big.Int

	// Binary search for optimal input
	iterations := 20 // Enough for reasonable precision
	for i := 0; i < iterations; i++ {
		mid := new(big.Int).Add(lowerBound, upperBound)
		mid.Div(mid, big.NewInt(2))

		if mid.Cmp(lowerBound) <= 0 {
			break
		}

		// Simulate with mid value
		result := simulateWithInput(cycle, mid)
		if result == nil || !result.IsProfitable {
			upperBound = mid
			continue
		}

		if bestProfit == nil || result.EstimatedProfitWei.Cmp(bestProfit) > 0 {
			bestInput = new(big.Int).Set(mid)
			bestProfit = new(big.Int).Set(result.EstimatedProfitWei)
		}

		// Try higher input for potentially more profit
		lowerBound = mid
	}

	return bestInput
}

// simulateWithInput simulates a cycle with a specific input amount.
func simulateWithInput(cycle *Cycle, inputAmount *big.Int) *SimulationResult {
	amounts, output := simulateSwaps(cycle, inputAmount)
	if output == nil || output.Sign() <= 0 {
		return &SimulationResult{IsProfitable: false}
	}

	profit := new(big.Int).Sub(output, inputAmount)
	if profit.Sign() <= 0 {
		return &SimulationResult{IsProfitable: false}
	}

	inputFloat := new(big.Float).SetInt(inputAmount)
	outputFloat := new(big.Float).SetInt(output)
	profitFactorFloat := new(big.Float).Quo(outputFloat, inputFloat)
	profitFactor, _ := profitFactorFloat.Float64()

	return &SimulationResult{
		MaxInputWei:         inputAmount,
		EstimatedOutputWei:  output,
		EstimatedProfitWei:  profit,
		ProfitFactor:        profitFactor,
		IsProfitable:        profitFactor > 1.0,
		IntermediateAmounts: amounts,
	}
}
