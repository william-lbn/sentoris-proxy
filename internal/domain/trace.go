package domain

import (
	"encoding/json"
	"time"
)

type ExecutionState string

const (
	StateInit            ExecutionState = "INIT"
	StateConstraintEval  ExecutionState = "CONSTRAINT_EVAL"
	StateExecuting       ExecutionState = "EXECUTING"
	StateValidation      ExecutionState = "VALIDATION"
	StateFinalized       ExecutionState = "FINALIZED"
	StateFailed          ExecutionState = "FAILED"
)

type PrivacyLevel string

const (
	PrivacyRaw       PrivacyLevel = "raw"
	PrivacyMasked    PrivacyLevel = "masked"
	PrivacyHashOnly  PrivacyLevel = "hash_only"
)

type ReproducibilityMode string

const (
	ReproducibilityNone    ReproducibilityMode = "none"
	ReproducibilityBounded ReproducibilityMode = "bounded"
	ReproducibilityStrict  ReproducibilityMode = "strict"
)

type BudgetStrategy string

const (
	BudgetStrategyHardStop    BudgetStrategy = "hard_stop"
	BudgetStrategyDegradeModel BudgetStrategy = "degrade_model"
	BudgetStrategySoftAlert   BudgetStrategy = "soft_alert"
)

type PolicyDecision string

const (
	PolicyDecisionAllow  PolicyDecision = "allow"
	PolicyDecisionBlock  PolicyDecision = "block"
	PolicyDecisionDegrade PolicyDecision = "degrade"
)

type FinishReason string

const (
	FinishReasonStop           FinishReason = "stop"
	FinishReasonLength         FinishReason = "length"
	FinishReasonBudgetExceeded FinishReason = "budget_exceeded"
	FinishReasonError          FinishReason = "error"
	FinishReasonOther          FinishReason = "other"
)

type DegradeAction string

const (
	DegradeActionSwitchModel         DegradeAction = "SWITCH_MODEL"
	DegradeActionReduceMaxTokens     DegradeAction = "REDUCE_MAX_TOKENS"
	DegradeActionSkipOptionalPlugin  DegradeAction = "SKIP_OPTIONAL_PLUGIN"
	DegradeActionReduceSamplingQuality DegradeAction = "REDUCE_SAMPLING_QUALITY"
	DegradeActionFallbackCached      DegradeAction = "FALLBACK_CACHED"
)

type ChangeType string

const (
	ChangeTypeValueChange      ChangeType = "VALUE_CHANGE"
	ChangeTypeStructureChange ChangeType = "STRUCTURE_CHANGE"
	ChangeTypeFactualError    ChangeType = "FACTUAL_ERROR"
	ChangeTypeHallucination   ChangeType = "HALLUCINATION"
	ChangeTypeOmission        ChangeType = "OMISSION"
	ChangeTypeAdded           ChangeType = "ADDED"
	ChangeTypeRemoved         ChangeType = "REMOVED"
)

type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "LOW"
	RiskLevelMedium   RiskLevel = "MEDIUM"
	RiskLevelHigh     RiskLevel = "HIGH"
	RiskLevelCritical RiskLevel = "CRITICAL"
)

type Recommendation string

const (
	RecommendationApprove      Recommendation = "APPROVE"
	RecommendationReviewRequired Recommendation = "REVIEW_REQUIRED"
	RecommendationBlockRelease Recommendation = "BLOCK_RELEASE"
)

type Trace struct {
	TraceID            string            `json:"trace_id"`
	ParentID           *string           `json:"parent_id,omitempty"`
	SessionID         *string           `json:"session_id,omitempty"`
	ExecutionState    ExecutionState    `json:"execution_state"`
	Model              string            `json:"model"`
	Input              Input             `json:"input"`
	Output             Output            `json:"output"`
	Observations       Observations      `json:"observations"`
	Proofs             Proofs            `json:"proofs"`
	ConstraintsApplied ConstraintsApplied `json:"constraints_applied"`
	CreatedAt          time.Time         `json:"created_at"`
	TTLExpireAt       *time.Time        `json:"ttl_expire_at,omitempty"`
	Extensions        map[string]any    `json:"extensions,omitempty"`
}

type Input struct {
	Prompt   any            `json:"prompt,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Seed     *int64         `json:"seed,omitempty"`
}

type Output struct {
	Response    *string      `json:"response,omitempty"`
	Truncated   bool         `json:"truncated"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
}

type Observations struct {
	TokensCount      int              `json:"tokens_count,omitempty"`
	CostEstimatedUSD float64          `json:"cost_estimated_usd,omitempty"`
	LatencyMs        int              `json:"latency_ms,omitempty"`
	StateTransitions []StateTransition `json:"state_transitions,omitempty"`
	Error            *ErrorInfo       `json:"error,omitempty"`
	DegradeAction    *DegradeAction   `json:"degrade_action,omitempty"`
	InternalMetrics  map[string]any   `json:"internal_metrics,omitempty"`
}

type StateTransition struct {
	From      ExecutionState `json:"from"`
	To        ExecutionState `json:"to"`
	Timestamp time.Time      `json:"timestamp"`
}

type ErrorInfo struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Proofs struct {
	AuditSignature         string `json:"audit_signature"`
	ProofType             string `json:"proof_type,omitempty"`
	CanonicalizationMethod string `json:"canonicalization_method,omitempty"`
}

type ConstraintsApplied struct {
	BudgetLimitUSD      float64        `json:"budget_limit_usd,omitempty"`
	BudgetActualUSD     float64        `json:"budget_actual_usd,omitempty"`
	BudgetStrategy      BudgetStrategy `json:"budget_strategy,omitempty"`
	PrivacyLevel        PrivacyLevel   `json:"privacy_level,omitempty"`
	PrivacyMaskedFields []string       `json:"privacy_masked_fields,omitempty"`
	ReproducibilityMode ReproducibilityMode `json:"reproducibility_mode,omitempty"`
	ReproducibilitySeed *int64         `json:"reproducibility_seed,omitempty"`
	PolicyEvaluation    *PolicyEvaluation `json:"policy_evaluation,omitempty"`
	DegradeAction       *DegradeAction `json:"degrade_action,omitempty"`
	NegotiatedCapabilities []string   `json:"negotiated_capabilities,omitempty"`
}

type PolicyEvaluation struct {
	Decision   PolicyDecision `json:"decision,omitempty"`
	ChecksPassed []string    `json:"checks_passed,omitempty"`
	NegotiatedCapabilities []string `json:"negotiated_capabilities,omitempty"`
}

func (t *Trace) IsValid() bool {
	return t.TraceID != "" && t.Model != "" && !t.CreatedAt.IsZero()
}

func (t *Trace) FillDefaults() {
	if t.Proofs.ProofType == "" {
		t.Proofs.ProofType = "hash_chain"
	}
	if t.Proofs.CanonicalizationMethod == "" {
		t.Proofs.CanonicalizationMethod = "rfc8785"
	}
	if t.Output.FinishReason == "" {
		t.Output.FinishReason = FinishReasonStop
	}
}

func (t *Trace) ValidateSchema() error {
	if t.TraceID == "" {
		return &ValidationError{Field: "trace_id", Message: "trace_id is required"}
	}
	if t.Model == "" {
		return &ValidationError{Field: "model", Message: "model is required"}
	}
	if t.CreatedAt.IsZero() {
		return &ValidationError{Field: "created_at", Message: "created_at is required"}
	}
	return nil
}

func (t *Trace) AddStateTransition(from, to ExecutionState) {
	t.Observations.StateTransitions = append(t.Observations.StateTransitions, StateTransition{
		From:      from,
		To:        to,
		Timestamp: time.Now().UTC(),
	})
	t.ExecutionState = to
}

func (t *Trace) IsFinalState() bool {
	return t.ExecutionState == StateFinalized || t.ExecutionState == StateFailed
}

func (t *Trace) CanTransitionTo(to ExecutionState) bool {
	switch t.ExecutionState {
	case StateInit:
		return to == StateConstraintEval
	case StateConstraintEval:
		return to == StateExecuting || to == StateFailed
	case StateExecuting:
		return to == StateValidation || to == StateFailed
	case StateValidation:
		return to == StateFinalized || to == StateFailed
	case StateFinalized, StateFailed:
		return false
	default:
		return false
	}
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func (t *Trace) ToJSON() ([]byte, error) {
	return json.Marshal(t)
}

func TraceFromJSON(data []byte) (*Trace, error) {
	var trace Trace
	if err := json.Unmarshal(data, &trace); err != nil {
		return nil, err
	}
	return &trace, nil
}
