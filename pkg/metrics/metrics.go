package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector 收集Sentoris专属指标
type MetricsCollector struct {
	budgetCutoffTotal       prometheus.Counter
	diffRiskLevel           *prometheus.CounterVec
	constraintEvalDuration  prometheus.Histogram
	stateTransitionDuration *prometheus.HistogramVec
	traceCount              *prometheus.CounterVec
	traceDuration           prometheus.Histogram
	providerRequestTotal    *prometheus.CounterVec
	providerRequestDuration *prometheus.HistogramVec
	providerErrorTotal      *prometheus.CounterVec
	degradeActionTotal      *prometheus.CounterVec
	privacyLevelCount       *prometheus.CounterVec
}

var (
	instance *MetricsCollector
	once     sync.Once
)

// GetMetricsCollector 获取指标收集器单例
func GetMetricsCollector() *MetricsCollector {
	once.Do(func() {
		instance = &MetricsCollector{
			budgetCutoffTotal: promauto.NewCounter(prometheus.CounterOpts{
				Name: "sentoris_budget_cutoff_total",
				Help: "Total number of budget cutoffs",
			}),
			diffRiskLevel: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "sentoris_diff_risk_level_total",
				Help: "Total number of risk assessments by risk level",
			}, []string{"level"}),
			constraintEvalDuration: promauto.NewHistogram(prometheus.HistogramOpts{
				Name:    "sentoris_constraint_eval_duration_seconds",
				Help:    "Duration of constraint evaluation",
				Buckets: prometheus.DefBuckets,
			}),
			stateTransitionDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "sentoris_state_transition_duration_seconds",
				Help:    "Duration of state transitions",
				Buckets: prometheus.DefBuckets,
			}, []string{"from_state", "to_state"}),
			traceCount: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "sentoris_trace_count_total",
				Help: "Total number of traces by state",
			}, []string{"state"}),
			traceDuration: promauto.NewHistogram(prometheus.HistogramOpts{
				Name:    "sentoris_trace_duration_seconds",
				Help:    "Duration of trace execution",
				Buckets: prometheus.DefBuckets,
			}),
			providerRequestTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "sentoris_provider_request_total",
				Help: "Total number of provider requests",
			}, []string{"provider", "model"}),
			providerRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "sentoris_provider_request_duration_seconds",
				Help:    "Duration of provider requests",
				Buckets: prometheus.DefBuckets,
			}, []string{"provider", "model"}),
			providerErrorTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "sentoris_provider_error_total",
				Help: "Total number of provider errors",
			}, []string{"provider", "model", "error_type"}),
			degradeActionTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "sentoris_degrade_action_total",
				Help: "Total number of degrade actions",
			}, []string{"action_type"}),
			privacyLevelCount: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "sentoris_privacy_level_count_total",
				Help: "Total number of traces by privacy level",
			}, []string{"level"}),
		}
	})
	return instance
}

// RecordBudgetCutoff 记录预算截断
func (m *MetricsCollector) RecordBudgetCutoff() {
	m.budgetCutoffTotal.Inc()
}

// RecordDiffRiskLevel 记录风险级别
func (m *MetricsCollector) RecordDiffRiskLevel(level string) {
	m.diffRiskLevel.WithLabelValues(level).Inc()
}

// RecordConstraintEvalDuration 记录约束评估持续时间
func (m *MetricsCollector) RecordConstraintEvalDuration(duration time.Duration) {
	m.constraintEvalDuration.Observe(duration.Seconds())
}

// RecordStateTransitionDuration 记录状态转换持续时间
func (m *MetricsCollector) RecordStateTransitionDuration(fromState, toState string, duration time.Duration) {
	m.stateTransitionDuration.WithLabelValues(fromState, toState).Observe(duration.Seconds())
}

// RecordTraceCount 记录Trace数量
func (m *MetricsCollector) RecordTraceCount(state string) {
	m.traceCount.WithLabelValues(state).Inc()
}

// RecordTraceDuration 记录Trace执行持续时间
func (m *MetricsCollector) RecordTraceDuration(duration time.Duration) {
	m.traceDuration.Observe(duration.Seconds())
}

// RecordProviderRequest 记录提供商请求
func (m *MetricsCollector) RecordProviderRequest(provider, model string, duration time.Duration) {
	m.providerRequestTotal.WithLabelValues(provider, model).Inc()
	m.providerRequestDuration.WithLabelValues(provider, model).Observe(duration.Seconds())
}

// RecordProviderError 记录提供商错误
func (m *MetricsCollector) RecordProviderError(provider, model, errorType string) {
	m.providerErrorTotal.WithLabelValues(provider, model, errorType).Inc()
}

// RecordDegradeAction 记录降级动作
func (m *MetricsCollector) RecordDegradeAction(actionType string) {
	m.degradeActionTotal.WithLabelValues(actionType).Inc()
}

// RecordPrivacyLevel 记录隐私级别
func (m *MetricsCollector) RecordPrivacyLevel(level string) {
	m.privacyLevelCount.WithLabelValues(level).Inc()
}

// GetRegistry 获取Prometheus注册表
func (m *MetricsCollector) GetRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		m.budgetCutoffTotal,
		m.diffRiskLevel,
		m.constraintEvalDuration,
		m.stateTransitionDuration,
		m.traceCount,
		m.traceDuration,
		m.providerRequestTotal,
		m.providerRequestDuration,
		m.providerErrorTotal,
		m.degradeActionTotal,
		m.privacyLevelCount,
	)
	return registry
}

// 便捷函数，使用全局实例

// RecordBudgetCutoff 记录预算截断
func RecordBudgetCutoff() {
	GetMetricsCollector().RecordBudgetCutoff()
}

// RecordDiffRiskLevel 记录风险级别
func RecordDiffRiskLevel(level string) {
	GetMetricsCollector().RecordDiffRiskLevel(level)
}

// RecordConstraintEvalDuration 记录约束评估持续时间
func RecordConstraintEvalDuration(duration time.Duration) {
	GetMetricsCollector().RecordConstraintEvalDuration(duration)
}

// RecordStateTransitionDuration 记录状态转换持续时间
func RecordStateTransitionDuration(fromState, toState string, duration time.Duration) {
	GetMetricsCollector().RecordStateTransitionDuration(fromState, toState, duration)
}

// RecordTraceCount 记录Trace数量
func RecordTraceCount(state string) {
	GetMetricsCollector().RecordTraceCount(state)
}

// RecordTraceDuration 记录Trace执行持续时间
func RecordTraceDuration(duration time.Duration) {
	GetMetricsCollector().RecordTraceDuration(duration)
}

// RecordProviderRequest 记录提供商请求
func RecordProviderRequest(provider, model string, duration time.Duration) {
	GetMetricsCollector().RecordProviderRequest(provider, model, duration)
}

// RecordProviderError 记录提供商错误
func RecordProviderError(provider, model, errorType string) {
	GetMetricsCollector().RecordProviderError(provider, model, errorType)
}

// RecordDegradeAction 记录降级动作
func RecordDegradeAction(actionType string) {
	GetMetricsCollector().RecordDegradeAction(actionType)
}

// RecordPrivacyLevel 记录隐私级别
func RecordPrivacyLevel(level string) {
	GetMetricsCollector().RecordPrivacyLevel(level)
}

// GetRegistry 获取Prometheus注册表
func GetRegistry() *prometheus.Registry {
	return GetMetricsCollector().GetRegistry()
}
