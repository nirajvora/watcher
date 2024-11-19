package models

type Pool struct {
    ID                      string
    Exchange                string
    Chain                   string
    Asset1Name              string
    Asset1ID                string
    Asset2Name              string
    Asset2ID                string
    Liquidity1              float64
    Liquidity2              float64
    ExchangeRate            float64
    ReciprocalExchangeRate  float64
}