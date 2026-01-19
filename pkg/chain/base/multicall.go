package base

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Multicall3 contract address (same on all EVM chains)
var Multicall3Address = common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11")

// Multicall3 ABI for aggregate3
const Multicall3ABIJSON = `[
	{
		"inputs": [
			{
				"components": [
					{"internalType": "address", "name": "target", "type": "address"},
					{"internalType": "bool", "name": "allowFailure", "type": "bool"},
					{"internalType": "bytes", "name": "callData", "type": "bytes"}
				],
				"internalType": "struct Multicall3.Call3[]",
				"name": "calls",
				"type": "tuple[]"
			}
		],
		"name": "aggregate3",
		"outputs": [
			{
				"components": [
					{"internalType": "bool", "name": "success", "type": "bool"},
					{"internalType": "bytes", "name": "returnData", "type": "bytes"}
				],
				"internalType": "struct Multicall3.Result[]",
				"name": "returnData",
				"type": "tuple[]"
			}
		],
		"stateMutability": "payable",
		"type": "function"
	}
]`

var Multicall3ABI abi.ABI

func init() {
	var err error
	Multicall3ABI, err = abi.JSON(strings.NewReader(Multicall3ABIJSON))
	if err != nil {
		panic("failed to parse Multicall3 ABI: " + err.Error())
	}
}

// ContractCall represents a single call to be batched
type ContractCall struct {
	Target   common.Address
	CallData []byte
}

// CallResult represents the result of a single call
type CallResult struct {
	Success bool
	Data    []byte
}

// BatchCallContract executes multiple contract calls in a single RPC request using Multicall3
func (c *Client) BatchCallContract(ctx context.Context, calls []ContractCall) ([]CallResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	// Build the Call3 structs for aggregate3
	type Call3 struct {
		Target       common.Address
		AllowFailure bool
		CallData     []byte
	}

	call3s := make([]Call3, len(calls))
	for i, call := range calls {
		call3s[i] = Call3{
			Target:       call.Target,
			AllowFailure: true, // Allow individual calls to fail
			CallData:     call.CallData,
		}
	}

	// Pack the aggregate3 call
	data, err := Multicall3ABI.Pack("aggregate3", call3s)
	if err != nil {
		return nil, fmt.Errorf("failed to pack aggregate3 call: %w", err)
	}

	// Execute the multicall with retry logic
	var result []byte
	err = c.retryCall(func() error {
		var callErr error
		msg := ethereum.CallMsg{
			To:   &Multicall3Address,
			Data: data,
		}
		result, callErr = c.ethClient.CallContract(ctx, msg, nil)
		return callErr
	}, 3)
	if err != nil {
		return nil, fmt.Errorf("multicall failed: %w", err)
	}

	// Unpack the results
	type Result struct {
		Success    bool
		ReturnData []byte
	}

	var results []Result
	err = Multicall3ABI.UnpackIntoInterface(&results, "aggregate3", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack aggregate3 result: %w", err)
	}

	// Convert to CallResult
	callResults := make([]CallResult, len(results))
	for i, r := range results {
		callResults[i] = CallResult{
			Success: r.Success,
			Data:    r.ReturnData,
		}
	}

	return callResults, nil
}

// retryCall executes a function with exponential backoff retry
func (c *Client) retryCall(fn func() error, maxRetries int) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		// Check if it's a transient error worth retrying
		errStr := err.Error()
		if !isTransientError(errStr) {
			return err
		}

		// Exponential backoff: 100ms, 200ms, 400ms
		backoff := time.Duration(100<<attempt) * time.Millisecond
		time.Sleep(backoff)
	}
	return lastErr
}

// isTransientError checks if an error is likely transient and worth retrying
func isTransientError(errStr string) bool {
	transientPatterns := []string{
		"EOF",
		"connection reset",
		"timeout",
		"temporary failure",
		"too many requests",
		"rate limit",
		"503",
		"502",
		"504",
	}
	errLower := strings.ToLower(errStr)
	for _, pattern := range transientPatterns {
		if strings.Contains(errLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
