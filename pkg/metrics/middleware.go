package metrics

import (
	"github.com/panjf2000/gnet/v2"
	"time"

	"github.com/pzhenzhou/elika/pkg/respio"
)

// ProxyMetricsMiddleWare provides metrics collection for the  proxy server
type ProxyMetricsMiddleWare struct {
	collector            ProxyMetricsCollector
	recordCommandLatency bool // Controls whether to record per-command latency metrics
}

// NewProxyMetricsMiddleware creates a new proxy metrics middleware
func NewProxyMetricsMiddleware(collector ProxyMetricsCollector) *ProxyMetricsMiddleWare {
	return &ProxyMetricsMiddleWare{
		collector:            collector,
		recordCommandLatency: true, // Enable command-based latency by default
	}
}

// NewProxyMetricsMiddlewareWithOptions creates a new proxy metrics middleware with custom options
func NewProxyMetricsMiddlewareWithOptions(collector ProxyMetricsCollector, recordCommandLatency bool) *ProxyMetricsMiddleWare {
	return &ProxyMetricsMiddleWare{
		collector:            collector,
		recordCommandLatency: recordCommandLatency,
	}
}

func (m *ProxyMetricsMiddleWare) GetCollector() ProxyMetricsCollector {
	return m.collector
}

// SetRecordCommandLatency enables or disables command-based latency recording
func (m *ProxyMetricsMiddleWare) SetRecordCommandLatency(enable bool) {
	m.recordCommandLatency = enable
}

// OnConnectionOpen tracks metrics when a connection is opened
func (m *ProxyMetricsMiddleWare) OnConnectionOpen() {
	m.collector.IncrementActiveConnections()
}

// OnConnectionClose tracks metrics when a connection is closed
func (m *ProxyMetricsMiddleWare) OnConnectionClose() {
	m.collector.DecrementActiveConnections()
}

// TrackCommand tracks metrics for a command
func (m *ProxyMetricsMiddleWare) TrackCommand(command string) {
	m.collector.IncrementCommandCounter(command)
}

// TrackLatency measures and records the end-to-end latency for a specific command
func (m *ProxyMetricsMiddleWare) TrackLatency(command string, start time.Time) {
	duration := time.Since(start)

	// Record command-specific latency only if enabled
	if m.recordCommandLatency {
		m.collector.RecordCommandLatency(command, duration)
	}

	// Always record in the overall metrics
	m.collector.RecordOverallLatency(duration)
}

// TrackForwardingLatency measures and records the forwarding latency for a specific command
func (m *ProxyMetricsMiddleWare) TrackForwardingLatency(command string, start time.Time) {
	duration := time.Since(start)

	// Record command-specific forwarding latency only if enabled
	if m.recordCommandLatency {
		m.collector.RecordCommandForwardingLatency(command, duration)
	}

	// Always record in the overall metrics
	m.collector.RecordOverallForwardingLatency(duration)
}

// TrackError increments the error counter for a specific error type
func (m *ProxyMetricsMiddleWare) TrackError(errorType string) {
	m.collector.IncrementErrorCounter(errorType)
}

// WrapDispatch wraps the command dispatch process with metrics
func (m *ProxyMetricsMiddleWare) WrapDispatch(packet *respio.RespPacket, fn func() error) error {
	// Convert []byte to string for the command
	command := string(packet.GetCommand())

	// Track command count
	m.TrackCommand(command)

	// Track end-to-end latency
	start := time.Now()

	// Execute the dispatch function
	err := fn()

	// Record latency after execution
	m.TrackLatency(command, start)

	// Track errors
	if err != nil {
		m.TrackError("dispatch_error")
	}

	return err
}

// WrapTraffic wraps the entire traffic handling process with metrics
// This provides a more accurate end-to-end latency measurement
func (m *ProxyMetricsMiddleWare) WrapTraffic(fn func() gnet.Action) gnet.Action {
	// Track overall latency for the entire traffic handling
	start := time.Now()

	// Execute the traffic handling function
	rs := fn()

	// Record overall latency without command specificity
	// This captures the true end-to-end latency including all processing
	m.collector.RecordOverallLatency(time.Since(start))

	return rs
}

// WrapForwarding wraps the forwarding process with metrics
func (m *ProxyMetricsMiddleWare) WrapForwarding(packet *respio.RespPacket, fn func() error) error {
	command := string(packet.GetCommand())
	// Track forwarding latency
	start := time.Now()

	// Execute the forwarding function
	err := fn()

	// Record forwarding latency
	m.TrackForwardingLatency(command, start)

	// Track errors
	if err != nil {
		m.TrackError("forwarding_error")
	}

	return err
}
