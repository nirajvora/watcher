package aerodrome

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// PoolInfo holds raw pool data from contract calls
type PoolInfo struct {
	Address  common.Address
	Token0   common.Address
	Token1   common.Address
	Reserve0 *big.Int
	Reserve1 *big.Int
	Stable   bool
}

// TokenInfo holds token metadata
type TokenInfo struct {
	Address  common.Address
	Symbol   string
	Decimals uint8
}

// Known token addresses on Base
var (
	WETHAddress  = common.HexToAddress("0x4200000000000000000000000000000000000006")
	USDCAddress  = common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913")
	USDbCAddress = common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA")
	ZeroAddress  = common.HexToAddress("0x0000000000000000000000000000000000000000")
)
