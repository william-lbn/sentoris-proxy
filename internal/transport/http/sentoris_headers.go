package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

type SentorisHeaders struct {
	Version            string
	Capabilities       []string
	AcceptedCapabilities []string
	Action            string
	BaselineRef       string
	BudgetLimit       float64
	BudgetStrategy    domain.BudgetStrategy
	PrivacyLevel      domain.PrivacyLevel
	DiffOutput        bool
	FocusFields       []string
	OverrideModel     string
	Seed              *int64
	Repeatability     bool
	RepeatabilityID  string
}

func ParseSentorisHeaders(r *http.Request) *SentorisHeaders {
	h := &SentorisHeaders{
		Version: r.Header.Get("Sentoris-Version"),
		Capabilities: parseCapabilities(r.Header.Get("Sentoris-Capabilities")),
		Action: r.Header.Get("Sentoris-Action"),
		BaselineRef: r.Header.Get("Sentoris-Baseline-Ref"),
		DiffOutput: r.Header.Get("Sentoris-Diff-Output") == "true",
		OverrideModel: r.Header.Get("Sentoris-Override-Model"),
	}

	if h.Version == "" {
		h.Version = "1.0"
	}

	if budgetStr := r.Header.Get("Sentoris-Budget-Limit"); budgetStr != "" {
		if budget, err := strconv.ParseFloat(budgetStr, 64); err == nil {
			h.BudgetLimit = budget
		}
	}

	strategy := r.Header.Get("Sentoris-Budget-Strategy")
	switch strategy {
	case "hard_stop":
		h.BudgetStrategy = domain.BudgetStrategyHardStop
	case "degrade_model":
		h.BudgetStrategy = domain.BudgetStrategyDegradeModel
	case "soft_alert":
		h.BudgetStrategy = domain.BudgetStrategySoftAlert
	default:
		h.BudgetStrategy = domain.BudgetStrategyHardStop
	}

	privacy := r.Header.Get("Sentoris-Privacy-Level")
	switch privacy {
	case "raw":
		h.PrivacyLevel = domain.PrivacyRaw
	case "masked":
		h.PrivacyLevel = domain.PrivacyMasked
	case "hash_only":
		h.PrivacyLevel = domain.PrivacyHashOnly
	default:
		h.PrivacyLevel = domain.PrivacyRaw
	}

	if focusFieldsStr := r.Header.Get("Sentoris-Focus-Fields"); focusFieldsStr != "" {
		h.FocusFields = strings.Split(focusFieldsStr, ",")
		for i, f := range h.FocusFields {
			h.FocusFields[i] = strings.TrimSpace(f)
		}
	}

	if seedStr := r.Header.Get("Sentoris-Seed"); seedStr != "" {
		if seed, err := strconv.ParseInt(seedStr, 10, 64); err == nil {
			h.Seed = &seed
		}
	}

	h.Repeatability = r.Header.Get("Sentoris-Repeatability") == "true"
	h.RepeatabilityID = r.Header.Get("Sentoris-Repeatability-ID")

	return h
}

func parseCapabilities(capStr string) []string {
	if capStr == "" {
		return []string{}
	}
	capabilities := strings.Split(capStr, ",")
	result := make([]string, 0, len(capabilities))
	for _, c := range capabilities {
		c = strings.TrimSpace(c)
		if c != "" {
			result = append(result, c)
		}
	}
	return result
}

func NegotiateCapabilities(requested []string, supported []string) []string {
	accepted := make([]string, 0)
	supportedMap := make(map[string]bool)
	for _, s := range supported {
		supportedMap[s] = true
	}

	for _, req := range requested {
		if supportedMap[req] {
			accepted = append(accepted, req)
		}
	}
	return accepted
}

const (
	SupportedProtocolVersion = "1.0"
)

var supportedVersions = []string{"1.0", "1.0.0"}

func (h *SentorisHeaders) IsVersionSupported() bool {
	for _, v := range supportedVersions {
		if h.Version == v {
			return true
		}
	}
	return false
}

func NegotiateVersion(requested string) (string, bool) {
	for _, v := range supportedVersions {
		if requested == v {
			return v, true
		}
	}
	return SupportedProtocolVersion, false
}

func (h *SentorisHeaders) GetNegotiatedCapabilities(supportedCapabilities []string) []string {
	return NegotiateCapabilities(h.Capabilities, supportedCapabilities)
}

func (h *SentorisHeaders) HasCapability(capability string) bool {
	for _, c := range h.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

func (h *SentorisHeaders) IsReplayAction() bool {
	return h.Action == "replay" || h.Action == "replay-eval" || h.Action == "save-baseline"
}

func (h *SentorisHeaders) IsSaveBaselineAction() bool {
	return h.Action == "save-baseline"
}

func (h *SentorisHeaders) IsDiffRequested() bool {
	return h.DiffOutput || h.Action == "replay-eval"
}

func (h *SentorisHeaders) ToConstraints() *domain.Constraints {
	constraints := &domain.Constraints{}

	if h.BudgetLimit > 0 {
		constraints.Budget = &domain.BudgetConstraint{
			LimitUSD: h.BudgetLimit,
			Strategy: h.BudgetStrategy,
		}
	}

	if h.PrivacyLevel != domain.PrivacyRaw {
		constraints.Privacy = &domain.PrivacyConstraint{
			Level: h.PrivacyLevel,
		}
	}

	if h.Seed != nil || h.Repeatability {
		constraints.Reproducibility = &domain.ReproducibilityConstraint{
			Mode: domain.ReproducibilityBounded,
			Seed: h.Seed,
		}
	}

	return constraints
}
