package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// Metrics holds all Prometheus metrics for the arbitrage detection system.
type Metrics struct {
	// Event metrics
	EventsReceived *prometheus.CounterVec
	EventLatency   prometheus.Histogram

	// Graph metrics
	GraphNodes prometheus.Gauge
	GraphEdges prometheus.Gauge

	// Snapshot metrics
	SnapshotLatency prometheus.Histogram

	// Detection metrics
	DetectionLatency       prometheus.Histogram
	CyclesFound            prometheus.Counter
	ProfitableOpportunities prometheus.Counter

	// Pipeline metrics
	PipelineLatency prometheus.Histogram

	// System metrics
	PoolsTracked     prometheus.Gauge
	WebSocketStatus  prometheus.Gauge
	LastBlockSeen    prometheus.Gauge
	BootstrapLatency prometheus.Histogram

	server *http.Server
}

// New creates and registers all Prometheus metrics.
func New() *Metrics {
	m := &Metrics{
		EventsReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "arb_events_received_total",
				Help: "Total number of events received by type",
			},
			[]string{"type"},
		),
		EventLatency: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "arb_event_latency_seconds",
				Help:    "Latency from block timestamp to event processing",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~32s
			},
		),
		GraphNodes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arb_graph_nodes",
				Help: "Current number of nodes (tokens) in the graph",
			},
		),
		GraphEdges: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arb_graph_edges",
				Help: "Current number of edges (pool directions) in the graph",
			},
		),
		SnapshotLatency: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "arb_snapshot_latency_seconds",
				Help:    "Time to create a graph snapshot",
				Buckets: prometheus.ExponentialBuckets(0.0001, 2, 12), // 0.1ms to ~400ms
			},
		),
		DetectionLatency: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "arb_detection_latency_seconds",
				Help:    "Time to run arbitrage detection on a snapshot",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
			},
		),
		CyclesFound: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "arb_cycles_found_total",
				Help: "Total number of negative cycles found",
			},
		),
		ProfitableOpportunities: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "arb_profitable_opportunities_total",
				Help: "Total number of profitable opportunities after simulation",
			},
		),
		PipelineLatency: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "arb_pipeline_latency_seconds",
				Help:    "Full pipeline latency from event receipt to opportunity identification",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
			},
		),
		PoolsTracked: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arb_pools_tracked",
				Help: "Number of pools currently being tracked",
			},
		),
		WebSocketStatus: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arb_websocket_connected",
				Help: "WebSocket connection status (1=connected, 0=disconnected)",
			},
		),
		LastBlockSeen: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "arb_last_block_seen",
				Help: "Last block number seen from events",
			},
		),
		BootstrapLatency: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "arb_bootstrap_latency_seconds",
				Help:    "Time to bootstrap pool data",
				Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17 minutes
			},
		),
	}

	// Register all metrics
	prometheus.MustRegister(
		m.EventsReceived,
		m.EventLatency,
		m.GraphNodes,
		m.GraphEdges,
		m.SnapshotLatency,
		m.DetectionLatency,
		m.CyclesFound,
		m.ProfitableOpportunities,
		m.PipelineLatency,
		m.PoolsTracked,
		m.WebSocketStatus,
		m.LastBlockSeen,
		m.BootstrapLatency,
	)

	return m
}

// StartServer starts the HTTP server for Prometheus metrics.
func (m *Metrics) StartServer(port int, path string) error {
	mux := http.NewServeMux()
	mux.Handle(path, promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	m.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		log.Info().Int("port", port).Str("path", path).Msg("Starting metrics server")
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Metrics server error")
		}
	}()

	return nil
}

// Shutdown gracefully stops the metrics server.
func (m *Metrics) Shutdown(ctx context.Context) error {
	if m.server != nil {
		return m.server.Shutdown(ctx)
	}
	return nil
}

// RecordEventReceived increments the event counter for the given type.
func (m *Metrics) RecordEventReceived(eventType string) {
	m.EventsReceived.WithLabelValues(eventType).Inc()
}

// RecordEventLatency records the latency from block timestamp to processing.
func (m *Metrics) RecordEventLatency(blockTime time.Time) {
	latency := time.Since(blockTime).Seconds()
	m.EventLatency.Observe(latency)
}

// RecordGraphStats updates the graph node and edge counts.
func (m *Metrics) RecordGraphStats(nodes, edges int) {
	m.GraphNodes.Set(float64(nodes))
	m.GraphEdges.Set(float64(edges))
}

// RecordSnapshotLatency records the time to create a snapshot.
func (m *Metrics) RecordSnapshotLatency(d time.Duration) {
	m.SnapshotLatency.Observe(d.Seconds())
}

// RecordDetectionLatency records the time to run detection.
func (m *Metrics) RecordDetectionLatency(d time.Duration) {
	m.DetectionLatency.Observe(d.Seconds())
}

// RecordCycleFound increments the cycles found counter.
func (m *Metrics) RecordCycleFound() {
	m.CyclesFound.Inc()
}

// RecordProfitableOpportunity increments the profitable opportunities counter.
func (m *Metrics) RecordProfitableOpportunity() {
	m.ProfitableOpportunities.Inc()
}

// RecordPipelineLatency records the full pipeline latency.
func (m *Metrics) RecordPipelineLatency(d time.Duration) {
	m.PipelineLatency.Observe(d.Seconds())
}

// SetPoolsTracked sets the current number of tracked pools.
func (m *Metrics) SetPoolsTracked(count int) {
	m.PoolsTracked.Set(float64(count))
}

// SetWebSocketConnected sets the WebSocket connection status.
func (m *Metrics) SetWebSocketConnected(connected bool) {
	if connected {
		m.WebSocketStatus.Set(1)
	} else {
		m.WebSocketStatus.Set(0)
	}
}

// SetLastBlockSeen sets the last block number seen.
func (m *Metrics) SetLastBlockSeen(block uint64) {
	m.LastBlockSeen.Set(float64(block))
}

// RecordBootstrapLatency records the bootstrap duration.
func (m *Metrics) RecordBootstrapLatency(d time.Duration) {
	m.BootstrapLatency.Observe(d.Seconds())
}
