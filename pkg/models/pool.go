package models

import (
	"math"

	"github.com/shopspring/decimal"
)

type Pool struct {
	Address                string
	Exchange               string
	Chain                  string
	Asset1ID               string
	Asset1Name             string
	Asset1Decimals         uint8
	Asset2ID               string
	Asset2Name             string
	Asset2Decimals         uint8
	Liquidity1             decimal.Decimal
	Liquidity2             decimal.Decimal
	Fee                    decimal.Decimal // Pool fee as a multiplier (e.g., 0.997 for 0.3% fee)
	ExchangeRate           decimal.Decimal
	ReciprocalExchangeRate decimal.Decimal
}

// minValidRate is the minimum exchange rate we consider valid for log calculations
// Below this, the rate is essentially zero and would produce infinity
const minValidRate = 1e-18

// maxNegLogRate is the maximum negative log rate we return to avoid infinity
// This represents an exchange rate of approximately 1e-100
const maxNegLogRate = 230.0

func (p *Pool) NegativeLogExchangeRate() decimal.Decimal {
	f64, _ := p.ExchangeRate.Float64()
	return safeNegativeLog(f64)
}

func (p *Pool) NegativeLogReciprocalExchangeRate() decimal.Decimal {
	f64, _ := p.ReciprocalExchangeRate.Float64()
	return safeNegativeLog(f64)
}

// safeNegativeLog computes -log(x) safely, handling edge cases
func safeNegativeLog(x float64) decimal.Decimal {
	// Handle zero, negative, or extremely small values
	if x <= minValidRate {
		return decimal.NewFromFloat(maxNegLogRate)
	}

	result := -math.Log(x)

	// Handle infinity or NaN
	if math.IsInf(result, 0) || math.IsNaN(result) {
		if result > 0 {
			return decimal.NewFromFloat(maxNegLogRate)
		}
		return decimal.NewFromFloat(-maxNegLogRate)
	}

	return decimal.NewFromFloat(result)
}

// IsValidForArbitrage checks if this pool has valid exchange rates for arbitrage detection
func (p *Pool) IsValidForArbitrage() bool {
	rate, _ := p.ExchangeRate.Float64()
	recipRate, _ := p.ReciprocalExchangeRate.Float64()

	// Both rates must be positive and finite
	if rate <= 0 || recipRate <= 0 {
		return false
	}
	if math.IsInf(rate, 0) || math.IsNaN(rate) {
		return false
	}
	if math.IsInf(recipRate, 0) || math.IsNaN(recipRate) {
		return false
	}

	return true
}
