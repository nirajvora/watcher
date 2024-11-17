package models

type Pool struct {
    ID           string
    Exchange     string
    Asset1       string
    Asset2       string
    Liquidity1   float64
    Liquidity2   float64
    ExchangeRate float64
}