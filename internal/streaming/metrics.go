package streaming

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics tracks ingest endpoint and producer behavior.
type Metrics struct {
	httpResponsesTotal *prometheus.CounterVec
	produceLatency     prometheus.Histogram
	produceErrorsTotal prometheus.Counter
	producerQueueDepth prometheus.Gauge
}

// NewMetrics registers and returns streaming metrics collectors.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	m := &Metrics{
		httpResponsesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "orca",
			Subsystem: "stream",
			Name:      "http_requests_total",
			Help:      "HTTP responses emitted by the stream ingest endpoint by status code.",
		}, []string{"code"}),
		produceLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "orca",
			Subsystem: "stream",
			Name:      "produce_duration_seconds",
			Help:      "Time between async produce enqueue and broker callback.",
			Buckets:   prometheus.DefBuckets,
		}),
		produceErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "orca",
			Subsystem: "stream",
			Name:      "produce_errors_total",
			Help:      "Total async produce callback failures.",
		}),
		producerQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "orca",
			Subsystem: "stream",
			Name:      "producer_queue_depth",
			Help:      "Approximate number of in-flight async produce records.",
		}),
	}

	reg.MustRegister(m.httpResponsesTotal, m.produceLatency, m.produceErrorsTotal, m.producerQueueDepth)
	return m
}

// ObserveHTTPResponse tracks ingest endpoint status codes.
func (m *Metrics) ObserveHTTPResponse(code int) {
	if m == nil {
		return
	}
	m.httpResponsesTotal.WithLabelValues(prometheusLabelCode(code)).Inc()
}

// Enqueue increments queue depth when records are accepted by the client.
func (m *Metrics) Enqueue() {
	if m == nil {
		return
	}
	m.producerQueueDepth.Inc()
}

// ObserveProduceResult records callback latency and errors.
func (m *Metrics) ObserveProduceResult(duration time.Duration, err error) {
	if m == nil {
		return
	}
	m.producerQueueDepth.Dec()
	m.produceLatency.Observe(duration.Seconds())
	if err != nil {
		m.produceErrorsTotal.Inc()
	}
}

func prometheusLabelCode(code int) string {
	if code < 100 {
		return "unknown"
	}
	return strconv.Itoa(code)
}
