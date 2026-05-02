package governance

import (
	"context"
	"testing"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

func int64Ptr(i int64) *int64 {
	return &i
}

func TestConstraintEvaluator_Evaluate_BudgetOk(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.Constraints{
		Budget: &domain.BudgetConstraint{
			LimitUSD: 10.0,
			Strategy: domain.BudgetStrategyHardStop,
		},
	}

	result := eval.Evaluate(context.Background(), constraints, 5.0)

	if result.Decision != domain.PolicyDecisionAllow {
		t.Errorf("Expected PolicyDecisionAllow, got %v", result.Decision)
	}

	if len(result.ChecksPassed) != 1 || result.ChecksPassed[0] != "budget_check" {
		t.Errorf("Expected checks passed to contain 'budget_check', got %v", result.ChecksPassed)
	}

	if len(result.ChecksFailed) != 0 {
		t.Errorf("Expected no checks failed, got %v", result.ChecksFailed)
	}
}

func TestConstraintEvaluator_Evaluate_BudgetExceeded(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.Constraints{
		Budget: &domain.BudgetConstraint{
			LimitUSD: 5.0,
			Strategy: domain.BudgetStrategyHardStop,
		},
	}

	result := eval.Evaluate(context.Background(), constraints, 10.0)

	if result.Decision != domain.PolicyDecisionBlock {
		t.Errorf("Expected PolicyDecisionBlock, got %v", result.Decision)
	}

	if len(result.ChecksFailed) != 1 || result.ChecksFailed[0] != "budget_check" {
		t.Errorf("Expected checks failed to contain 'budget_check', got %v", result.ChecksFailed)
	}

	if result.Error == nil {
		t.Error("Expected error for budget exceeded")
	}
}

func TestConstraintEvaluator_Evaluate_PrivacyConstraint(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.Constraints{
		Privacy: &domain.PrivacyConstraint{
			Level: domain.PrivacyMasked,
			MaskedFields: []string{"email", "phone"},
		},
	}

	result := eval.Evaluate(context.Background(), constraints, 0)

	if result.Decision != domain.PolicyDecisionAllow {
		t.Errorf("Expected PolicyDecisionAllow, got %v", result.Decision)
	}

	if len(result.ChecksPassed) != 1 || result.ChecksPassed[0] != "privacy_check" {
		t.Errorf("Expected checks passed to contain 'privacy_check', got %v", result.ChecksPassed)
	}

	if result.ConstraintsUsed.PrivacyLevel != domain.PrivacyMasked {
		t.Errorf("Expected privacy level to be PrivacyMasked, got %v", result.ConstraintsUsed.PrivacyLevel)
	}
}

func TestConstraintEvaluator_Evaluate_ReproducibilityConstraint(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.Constraints{
		Reproducibility: &domain.ReproducibilityConstraint{
			Mode: domain.ReproducibilityBounded,
			Seed: int64Ptr(12345),
		},
	}

	result := eval.Evaluate(context.Background(), constraints, 0)

	if result.Decision != domain.PolicyDecisionAllow {
		t.Errorf("Expected PolicyDecisionAllow, got %v", result.Decision)
	}

	if len(result.ChecksPassed) != 1 || result.ChecksPassed[0] != "reproducibility_check" {
		t.Errorf("Expected checks passed to contain 'reproducibility_check', got %v", result.ChecksPassed)
	}

	if result.ConstraintsUsed.ReproducibilityMode != domain.ReproducibilityBounded {
		t.Errorf("Expected reproducibility mode to be Bounded, got %v", result.ConstraintsUsed.ReproducibilityMode)
	}

	if result.ConstraintsUsed.ReproducibilitySeed == nil || *result.ConstraintsUsed.ReproducibilitySeed != 12345 {
		t.Errorf("Expected reproducibility seed to be 12345, got %v", result.ConstraintsUsed.ReproducibilitySeed)
	}
}

func TestConstraintEvaluator_ValidateConstraintCombination_Valid(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.Constraints{
		Budget: &domain.BudgetConstraint{
			LimitUSD: 10.0,
			Strategy: domain.BudgetStrategyHardStop,
		},
		Privacy: &domain.PrivacyConstraint{
			Level: domain.PrivacyMasked,
		},
		Reproducibility: &domain.ReproducibilityConstraint{
			Mode: domain.ReproducibilityStrict,
		},
	}

	err := eval.ValidateConstraintCombination(constraints)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestConstraintEvaluator_ValidateConstraintCombination_Conflict(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.Constraints{
		Privacy: &domain.PrivacyConstraint{
			Level: domain.PrivacyHashOnly,
		},
		Reproducibility: &domain.ReproducibilityConstraint{
			Mode: domain.ReproducibilityStrict,
		},
	}

	err := eval.ValidateConstraintCombination(constraints)

	if err == nil {
		t.Error("Expected error for conflicting constraints")
	}
}

func TestConstraintEvaluator_ApplyPrivacyMasking_Raw(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.ConstraintsApplied{
		PrivacyLevel: domain.PrivacyRaw,
	}

	content := "test content"
	result, err := eval.ApplyPrivacyMaskingWithStrategy(content, constraints, MaskingStrategyRegex)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result != content {
		t.Errorf("Expected content to remain unchanged, got %v", result)
	}
}

func TestConstraintEvaluator_ApplyPrivacyMasking_Masked(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.ConstraintsApplied{
		PrivacyLevel: domain.PrivacyMasked,
		PrivacyMaskedFields: []string{"email"},
	}

	content := "test@example.com"
	result, err := eval.ApplyPrivacyMaskingWithStrategy(content, constraints, MaskingStrategyRegex)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result != "[EMAIL_REDACTED]" {
		t.Errorf("Expected content to be masked, got %v", result)
	}
}

func TestConstraintEvaluator_ApplyPrivacyMasking_HashOnly(t *testing.T) {
	eval := NewConstraintEvaluator(nil)
	constraints := &domain.ConstraintsApplied{
		PrivacyLevel: domain.PrivacyHashOnly,
	}

	content := "test content"
	result, err := eval.ApplyPrivacyMaskingWithStrategy(content, constraints, MaskingStrategyRegex)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Should return a SHA-256 hash (64 hex characters)
	hash, ok := result.(string)
	if !ok {
		t.Error("Expected string result")
	}

	if len(hash) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hash))
	}
}
