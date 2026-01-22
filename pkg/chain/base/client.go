package base

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Client struct {
	ethClient   *ethclient.Client
	rateLimiter *time.Ticker
}

func NewClient(rpcURL string) (*Client, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Base RPC: %w", err)
	}

	return &Client{
		ethClient:   client,
		rateLimiter: time.NewTicker(100 * time.Millisecond), // 10 requests per second
	}, nil
}

func (c *Client) Close() {
	c.ethClient.Close()
	c.rateLimiter.Stop()
}

func (c *Client) rateLimit() {
	<-c.rateLimiter.C
}

func (c *Client) CallContract(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	c.rateLimit()

	msg := ethereum.CallMsg{
		To:   &to,
		Data: data,
	}

	result, err := c.ethClient.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("contract call failed: %w", err)
	}

	return result, nil
}

func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	return c.ethClient.ChainID(ctx)
}

// BlockNumber returns the current block number.
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	c.rateLimit()
	return c.ethClient.BlockNumber(ctx)
}

// FilterLogs retrieves logs matching the given filter query.
func (c *Client) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	c.rateLimit()
	return c.ethClient.FilterLogs(ctx, query)
}
