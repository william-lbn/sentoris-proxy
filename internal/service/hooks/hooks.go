package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

// Hook 定义了 Sentoris 代理的钩子接口
type Hook interface {
	// Name 返回钩子的名称
	Name() string

	// Priority 返回钩子的优先级（数值越小优先级越高）
	Priority() int

	// PreExecute 在执行 LLM 请求前调用
	PreExecute(ctx context.Context, trace *domain.Trace) error

	// PreStream 在流式响应开始前调用
	PreStream(ctx context.Context, trace *domain.Trace) error

	// PostStream 在流式响应结束后调用
	PostStream(ctx context.Context, trace *domain.Trace) error

	// OnFailure 在执行失败时调用
	OnFailure(ctx context.Context, trace *domain.Trace) error
}

// HookError 表示钩子执行错误
type HookError struct {
	Message   string
	IsFatal   bool
	IsDegrade bool
}

func (e *HookError) Error() string {
	return e.Message
}

// HookContext 包含钩子执行的上下文信息
type HookContext struct {
	TraceID string
	Config  interface{}
	Logger  interface{}
}

// HookRegistry 管理钩子的注册和发现
type HookRegistry struct {
	hooks map[string]Hook
}

// NewHookRegistry 创建一个新的钩子注册表
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[string]Hook),
	}
}

// Register 注册一个钩子
func (r *HookRegistry) Register(hook Hook) {
	r.hooks[hook.Name()] = hook
}

// Get 获取指定名称的钩子
func (r *HookRegistry) Get(name string) Hook {
	return r.hooks[name]
}

// List 获取所有钩子
func (r *HookRegistry) List() []Hook {
	hooks := make([]Hook, 0, len(r.hooks))
	for _, hook := range r.hooks {
		hooks = append(hooks, hook)
	}
	return hooks
}

// HookChain 管理多个钩子的执行
type HookChain struct {
	hooks    []Hook
	strategy string // "short-circuit" 或 "all-execute"
}

// NewHookChain 创建一个新的钩子链
func NewHookChain(strategy string) *HookChain {
	if strategy == "" {
		strategy = "short-circuit"
	}
	return &HookChain{
		hooks:    []Hook{},
		strategy: strategy,
	}
}

// AddHook 添加一个钩子到链中
func (c *HookChain) AddHook(hook Hook) {
	c.hooks = append(c.hooks, hook)
}

// PreExecute 执行所有钩子的 PreExecute 方法
func (c *HookChain) PreExecute(ctx context.Context, trace *domain.Trace) error {
	return c.executeHooks(ctx, trace, func(hook Hook) error {
		return hook.PreExecute(ctx, trace)
	})
}

// PreStream 执行所有钩子的 PreStream 方法
func (c *HookChain) PreStream(ctx context.Context, trace *domain.Trace) error {
	return c.executeHooks(ctx, trace, func(hook Hook) error {
		return hook.PreStream(ctx, trace)
	})
}

// PostStream 执行所有钩子的 PostStream 方法
func (c *HookChain) PostStream(ctx context.Context, trace *domain.Trace) error {
	return c.executeHooks(ctx, trace, func(hook Hook) error {
		return hook.PostStream(ctx, trace)
	})
}

// OnFailure 执行所有钩子的 OnFailure 方法
func (c *HookChain) OnFailure(ctx context.Context, trace *domain.Trace) error {
	// OnFailure 总是执行所有钩子
	for _, hook := range c.hooks {
		_ = hook.OnFailure(ctx, trace)
	}
	return nil
}

// executeHooks 执行钩子方法
func (c *HookChain) executeHooks(ctx context.Context, trace *domain.Trace, fn func(Hook) error) error {
	for _, hook := range c.hooks {
		err := fn(hook)
		if err != nil {
			if c.strategy == "short-circuit" {
				return err
			}
			// 否则记录错误但继续执行
		}
	}
	return nil
}

// NoopHook 是一个空实现的钩子，用于默认情况
type NoopHook struct{}

// Name 返回钩子名称
func (h *NoopHook) Name() string {
	return "noop"
}

// Priority 返回钩子优先级
func (h *NoopHook) Priority() int {
	return 1000
}

// PreExecute 空实现
func (h *NoopHook) PreExecute(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// PreStream 空实现
func (h *NoopHook) PreStream(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// PostStream 空实现
func (h *NoopHook) PostStream(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// OnFailure 空实现
func (h *NoopHook) OnFailure(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// NewNoopHook 创建一个空实现的钩子
func NewNoopHook() *NoopHook {
	return &NoopHook{}
}

// PII patterns for detection
var (
	emailPattern      = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	phonePattern      = regexp.MustCompile(`(\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}`)
	ssnPattern        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	creditCardPattern = regexp.MustCompile(`\b(?:\d[ -]*?){13,16}\b`)
)

// PIIDetectorHook 检测并处理个人身份信息
type PIIDetectorHook struct {
	detectedTypes []string
}

// Name 返回钩子名称
func (h *PIIDetectorHook) Name() string {
	return "pii-detector"
}

// Priority 返回钩子优先级
func (h *PIIDetectorHook) Priority() int {
	return 100
}

// PreExecute 检测并处理个人身份信息
func (h *PIIDetectorHook) PreExecute(ctx context.Context, trace *domain.Trace) error {
	h.detectedTypes = []string{}

	// Check prompt for PII
	if trace.Input.Prompt != nil {
		promptStr := fmt.Sprintf("%v", trace.Input.Prompt)
		h.detectPIIInString(promptStr)
	}

	// Check prompt with JSON serialization for complex objects
	if trace.Input.Prompt != nil {
		promptBytes, err := json.Marshal(trace.Input.Prompt)
		if err == nil {
			h.detectPIIInString(string(promptBytes))
		}
	}

	// Record PII detection in trace metadata
	if len(h.detectedTypes) > 0 {
		if trace.Input.Metadata == nil {
			trace.Input.Metadata = map[string]any{}
		}
		trace.Input.Metadata["pii_detected"] = true
		trace.Input.Metadata["pii_types"] = h.detectedTypes
	}

	return nil
}

// detectPIIInString detects PII in a string
func (h *PIIDetectorHook) detectPIIInString(s string) {
	if emailPattern.MatchString(s) && !contains(h.detectedTypes, "email") {
		h.detectedTypes = append(h.detectedTypes, "email")
	}
	if phonePattern.MatchString(s) && !contains(h.detectedTypes, "phone") {
		h.detectedTypes = append(h.detectedTypes, "phone")
	}
	if ssnPattern.MatchString(s) && !contains(h.detectedTypes, "ssn") {
		h.detectedTypes = append(h.detectedTypes, "ssn")
	}
	if creditCardPattern.MatchString(s) && !contains(h.detectedTypes, "credit_card") {
		h.detectedTypes = append(h.detectedTypes, "credit_card")
	}
}

// PreStream 空实现
func (h *PIIDetectorHook) PreStream(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// PostStream 检测输出中的PII
func (h *PIIDetectorHook) PostStream(ctx context.Context, trace *domain.Trace) error {
	if trace.Output.Response != nil {
		responseStr := fmt.Sprintf("%v", trace.Output.Response)
		outputDetected := []string{}

		if emailPattern.MatchString(responseStr) {
			outputDetected = append(outputDetected, "email")
		}
		if phonePattern.MatchString(responseStr) {
			outputDetected = append(outputDetected, "phone")
		}

		if len(outputDetected) > 0 {
			if trace.Input.Metadata == nil {
				trace.Input.Metadata = map[string]any{}
			}
			trace.Input.Metadata["output_pii_detected"] = true
			trace.Input.Metadata["output_pii_types"] = outputDetected
		}
	}
	return nil
}

// OnFailure 空实现
func (h *PIIDetectorHook) OnFailure(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// NewPIIDetectorHook 创建一个PII检测钩子
func NewPIIDetectorHook() *PIIDetectorHook {
	return &PIIDetectorHook{}
}

// RateLimiterHook 实现速率限制
type RateLimiterHook struct {
	requests map[string]*sessionRequestCounter
	mu       sync.Mutex
	// Default limits
	maxRequestsPerMinute int
}

type sessionRequestCounter struct {
	count     int
	startTime time.Time
}

// Name 返回钩子名称
func (h *RateLimiterHook) Name() string {
	return "rate-limiter"
}

// Priority 返回钩子优先级
func (h *RateLimiterHook) Priority() int {
	return 50
}

// PreExecute 实现速率限制
func (h *RateLimiterHook) PreExecute(ctx context.Context, trace *domain.Trace) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.requests == nil {
		h.requests = make(map[string]*sessionRequestCounter)
	}

	if h.maxRequestsPerMinute == 0 {
		h.maxRequestsPerMinute = 100 // Default: 100 requests per minute
	}

	var sessionID string
	if trace.SessionID != nil {
		sessionID = *trace.SessionID
	} else {
		sessionID = "default"
	}

	counter, exists := h.requests[sessionID]
	now := time.Now()

	if !exists || now.Sub(counter.startTime) > time.Minute {
		// New session or reset window
		h.requests[sessionID] = &sessionRequestCounter{
			count:     1,
			startTime: now,
		}
		return nil
	}

	// Increment counter
	counter.count++

	// Check if limit exceeded
	if counter.count > h.maxRequestsPerMinute {
		return &HookError{
			Message: fmt.Sprintf("Rate limit exceeded: %d requests per minute", h.maxRequestsPerMinute),
			IsFatal: true,
		}
	}

	// Record rate limit info in metadata
	if trace.Input.Metadata == nil {
		trace.Input.Metadata = map[string]any{}
	}
	trace.Input.Metadata["rate_limit_count"] = counter.count
	trace.Input.Metadata["rate_limit_max"] = h.maxRequestsPerMinute

	return nil
}

// PreStream 空实现
func (h *RateLimiterHook) PreStream(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// PostStream 空实现
func (h *RateLimiterHook) PostStream(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// OnFailure 空实现
func (h *RateLimiterHook) OnFailure(ctx context.Context, trace *domain.Trace) error {
	return nil
}

// NewRateLimiterHook 创建一个速率限制钩子
func NewRateLimiterHook() *RateLimiterHook {
	return &RateLimiterHook{
		maxRequestsPerMinute: 100,
	}
}

// NewRateLimiterHookWithLimit 创建一个带自定义限制的速率限制钩子
func NewRateLimiterHookWithLimit(maxRequestsPerMinute int) *RateLimiterHook {
	return &RateLimiterHook{
		maxRequestsPerMinute: maxRequestsPerMinute,
	}
}

// Helper function to check if a string is in a slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
