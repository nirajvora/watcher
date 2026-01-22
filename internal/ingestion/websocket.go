package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 1024 * 1024 // 1MB
)

// WSClient manages a WebSocket connection to an Ethereum node.
type WSClient struct {
	url  string
	conn *websocket.Conn
	mu   sync.Mutex

	// Subscription tracking
	subscriptionID string
	requestID      atomic.Int64

	// Message handling
	msgCh     chan json.RawMessage
	reconnect chan struct{}
	done      chan struct{}

	// State
	connected atomic.Bool
}

// NewWSClient creates a new WebSocket client.
func NewWSClient(url string) *WSClient {
	return &WSClient{
		url:       url,
		msgCh:     make(chan json.RawMessage, 1000),
		reconnect: make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
}

// Connect establishes a WebSocket connection.
func (c *WSClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("dialing websocket: %w", err)
	}

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	c.conn = conn
	c.connected.Store(true)

	log.Info().Str("url", c.url).Msg("WebSocket connected")
	return nil
}

// Close closes the WebSocket connection.
func (c *WSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.done)
	c.connected.Store(false)

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns true if the client is connected.
func (c *WSClient) IsConnected() bool {
	return c.connected.Load()
}

// Subscribe subscribes to log events for the given addresses and topics.
func (c *WSClient) Subscribe(ctx context.Context, addresses []string, topics []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	id := c.requestID.Add(1)

	// Build filter params
	filter := map[string]interface{}{
		"topics": []interface{}{topics},
	}
	if len(addresses) > 0 {
		filter["address"] = addresses
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "eth_subscribe",
		"params":  []interface{}{"logs", filter},
	}

	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("writing subscribe request: %w", err)
	}

	log.Info().
		Int64("id", id).
		Int("addresses", len(addresses)).
		Strs("topics", topics).
		Msg("Sent subscription request")

	return nil
}

// Unsubscribe removes a subscription.
func (c *WSClient) Unsubscribe(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil || c.subscriptionID == "" {
		return nil
	}

	id := c.requestID.Add(1)
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "eth_unsubscribe",
		"params":  []interface{}{c.subscriptionID},
	}

	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("writing unsubscribe request: %w", err)
	}

	c.subscriptionID = ""
	return nil
}

// ReadMessages reads messages from the WebSocket and sends them to the channel.
// Returns when the connection is closed or an error occurs.
func (c *WSClient) ReadMessages(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return nil
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			return fmt.Errorf("connection closed")
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			c.connected.Store(false)
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return fmt.Errorf("reading message: %w", err)
		}

		// Parse the message to check type
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int64          `json:"id"`
			Result  json.RawMessage `json:"result"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}

		if err := json.Unmarshal(message, &msg); err != nil {
			log.Warn().Err(err).Str("message", string(message)).Msg("Failed to parse message")
			continue
		}

		// Handle subscription response
		if msg.ID != nil && msg.Result != nil {
			var subID string
			if err := json.Unmarshal(msg.Result, &subID); err == nil && subID != "" {
				c.mu.Lock()
				c.subscriptionID = subID
				c.mu.Unlock()
				log.Info().Str("subscription_id", subID).Msg("Subscription confirmed")
			}
			continue
		}

		// Handle errors
		if msg.Error != nil {
			log.Error().
				Int("code", msg.Error.Code).
				Str("message", msg.Error.Message).
				Msg("WebSocket error")
			continue
		}

		// Handle subscription notifications
		if msg.Method == "eth_subscription" && msg.Params != nil {
			select {
			case c.msgCh <- msg.Params:
			default:
				log.Warn().Msg("Message channel full, discarding message")
			}
		}
	}
}

// Messages returns the channel for received messages.
func (c *WSClient) Messages() <-chan json.RawMessage {
	return c.msgCh
}

// Ping sends a ping to keep the connection alive.
func (c *WSClient) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteMessage(websocket.PingMessage, nil)
}

// StartPingLoop starts a goroutine that sends periodic pings.
func (c *WSClient) StartPingLoop(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.Ping(ctx); err != nil {
				log.Warn().Err(err).Msg("Ping failed")
			}
		}
	}
}
