package middleware

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

type contextKey string

const (
	TraceIDKey              contextKey = "sentoris_trace_id"
	ConstraintsKey          contextKey = "sentoris_constraints"
	SentorisVersionKey      contextKey = "sentoris_version"
	CapabilitiesKey         contextKey = "sentoris_capabilities"
	AcceptedCapabilitiesKey contextKey = "sentoris_accepted_capabilities"
)

// SentorisCapability 表示单个能力声明，符合 RFC 8942 格式
type SentorisCapability struct {
	Namespace string
	Major     int
	Minor     int
	Feature   string
	Raw       string
}

// ParseCapability 解析能力字符串，格式: sentoris.ai/v{major}/{feature}
func ParseCapability(capability string) (*SentorisCapability, error) {
	pattern := `^sentoris\.ai/v([0-9]+)/([a-z0-9_-]+)$`
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(capability)

	if len(matches) != 3 {
		return nil, fmt.Errorf("invalid capability format: %s", capability)
	}

	major, _ := strconv.Atoi(matches[1])

	return &SentorisCapability{
		Namespace: "sentoris.ai",
		Major:     major,
		Minor:     0,
		Feature:   matches[2],
		Raw:       capability,
	}, nil
}

// IsValidCapability 验证能力字符串格式
func IsValidCapability(capability string) bool {
	pattern := `^sentoris\.ai/v[0-9]+/[a-z0-9_-]+$`
	matched, _ := regexp.MatchString(pattern, capability)
	return matched
}

type SentorisHeaders struct {
	Version              string
	Capabilities         []*SentorisCapability
	AcceptedCapabilities []string
	Action               string
	BaselineRef          string
	BudgetLimit          float64
	BudgetStrategy       domain.BudgetStrategy
	PrivacyLevel         domain.PrivacyLevel
	DiffOutput           bool
	FocusFields          string
	OverrideModel        string
	Warning              string
}

func ParseSentorisHeaders(req *http.Request) *SentorisHeaders {
	headers := &SentorisHeaders{}

	headers.Version = getHeader(req, "Sentoris-Version")
	if headers.Version == "" {
		headers.Version = "1.0.0"
	}

	capabilities := getHeader(req, "Sentoris-Capabilities")
	if capabilities != "" {
		capStrings := strings.Split(capabilities, ",")
		for _, capStr := range capStrings {
			capStr = strings.TrimSpace(capStr)
			if cap, err := ParseCapability(capStr); err == nil {
				headers.Capabilities = append(headers.Capabilities, cap)
			}
		}
	}

	headers.Action = getHeader(req, "Sentoris-Action")
	headers.BaselineRef = getHeader(req, "Sentoris-Baseline-Ref")

	if budgetStr := getHeader(req, "Sentoris-Budget-Limit"); budgetStr != "" {
		if budget, err := strconv.ParseFloat(budgetStr, 64); err == nil {
			headers.BudgetLimit = budget
		}
	}

	budgetStrategy := getHeader(req, "Sentoris-Budget-Strategy")
	if budgetStrategy == "" {
		budgetStrategy = "hard_stop"
	}
	headers.BudgetStrategy = domain.BudgetStrategy(budgetStrategy)

	privacyLevel := getHeader(req, "Sentoris-Privacy-Level")
	if privacyLevel == "" {
		privacyLevel = "raw"
	}
	headers.PrivacyLevel = domain.PrivacyLevel(privacyLevel)

	if diffStr := getHeader(req, "Sentoris-Diff-Output"); diffStr != "" {
		headers.DiffOutput = strings.ToLower(diffStr) == "true"
	}

	headers.FocusFields = getHeader(req, "Sentoris-Focus-Fields")
	headers.OverrideModel = getHeader(req, "Sentoris-Override-Model")

	return headers
}

// NegotiateCapabilities 根据客户端请求的能力和服务端支持的能力进行协商
func NegotiateCapabilities(requested []*SentorisCapability, supported []string) ([]string, string) {
	accepted := make([]string, 0)
	var warning string

	supportedMap := make(map[string]bool)
	for _, cap := range supported {
		supportedMap[cap] = true
	}

	for _, reqCap := range requested {
		if supportedMap[reqCap.Raw] {
			accepted = append(accepted, reqCap.Raw)
		} else {
			// 检查是否支持相同feature的较低版本
			found := false
			for supportedCap := range supportedMap {
				if strings.HasSuffix(supportedCap, "/"+reqCap.Feature) {
					accepted = append(accepted, supportedCap)
					warning = fmt.Sprintf("extension downgraded: requested %s, using %s", reqCap.Raw, supportedCap)
					found = true
					break
				}
			}
			if !found {
				warning = fmt.Sprintf("unsupported capability: %s", reqCap.Raw)
			}
		}
	}

	return accepted, warning
}

func (h *SentorisHeaders) ToConstraints() *domain.Constraints {
	constraints := &domain.Constraints{}

	constraints.Budget = &domain.BudgetConstraint{
		LimitUSD: h.BudgetLimit,
		Strategy: h.BudgetStrategy,
	}

	constraints.Privacy = &domain.PrivacyConstraint{
		Level: h.PrivacyLevel,
	}

	if h.Action == "replay" && h.BaselineRef != "" {
		constraints.Reproducibility = &domain.ReproducibilityConstraint{
			Mode: domain.ReproducibilityBounded,
		}
	}

	return constraints
}

func getHeader(r *http.Request, name string) string {
	return r.Header.Get(name)
}

func InjectToContext(ctx context.Context, headers *SentorisHeaders, traceID string) context.Context {
	ctx = context.WithValue(ctx, TraceIDKey, traceID)
	ctx = context.WithValue(ctx, ConstraintsKey, headers.ToConstraints())
	ctx = context.WithValue(ctx, SentorisVersionKey, headers.Version)
	return ctx
}

func GetTraceIDFromContext(ctx context.Context) string {
	if v := ctx.Value(TraceIDKey); v != nil {
		return v.(string)
	}
	return ""
}

func GetConstraintsFromContext(ctx context.Context) *domain.Constraints {
	if v := ctx.Value(ConstraintsKey); v != nil {
		return v.(*domain.Constraints)
	}
	return nil
}

func GetSentorisVersionFromContext(ctx context.Context) string {
	if v := ctx.Value(SentorisVersionKey); v != nil {
		return v.(string)
	}
	return ""
}

type ResponseHeaders struct {
	SentorisVersion         string
	SentorisAccepted        string
	SentorisTraceID         string
	SentorisDiffReport      string
	SentorisCostConsumed    float64
	SentorisBudgetRemaining *float64
	SentorisWarning         string
	SentorisTruncated       bool
}

func (h *ResponseHeaders) Apply(resp *http.Response) {
	if h.SentorisVersion != "" {
		resp.Header.Set("Sentoris-Version", h.SentorisVersion)
	}
	if h.SentorisAccepted != "" {
		resp.Header.Set("Sentoris-Accepted", h.SentorisAccepted)
	}
	if h.SentorisTraceID != "" {
		resp.Header.Set("Sentoris-Trace-Id", h.SentorisTraceID)
	}
	if h.SentorisDiffReport != "" {
		resp.Header.Set("Sentoris-Diff-Report", h.SentorisDiffReport)
	}
	if h.SentorisCostConsumed > 0 {
		resp.Header.Set("Sentoris-Cost-Consumed", strconv.FormatFloat(h.SentorisCostConsumed, 'f', 6, 64))
	}
	if h.SentorisBudgetRemaining != nil {
		resp.Header.Set("Sentoris-Budget-Remaining", strconv.FormatFloat(*h.SentorisBudgetRemaining, 'f', 6, 64))
	}
	if h.SentorisWarning != "" {
		resp.Header.Set("Sentoris-Warning", h.SentorisWarning)
	}
	if h.SentorisTruncated {
		resp.Header.Set("Sentoris-Truncated", "true")
	}
}
