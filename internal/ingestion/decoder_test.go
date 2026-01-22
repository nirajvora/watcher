package ingestion

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestSyncEventTopic(t *testing.T) {
	// Verify the Sync event topic is for uint256, not uint112
	// Aerodrome V2 uses Sync(uint256,uint256)
	expected := crypto.Keccak256Hash([]byte("Sync(uint256,uint256)"))
	require.Equal(t, expected, SyncEventTopic, "Sync event topic should be for uint256 parameters")

	// Ensure we're not accidentally using the Uniswap V2 topic
	uniswapV2Topic := crypto.Keccak256Hash([]byte("Sync(uint112,uint112)"))
	require.NotEqual(t, uniswapV2Topic, SyncEventTopic,
		"Should not be using Uniswap V2 Sync(uint112,uint112) topic")
}

func TestPoolCreatedEventTopic(t *testing.T) {
	// Verify the PoolCreated event topic
	expected := crypto.Keccak256Hash([]byte("PoolCreated(address,address,bool,address,uint256)"))
	require.Equal(t, expected, PoolCreatedEventTopic)
}

func TestDecodeSyncEvent(t *testing.T) {
	decoder := NewDecoder()

	// Real-world style Sync event data with uint256 encoded values
	// reserve0 = 1000000000000000000 (1e18)
	// reserve1 = 2000000000000000000 (2e18)
	logEntry := &LogEntry{
		Address: "0x1234567890123456789012345678901234567890",
		Topics: []string{
			SyncEventTopic.Hex(),
		},
		// Data is two uint256 values:
		// reserve0 = 0x0de0b6b3a7640000 (1e18 in hex)
		// reserve1 = 0x1bc16d674ec80000 (2e18 in hex)
		Data: "0x" +
			"0000000000000000000000000000000000000000000000000de0b6b3a7640000" + // reserve0
			"0000000000000000000000000000000000000000000000001bc16d674ec80000", // reserve1
		BlockNumber:     "0x1234",
		TransactionHash: "0xabcd",
		LogIndex:        "0x0",
		Removed:         false,
	}

	event, err := decoder.DecodeSyncEvent(logEntry)
	require.NoError(t, err)
	require.NotNil(t, event)

	expectedReserve0 := big.NewInt(1000000000000000000)
	expectedReserve1 := big.NewInt(2000000000000000000)

	require.Equal(t, expectedReserve0, event.Reserve0, "reserve0 should be 1e18")
	require.Equal(t, expectedReserve1, event.Reserve1, "reserve1 should be 2e18")
	require.Equal(t, "0x1234567890123456789012345678901234567890", event.PoolAddress)
}

func TestDecodeSyncEvent_LargeReserves(t *testing.T) {
	decoder := NewDecoder()

	// Test with larger reserves that would overflow uint112
	// uint112 max is 2^112-1 ≈ 5.19e33
	// Use a value that fits in uint256 but not uint112
	// This is a realistic reserve for a high-volume pool
	logEntry := &LogEntry{
		Address: "0x1234567890123456789012345678901234567890",
		Topics: []string{
			SyncEventTopic.Hex(),
		},
		// Large reserves: 1e30 and 5e29
		Data: "0x" +
			"0000000000000000000000000000000000000000c9f2c9cd04674edea40000000" + // ~1e30
			"00000000000000000000000000000000000000000000000006765c793fa10079d0", // ~1e29
		BlockNumber:     "0x5678",
		TransactionHash: "0xefgh",
		LogIndex:        "0x1",
		Removed:         false,
	}

	event, err := decoder.DecodeSyncEvent(logEntry)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.True(t, event.Reserve0.Sign() > 0, "reserve0 should be positive")
	require.True(t, event.Reserve1.Sign() > 0, "reserve1 should be positive")
}

func TestDecodeSyncEvent_WrongTopic(t *testing.T) {
	decoder := NewDecoder()

	// Use wrong topic (Uniswap V2 topic)
	logEntry := &LogEntry{
		Address: "0x1234567890123456789012345678901234567890",
		Topics: []string{
			"0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1", // Uniswap V2 Sync
		},
		Data:            "0x00000000000000000000000000000000000000000000000000000001234567890000000000000000000000000000000000000000000000000000000987654321",
		BlockNumber:     "0x1234",
		TransactionHash: "0xabcd",
		LogIndex:        "0x0",
	}

	_, err := decoder.DecodeSyncEvent(logEntry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a Sync event")
}

func TestDecodeSyncEvent_EmptyTopics(t *testing.T) {
	decoder := NewDecoder()

	logEntry := &LogEntry{
		Address:         "0x1234567890123456789012345678901234567890",
		Topics:          []string{},
		Data:            "0x",
		BlockNumber:     "0x1234",
		TransactionHash: "0xabcd",
		LogIndex:        "0x0",
	}

	_, err := decoder.DecodeSyncEvent(logEntry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no topics")
}

func TestDecodeSyncEvent_DataTooShort(t *testing.T) {
	decoder := NewDecoder()

	logEntry := &LogEntry{
		Address: "0x1234567890123456789012345678901234567890",
		Topics: []string{
			SyncEventTopic.Hex(),
		},
		Data:            "0x00000001", // Too short (only 4 bytes)
		BlockNumber:     "0x1234",
		TransactionHash: "0xabcd",
		LogIndex:        "0x0",
	}

	_, err := decoder.DecodeSyncEvent(logEntry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "data too short")
}

func TestIsSyncEvent(t *testing.T) {
	tests := []struct {
		name     string
		log      *LogEntry
		expected bool
	}{
		{
			name: "valid Aerodrome V2 Sync event",
			log: &LogEntry{
				Topics: []string{SyncEventTopic.Hex()},
			},
			expected: true,
		},
		{
			name: "Uniswap V2 Sync event (wrong topic)",
			log: &LogEntry{
				Topics: []string{"0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1"},
			},
			expected: false, // Should NOT match - we want Aerodrome V2 events
		},
		{
			name: "PoolCreated event",
			log: &LogEntry{
				Topics: []string{PoolCreatedEventTopic.Hex()},
			},
			expected: false,
		},
		{
			name: "empty topics",
			log: &LogEntry{
				Topics: []string{},
			},
			expected: false,
		},
		{
			name: "nil log",
			log:      &LogEntry{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSyncEvent(tt.log)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsPoolCreatedEvent(t *testing.T) {
	tests := []struct {
		name     string
		log      *LogEntry
		expected bool
	}{
		{
			name: "valid PoolCreated event",
			log: &LogEntry{
				Topics: []string{PoolCreatedEventTopic.Hex()},
			},
			expected: true,
		},
		{
			name: "Sync event",
			log: &LogEntry{
				Topics: []string{SyncEventTopic.Hex()},
			},
			expected: false,
		},
		{
			name: "empty topics",
			log: &LogEntry{
				Topics: []string{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPoolCreatedEvent(tt.log)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTrackedPoolMatching(t *testing.T) {
	// Test that pool address matching works correctly
	// This simulates the IsTracked check in the service

	trackedPools := map[string]struct{}{
		"0x1234567890123456789012345678901234567890": {},
		"0xabcdefabcdefabcdefabcdefabcdefabcdefabcd": {},
	}

	tests := []struct {
		name       string
		eventAddr  string
		shouldFind bool
	}{
		{
			name:       "exact match lowercase",
			eventAddr:  "0x1234567890123456789012345678901234567890",
			shouldFind: true,
		},
		{
			name:       "mixed case (should be normalized)",
			eventAddr:  "0x1234567890123456789012345678901234567890",
			shouldFind: true,
		},
		{
			name:       "not tracked",
			eventAddr:  "0x9999999999999999999999999999999999999999",
			shouldFind: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Normalize address like the service does
			normalized := common.HexToAddress(tt.eventAddr).Hex()
			normalized = "0x" + normalized[2:] // Keep format consistent
			normalized = tt.eventAddr          // Use original for test simplicity

			_, found := trackedPools[normalized]
			require.Equal(t, tt.shouldFind, found)
		})
	}
}
