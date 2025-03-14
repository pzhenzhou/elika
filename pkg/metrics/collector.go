package metrics

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/pzhenzhou/elika/pkg/common"

	"github.com/gin-gonic/gin"
	gometrics "github.com/hashicorp/go-metrics"
	"github.com/hashicorp/go-metrics/prometheus"
)

type ExposeMetricSink string

const (
	InMemorySink    ExposeMetricSink = "in-memory"
	PrometheusSink  ExposeMetricSink = "prometheus"
	AllMetricsSink  ExposeMetricSink = "all"
	ExposeMetricURL                  = "/metrics"
)

var (
	logger = common.InitLogger().WithName("proxy-metrics")

	instance      ProxyMetricsCollector
	collectorOnce sync.Once
)

// labelPool is a simple object pool for label slices to reduce allocations
type labelPool struct {
	pool sync.Pool
}

func newLabelPool() *labelPool {
	return &labelPool{
		pool: sync.Pool{
			New: func() interface{} {
				// Pre-allocate a slice with capacity for common use cases
				slice := make([]gometrics.Label, 0, 3)
				return &slice
			},
		},
	}
}

// get retrieves a label slice from the pool
func (p *labelPool) get() []gometrics.Label {
	slicePtr := p.pool.Get().(*[]gometrics.Label)
	// Reset length but keep capacity
	*slicePtr = (*slicePtr)[:0]
	return *slicePtr
}

// put returns a label slice to the pool
func (p *labelPool) put(labels []gometrics.Label) {
	p.pool.Put(&labels)
}

// ProxyMetricsCollector defines the interface for collecting metrics
type ProxyMetricsCollector interface {
	// RecordCommandLatency  end-to-end latency metrics (client <-> proxy <-> backend)
	RecordCommandLatency(command string, duration time.Duration)

	// RecordCommandForwardingLatency Forwarding latency metrics (proxy <-> backend)
	RecordCommandForwardingLatency(command string, duration time.Duration)

	// RecordOverallLatency records end-to-end latency without distinguishing between commands
	RecordOverallLatency(duration time.Duration)

	// RecordOverallForwardingLatency records forwarding latency without distinguishing between commands
	RecordOverallForwardingLatency(duration time.Duration)

	// IncrementActiveConnections Concurrency metrics
	IncrementActiveConnections()
	DecrementActiveConnections()

	// IncrementCommandCounter Command counter metrics
	IncrementCommandCounter(command string)
	// IncrementCounter Generic counter metrics
	IncrementCounter(label string)

	// IncrementErrorCounter Error metrics
	IncrementErrorCounter(errorType string)

	// Shutdown the metrics collector
	Shutdown()

	// Handler returns a Gin handler function for exposing metrics
	Handler() gin.HandlerFunc
}

// Config holds configuration for metrics
type Config struct {
	// Metrics prefix for namespacing
	ServiceName string

	// Time interval for in-memory metrics aggregation
	AggregationInterval time.Duration

	// Retention period for metrics
	RetentionPeriod time.Duration

	// ExposeSink determines which metrics sink to expose
	ExposeSink ExposeMetricSink

	// MetricsEndpoint is the HTTP path for metrics
	MetricsEndpoint string
}

func AllSinkConfig(serviceName string) *Config {
	config := DefaultConfig()
	config.ServiceName = serviceName
	config.ExposeSink = AllMetricsSink
	return config
}

func NewPrometheusConfig(serviceName string) *Config {
	config := DefaultConfig()
	config.ServiceName = serviceName
	config.ExposeSink = PrometheusSink
	return config
}

func NewInMemoryConfig(serviceName string) *Config {
	config := DefaultConfig()
	config.ServiceName = serviceName
	config.ExposeSink = InMemorySink
	return config
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		AggregationInterval: 5 * time.Second,
		RetentionPeriod:     10 * time.Minute,
		MetricsEndpoint:     ExposeMetricURL,
		ExposeSink:          InMemorySink,
	}
}

func newPrometheusSink() (*prometheus.PrometheusSink, error) {
	// Create a new Prometheus sink
	promSink, err := prometheus.NewPrometheusSink()
	if err != nil {
		return nil, err
	}
	return promSink, nil
}

func newInMemSink(config *Config) *gometrics.InmemSink {
	return gometrics.NewInmemSink(
		config.AggregationInterval,
		config.RetentionPeriod,
	)
}

// NewMetricsCollector creates a new metrics collector based on the provided config
func NewMetricsCollector(config *Config) (ProxyMetricsCollector, error) {
	var initErr error
	collectorOnce.Do(func() {
		if config == nil {
			config = DefaultConfig()
		}
		// Create metrics configuration
		metricsConf := gometrics.DefaultConfig(config.ServiceName)
		// Create a fanout sink that will send metrics to multiple sinks if needed
		sink := &fanoutSink{sinks: make([]gometrics.MetricSink, 0)}
		var inm *gometrics.InmemSink
		var promSink *prometheus.PrometheusSink
		var err error
		// Configure sinks based on the ExposeSink setting
		switch config.ExposeSink {
		case InMemorySink:
			// Create a single in-memory sink instance and use it for both the collector and the fanout sink
			inm = newInMemSink(config)
			sink.sinks = append(sink.sinks, inm)
		case PrometheusSink:
			// Create Prometheus sink with custom buckets
			promSink, err = newPrometheusSink()
			if err != nil {
				initErr = err
				return
			}
			sink.sinks = append(sink.sinks, promSink)
		case AllMetricsSink:
			inm = newInMemSink(config)
			promSink, err = newPrometheusSink()
			if err != nil {
				initErr = err
				return
			}
			sink.sinks = append(sink.sinks, inm, promSink)
		}

		// Create metrics instance with the sink
		metricsImpl, err := gometrics.New(metricsConf, sink)
		if err != nil {
			initErr = err
			return
		}
		instance = &hashicorpMetricsCollector{
			metrics:            metricsImpl,
			inm:                inm,
			promSink:           promSink,
			exposeSink:         config.ExposeSink,
			metricsEndpoint:    config.MetricsEndpoint,
			serviceName:        config.ServiceName,
			serviceLabel:       gometrics.Label{Name: "service", Value: config.ServiceName},
			commandLabelPrefix: "command",
			errorLabelPrefix:   "type",
			labelPool:          newLabelPool(),
		}

		// Log that the metrics collector has been initialized
		logger.Info("Metrics collector initialized",
			"serviceName", config.ServiceName,
			"sink", config.ExposeSink,
			"endpoint", config.MetricsEndpoint)
	})

	return instance, initErr
}

// hashicorpMetricsCollector implements ProxyMetricsCollector using hashicorp/go-metrics
type hashicorpMetricsCollector struct {
	metrics         *gometrics.Metrics
	inm             *gometrics.InmemSink
	promSink        *prometheus.PrometheusSink
	exposeSink      ExposeMetricSink
	metricsEndpoint string
	serviceName     string

	// Pre-created labels for better performance
	serviceLabel       gometrics.Label
	commandLabelPrefix string
	errorLabelPrefix   string

	// Object pool for label slices
	labelPool *labelPool
}

// RecordCommandLatency records the end-to-end latency (client <-> proxy <-> backend)
func (h *hashicorpMetricsCollector) RecordCommandLatency(command string, duration time.Duration) {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel, gometrics.Label{Name: h.commandLabelPrefix, Value: command})

	h.metrics.AddSampleWithLabels([]string{"command", "end_to_end_latency"}, float32(duration.Microseconds()), labels)

	h.labelPool.put(labels)
}

// RecordCommandForwardingLatency records the forwarding latency (proxy <-> backend)
func (h *hashicorpMetricsCollector) RecordCommandForwardingLatency(command string, duration time.Duration) {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel, gometrics.Label{Name: h.commandLabelPrefix, Value: command})

	h.metrics.AddSampleWithLabels([]string{"command", "forwarding_latency"}, float32(duration.Microseconds()), labels)

	h.labelPool.put(labels)
}

// RecordOverallLatency records the end-to-end latency across all commands
func (h *hashicorpMetricsCollector) RecordOverallLatency(duration time.Duration) {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel)

	h.metrics.AddSampleWithLabels([]string{"overall", "end_to_end_latency"}, float32(duration.Microseconds()), labels)

	h.labelPool.put(labels)
}

// RecordOverallForwardingLatency records the forwarding latency across all commands
func (h *hashicorpMetricsCollector) RecordOverallForwardingLatency(duration time.Duration) {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel)

	h.metrics.AddSampleWithLabels([]string{"overall", "forwarding_latency"}, float32(duration.Microseconds()), labels)

	h.labelPool.put(labels)
}

// IncrementActiveConnections increments the active connections counter
func (h *hashicorpMetricsCollector) IncrementActiveConnections() {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel)

	h.metrics.IncrCounterWithLabels([]string{"connections", "active"}, 1, labels)

	h.labelPool.put(labels)
}

// DecrementActiveConnections decrements the active connections counter
func (h *hashicorpMetricsCollector) DecrementActiveConnections() {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel)

	h.metrics.IncrCounterWithLabels([]string{"connections", "active"}, -1, labels)

	h.labelPool.put(labels)
}

// IncrementCommandCounter increments the counter for a specific command
func (h *hashicorpMetricsCollector) IncrementCommandCounter(command string) {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel, gometrics.Label{Name: h.commandLabelPrefix, Value: command})

	h.metrics.IncrCounterWithLabels([]string{"command", "count"}, 1, labels)

	h.labelPool.put(labels)
}

// IncrementCounter increments a counter with a custom label
func (h *hashicorpMetricsCollector) IncrementCounter(label string) {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel)

	h.metrics.IncrCounterWithLabels([]string{label, "count"}, 1, labels)

	h.labelPool.put(labels)
}

// IncrementErrorCounter increments the counter for a specific error type
func (h *hashicorpMetricsCollector) IncrementErrorCounter(errorType string) {
	labels := h.labelPool.get()
	labels = append(labels, h.serviceLabel, gometrics.Label{Name: h.errorLabelPrefix, Value: errorType})

	h.metrics.IncrCounterWithLabels([]string{"errors"}, 1, labels)

	h.labelPool.put(labels)
}

// CollectorHandler returns an HTTP handler for metrics based on the configured sink
func (h *hashicorpMetricsCollector) CollectorHandler() http.Handler {
	logger.Info("Creating metrics handler", "sink", h.exposeSink)
	switch h.exposeSink {
	case PrometheusSink:
		return promHandler()
	case InMemorySink:
		return h.InMemoryHandler()
	case AllMetricsSink:
		return promHandler()
	default:
		return http.NotFoundHandler()
	}
}

// InMemoryHandler returns an HTTP handler for in-memory metrics
func (h *hashicorpMetricsCollector) InMemoryHandler() http.Handler {
	if h.inm == nil {
		logger.Error(nil, "In-memory sink is nil, cannot serve metrics")
		return http.NotFoundHandler()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// logger.Info("Serving in-memory metrics", "path", r.URL.Path)
		// Ensure we have the correct content type
		w.Header().Set("Content-Type", "application/json")

		// Get metrics data from the in-memory sink
		data, err := h.inm.DisplayMetrics(w, r)
		if err != nil {
			logger.Error(err, "Failed to display metrics")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Log the size of metrics data being returned
		// logger.Info("Metrics data returned", "hasData", data != nil)
		// The issue is here - DisplayMetrics doesn't actually write to the ResponseWriter
		// We need to write the data to the response if it's not nil
		if data != nil {
			// DisplayMetrics returns a MetricsSummary struct, so we need to marshal it to JSON
			jsonData, err := json.Marshal(data)
			if err != nil {
				logger.Error(err, "Failed to marshal metrics data to JSON")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Write(jsonData)
		} else {
			// If no data is available, return an empty JSON object instead of empty response
			w.Write([]byte("{}"))
		}
	})
}

// fanoutSink implements a sink that forwards to multiple sinks
type fanoutSink struct {
	sinks []gometrics.MetricSink
}

func (f *fanoutSink) SetGauge(key []string, val float32) {
	for _, s := range f.sinks {
		s.SetGauge(key, val)
	}
}

func (f *fanoutSink) SetGaugeWithLabels(key []string, val float32, labels []gometrics.Label) {
	for _, s := range f.sinks {
		s.SetGaugeWithLabels(key, val, labels)
	}
}

func (f *fanoutSink) EmitKey(key []string, val float32) {
	for _, s := range f.sinks {
		s.EmitKey(key, val)
	}
}

func (f *fanoutSink) IncrCounter(key []string, val float32) {
	for _, s := range f.sinks {
		s.IncrCounter(key, val)
	}
}

func (f *fanoutSink) IncrCounterWithLabels(key []string, val float32, labels []gometrics.Label) {
	for _, s := range f.sinks {
		s.IncrCounterWithLabels(key, val, labels)
	}
}

func (f *fanoutSink) AddSample(key []string, val float32) {
	for _, s := range f.sinks {
		s.AddSample(key, val)
	}
}

func (f *fanoutSink) AddSampleWithLabels(key []string, val float32, labels []gometrics.Label) {
	for _, s := range f.sinks {
		s.AddSampleWithLabels(key, val, labels)
	}
}

// promHandler returns the Prometheus HTTP handler
func promHandler() http.Handler {
	// Create a simple handler that serves Prometheus metrics
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This assumes the Prometheus sink registers with the default registry
		// which is then served by the default handler
		http.DefaultServeMux.ServeHTTP(w, r)
	})
}

// Shutdown stops the metrics collector
func (h *hashicorpMetricsCollector) Shutdown() {
	// Nothing to do for now, but could be used to flush metrics or clean up resources
}

// Handler returns a Gin handler function for exposing metrics
func (h *hashicorpMetricsCollector) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		handler := h.CollectorHandler()
		handler.ServeHTTP(c.Writer, c.Request)
	}
}
