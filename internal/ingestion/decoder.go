package ingestion

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Event topics (keccak256 hashes of event signatures)
var (
	// Sync(uint112,uint112) - Emitted when reserves are updated
	SyncEventTopic = crypto.Keccak256Hash([]byte("Sync(uint112,uint112)"))

	// PoolCreated(address,address,bool,address,uint256) - Emitted when a new pool is created
	PoolCreatedEventTopic = crypto.Keccak256Hash([]byte("PoolCreated(address,address,bool,address,uint256)"))
)

// SyncEvent represents a decoded Sync event.
type SyncEvent struct {
	PoolAddress string
	Reserve0    *big.Int
	Reserve1    *big.Int
	BlockNumber uint64
	LogIndex    uint
	TxHash      string
	Timestamp   time.Time
}

// PoolCreatedEvent represents a decoded PoolCreated event.
type PoolCreatedEvent struct {
	Token0      string
	Token1      string
	IsStable    bool
	PoolAddress string
	PoolIndex   *big.Int
	BlockNumber uint64
	LogIndex    uint
	TxHash      string
}

// LogEntry represents a raw log entry from the WebSocket.
type LogEntry struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	BlockHash        string   `json:"blockHash"`
	LogIndex         string   `json:"logIndex"`
	Removed          bool     `json:"removed"`
}

// Decoder handles event decoding.
type Decoder struct {
	syncABI        abi.Arguments
	poolCreatedABI abi.Arguments
}

// NewDecoder creates a new event decoder.
func NewDecoder() *Decoder {
	// Sync event: Sync(uint112 reserve0, uint112 reserve1)
	// Both values are in the data field (not indexed)
	uint112Type, _ := abi.NewType("uint112", "", nil)
	syncABI := abi.Arguments{
		{Type: uint112Type, Name: "reserve0"},
		{Type: uint112Type, Name: "reserve1"},
	}

	// PoolCreated event: PoolCreated(address token0, address token1, bool stable, address pool, uint256)
	// token0 and token1 are indexed (in topics), rest in data
	addressType, _ := abi.NewType("address", "", nil)
	boolType, _ := abi.NewType("bool", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)

	poolCreatedABI := abi.Arguments{
		{Type: boolType, Name: "stable"},
		{Type: addressType, Name: "pool"},
		{Type: uint256Type, Name: "index"},
	}

	return &Decoder{
		syncABI:        syncABI,
		poolCreatedABI: poolCreatedABI,
	}
}

// DecodeSyncEvent decodes a Sync event from a log entry.
func (d *Decoder) DecodeSyncEvent(log *LogEntry) (*SyncEvent, error) {
	if len(log.Topics) < 1 {
		return nil, fmt.Errorf("no topics in log")
	}

	// Verify event signature
	topic := common.HexToHash(log.Topics[0])
	if topic != SyncEventTopic {
		return nil, fmt.Errorf("not a Sync event: %s", log.Topics[0])
	}

	// Decode data
	data := common.FromHex(log.Data)
	if len(data) < 64 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	values, err := d.syncABI.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("unpacking sync data: %w", err)
	}

	reserve0, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid reserve0 type")
	}

	reserve1, ok := values[1].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid reserve1 type")
	}

	blockNum, err := hexToUint64(log.BlockNumber)
	if err != nil {
		return nil, fmt.Errorf("parsing block number: %w", err)
	}

	logIdx, err := hexToUint(log.LogIndex)
	if err != nil {
		return nil, fmt.Errorf("parsing log index: %w", err)
	}

	return &SyncEvent{
		PoolAddress: strings.ToLower(log.Address),
		Reserve0:    reserve0,
		Reserve1:    reserve1,
		BlockNumber: blockNum,
		LogIndex:    logIdx,
		TxHash:      log.TransactionHash,
		Timestamp:   time.Now(),
	}, nil
}

// DecodePoolCreatedEvent decodes a PoolCreated event from a log entry.
func (d *Decoder) DecodePoolCreatedEvent(log *LogEntry) (*PoolCreatedEvent, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("insufficient topics for PoolCreated: %d", len(log.Topics))
	}

	// Verify event signature
	topic := common.HexToHash(log.Topics[0])
	if topic != PoolCreatedEventTopic {
		return nil, fmt.Errorf("not a PoolCreated event: %s", log.Topics[0])
	}

	// Token0 and Token1 are indexed (in topics[1] and topics[2])
	token0 := common.HexToAddress(log.Topics[1]).Hex()
	token1 := common.HexToAddress(log.Topics[2]).Hex()

	// Decode data for stable, pool address, and index
	data := common.FromHex(log.Data)
	if len(data) < 96 {
		return nil, fmt.Errorf("data too short for PoolCreated: %d bytes", len(data))
	}

	values, err := d.poolCreatedABI.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("unpacking PoolCreated data: %w", err)
	}

	isStable, ok := values[0].(bool)
	if !ok {
		return nil, fmt.Errorf("invalid stable type")
	}

	poolAddr, ok := values[1].(common.Address)
	if !ok {
		return nil, fmt.Errorf("invalid pool address type")
	}

	poolIndex, ok := values[2].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid pool index type")
	}

	blockNum, err := hexToUint64(log.BlockNumber)
	if err != nil {
		return nil, fmt.Errorf("parsing block number: %w", err)
	}

	logIdx, err := hexToUint(log.LogIndex)
	if err != nil {
		return nil, fmt.Errorf("parsing log index: %w", err)
	}

	return &PoolCreatedEvent{
		Token0:      strings.ToLower(token0),
		Token1:      strings.ToLower(token1),
		IsStable:    isStable,
		PoolAddress: strings.ToLower(poolAddr.Hex()),
		PoolIndex:   poolIndex,
		BlockNumber: blockNum,
		LogIndex:    logIdx,
		TxHash:      log.TransactionHash,
	}, nil
}

// IsSyncEvent checks if a log entry is a Sync event.
func IsSyncEvent(log *LogEntry) bool {
	if len(log.Topics) < 1 {
		return false
	}
	return common.HexToHash(log.Topics[0]) == SyncEventTopic
}

// IsPoolCreatedEvent checks if a log entry is a PoolCreated event.
func IsPoolCreatedEvent(log *LogEntry) bool {
	if len(log.Topics) < 1 {
		return false
	}
	return common.HexToHash(log.Topics[0]) == PoolCreatedEventTopic
}

func hexToUint64(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	var val uint64
	_, err := fmt.Sscanf(s, "%x", &val)
	return val, err
}

func hexToUint(s string) (uint, error) {
	s = strings.TrimPrefix(s, "0x")
	var val uint
	_, err := fmt.Sscanf(s, "%x", &val)
	return val, err
}
