package audit

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
	"github.com/sentoris-ai/sentoris-proxy/pkg/jcs"
)

type Signer struct{}

func NewSigner() *Signer {
	return &Signer{}
}

func (s *Signer) Sign(trace *domain.Trace) (string, error) {
	if trace == nil {
		return "", fmt.Errorf("trace cannot be nil")
	}

	traceCopy := *trace
	traceCopy.Proofs.AuditSignature = ""

	traceMap := traceToMap(&traceCopy)

	stripped := jcs.StripNonSignatureFields(traceMap)

	canonical, err := jcs.Canonicalize(stripped)
	if err != nil {
		return "", fmt.Errorf("canonicalization failed: %w", err)
	}

	signature := jcs.ComputeSHA256Hex(canonical)

	return signature, nil
}

func (s *Signer) Verify(trace *domain.Trace) (bool, error) {
	if trace == nil {
		return false, fmt.Errorf("trace cannot be nil")
	}

	if trace.Proofs.AuditSignature == "" {
		return false, fmt.Errorf("audit_signature is empty")
	}

	computed, err := s.Sign(trace)
	if err != nil {
		return false, err
	}

	return computed == trace.Proofs.AuditSignature, nil
}

func traceToMap(t *domain.Trace) map[string]any {
	m := make(map[string]any)

	m["trace_id"] = t.TraceID
	if t.ParentID != nil {
		m["parent_id"] = *t.ParentID
	}
	if t.SessionID != nil {
		m["session_id"] = *t.SessionID
	}
	m["execution_state"] = string(t.ExecutionState)
	m["model"] = t.Model
	m["input"] = inputToMap(&t.Input)
	m["output"] = outputToMap(&t.Output)
	m["observations"] = observationsToMap(&t.Observations)
	m["proofs"] = proofsToMap(&t.Proofs)
	m["constraints_applied"] = constraintsAppliedToMap(&t.ConstraintsApplied)
	m["created_at"] = t.CreatedAt.Format(time.RFC3339Nano)
	if t.TTLExpireAt != nil {
		m["ttl_expire_at"] = t.TTLExpireAt.Format(time.RFC3339Nano)
	}
	if t.Extensions != nil {
		ext := make(map[string]any)
		for k, v := range t.Extensions {
			if !isInternalField(k) {
				ext[k] = v
			}
		}
		if len(ext) > 0 {
			m["extensions"] = ext
		}
	}

	return m
}

func inputToMap(in *domain.Input) map[string]any {
	m := make(map[string]any)
	if in.Prompt != nil {
		m["prompt"] = in.Prompt
	}
	if in.Params != nil {
		m["params"] = in.Params
	}
	if in.Metadata != nil {
		m["metadata"] = in.Metadata
	}
	return m
}

func outputToMap(out *domain.Output) map[string]any {
	m := make(map[string]any)
	if out.Response != nil {
		if out.Truncated {
			m["response"] = jcs.SanitizeResponse(*out.Response)
		} else {
			m["response"] = *out.Response
		}
	}
	m["truncated"] = out.Truncated
	if out.FinishReason != "" {
		m["finish_reason"] = string(out.FinishReason)
	}
	return m
}

func observationsToMap(obs *domain.Observations) map[string]any {
	m := make(map[string]any)
	if obs.TokensCount > 0 {
		m["tokens_count"] = obs.TokensCount
	}
	if obs.CostEstimatedUSD > 0 {
		m["cost_estimated_usd"] = obs.CostEstimatedUSD
	}
	if obs.LatencyMs > 0 {
		m["latency_ms"] = obs.LatencyMs
	}
	if len(obs.StateTransitions) > 0 {
		transitions := make([]map[string]any, len(obs.StateTransitions))
		for i, st := range obs.StateTransitions {
			transitions[i] = map[string]any{
				"from":      string(st.From),
				"to":        string(st.To),
				"timestamp": st.Timestamp.Format(time.RFC3339Nano),
			}
		}
		m["state_transitions"] = transitions
	}
	if obs.Error != nil {
		errMap := map[string]any{
			"code":    obs.Error.Code,
			"message": obs.Error.Message,
		}
		if obs.Error.Details != nil {
			errMap["details"] = obs.Error.Details
		}
		m["error"] = errMap
	}
	return m
}

func proofsToMap(p *domain.Proofs) map[string]any {
	m := make(map[string]any)
	m["audit_signature"] = p.AuditSignature
	if p.ProofType != "" {
		m["proof_type"] = p.ProofType
	}
	if p.CanonicalizationMethod != "" {
		m["canonicalization_method"] = p.CanonicalizationMethod
	}
	return m
}

func constraintsAppliedToMap(ca *domain.ConstraintsApplied) map[string]any {
	m := make(map[string]any)
	if ca.BudgetLimitUSD > 0 {
		m["budget_limit_usd"] = ca.BudgetLimitUSD
	}
	if ca.BudgetActualUSD > 0 {
		m["budget_actual_usd"] = ca.BudgetActualUSD
	}
	if ca.BudgetStrategy != "" {
		m["budget_strategy"] = string(ca.BudgetStrategy)
	}
	if ca.PrivacyLevel != "" {
		m["privacy_level"] = string(ca.PrivacyLevel)
	}
	if len(ca.PrivacyMaskedFields) > 0 {
		m["privacy_masked_fields"] = ca.PrivacyMaskedFields
	}
	if ca.ReproducibilityMode != "" {
		m["reproducibility_mode"] = string(ca.ReproducibilityMode)
	}
	if ca.PolicyEvaluation != nil {
		pe := make(map[string]any)
		if ca.PolicyEvaluation.Decision != "" {
			pe["decision"] = string(ca.PolicyEvaluation.Decision)
		}
		if len(ca.PolicyEvaluation.ChecksPassed) > 0 {
			pe["checks_passed"] = ca.PolicyEvaluation.ChecksPassed
		}
		if len(ca.PolicyEvaluation.NegotiatedCapabilities) > 0 {
			pe["negotiated_capabilities"] = ca.PolicyEvaluation.NegotiatedCapabilities
		}
		m["policy_evaluation"] = pe
	}
	return m
}

func isInternalField(key string) bool {
	return len(key) > 0 && key[0] == '_'
}

type Verifier struct {
	signer *Signer
}

func NewVerifier() *Verifier {
	return &Verifier{
		signer: NewSigner(),
	}
}

func (v *Verifier) Verify(trace *domain.Trace) (bool, error) {
	return v.signer.Verify(trace)
}

func (v *Verifier) VerifyFromJSON(data []byte) (bool, error) {
	var trace domain.Trace
	if err := json.Unmarshal(data, &trace); err != nil {
		return false, fmt.Errorf("failed to unmarshal trace: %w", err)
	}
	return v.Verify(&trace)
}

type Canonicalizer struct{}

func NewCanonicalizer() *Canonicalizer {
	return &Canonicalizer{}
}

func (c *Canonicalizer) Canonicalize(v any) ([]byte, error) {
	return jcs.Canonicalize(v)
}

func (c *Canonicalizer) CanonicalizeToString(v any) (string, error) {
	return jcs.CanonicalizeToString(v)
}
