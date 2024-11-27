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
	Asset2ID               string
	Asset2Name             string
	Liquidity1             decimal.Decimal
	Liquidity2             decimal.Decimal
	ExchangeRate           decimal.Decimal
	ReciprocalExchangeRate decimal.Decimal
}

func (p *Pool) NegativeLogExchangeRate() decimal.Decimal {
	f64, _ := p.ExchangeRate.Float64()
	return decimal.NewFromFloat(-math.Log(f64))
}

func (p *Pool) NegativeLogReciprocalExchangeRate() decimal.Decimal {
	f64, _ := p.ReciprocalExchangeRate.Float64()
	return decimal.NewFromFloat(-math.Log(f64))
}
