package aerodrome

import (
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Aerodrome V2 Factory address on Base
var V2FactoryAddress = common.HexToAddress("0x420DD381b31aEf6683db6B902084cB0FFECe40Da")

// ABI definitions for Aerodrome V2 contracts

// V2 Factory ABI - only the functions we need
const V2FactoryABIJSON = `[
	{
		"inputs": [],
		"name": "allPoolsLength",
		"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
		"name": "allPools",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// V2 Pool ABI - only the functions we need
const V2PoolABIJSON = `[
	{
		"inputs": [],
		"name": "getReserves",
		"outputs": [
			{"internalType": "uint256", "name": "_reserve0", "type": "uint256"},
			{"internalType": "uint256", "name": "_reserve1", "type": "uint256"},
			{"internalType": "uint256", "name": "_blockTimestampLast", "type": "uint256"}
		],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "token0",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "token1",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "stable",
		"outputs": [{"internalType": "bool", "name": "", "type": "bool"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// ERC20 ABI - only the functions we need
const ERC20ABIJSON = `[
	{
		"inputs": [],
		"name": "decimals",
		"outputs": [{"internalType": "uint8", "name": "", "type": "uint8"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "symbol",
		"outputs": [{"internalType": "string", "name": "", "type": "string"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "name",
		"outputs": [{"internalType": "string", "name": "", "type": "string"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

var (
	V2FactoryABI abi.ABI
	V2PoolABI    abi.ABI
	ERC20ABI     abi.ABI
)

func init() {
	var err error

	V2FactoryABI, err = abi.JSON(strings.NewReader(V2FactoryABIJSON))
	if err != nil {
		panic("failed to parse V2 Factory ABI: " + err.Error())
	}

	V2PoolABI, err = abi.JSON(strings.NewReader(V2PoolABIJSON))
	if err != nil {
		panic("failed to parse V2 Pool ABI: " + err.Error())
	}

	ERC20ABI, err = abi.JSON(strings.NewReader(ERC20ABIJSON))
	if err != nil {
		panic("failed to parse ERC20 ABI: " + err.Error())
	}
}
