package otel

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	TraceCount    prometheus.Counter
	TraceDuration prometheus.Histogram
	TraceErrors   prometheus.Counter

	BudgetReserved  prometheus.Counter
	BudgetUsed      prometheus.Counter
	BudgetRemaining prometheus.Gauge

	UpstreamCalls   prometheus.Counter
	UpstreamErrors  prometheus.Counter
	UpstreamLatency prometheus.Histogram

	SSEEventsSent  prometheus.Counter
	SSEConnections prometheus.Gauge
}

func NewMetrics() *Metrics {
	m := &Metrics{
		TraceCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentoris_trace_count_total",
			Help: "Total number of traces processed",
		}),
		TraceDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "sentoris_trace_duration_seconds",
			Help:    "Duration of trace processing",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		}),
		TraceErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentoris_trace_errors_total",
			Help: "Total number of trace processing errors",
		}),

		BudgetReserved: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentoris_budget_reserved_usd",
			Help: "Total budget reserved in USD",
		}),
		BudgetUsed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentoris_budget_used_usd",
			Help: "Total budget used in USD",
		}),
		BudgetRemaining: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sentoris_budget_remaining_usd",
			Help: "Remaining budget in USD",
		}),

		UpstreamCalls: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentoris_upstream_calls_total",
			Help: "Total number of upstream LLM calls",
		}),
		UpstreamErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentoris_upstream_errors_total",
			Help: "Total number of upstream LLM errors",
		}),
		UpstreamLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "sentoris_upstream_latency_seconds",
			Help:    "Latency of upstream LLM calls",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		}),

		SSEEventsSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentoris_sse_events_sent_total",
			Help: "Total number of SSE events sent",
		}),
		SSEConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sentoris_sse_connections",
			Help: "Current number of SSE connections",
		}),
	}

	prometheus.MustRegister(
		m.TraceCount,
		m.TraceDuration,
		m.TraceErrors,
		m.BudgetReserved,
		m.BudgetUsed,
		m.BudgetRemaining,
		m.UpstreamCalls,
		m.UpstreamErrors,
		m.UpstreamLatency,
		m.SSEEventsSent,
		m.SSEConnections,
	)

	return m
}

func Handler() http.Handler {
	return promhttp.Handler()
}
