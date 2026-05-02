package governance

import (
	"context"
	"fmt"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

type EvaluationResult struct {
	Decision        domain.PolicyDecision
	ChecksPassed    []string
	ChecksFailed    []string
	ConstraintsUsed *domain.ConstraintsApplied
	Error           *domain.ErrorInfo
	DegradeAction   *domain.DegradeAction
}

type ConstraintEvaluator struct {
	budgetService   *BudgetService
	privacyService  *PrivacyService
	hookChain       interface{}
	constraintStore interface{}
}

func NewConstraintEvaluator(budgetStore interface{}) *ConstraintEvaluator {
	return &ConstraintEvaluator{
		budgetService:  NewBudgetService(nil),
		privacyService: NewPrivacyService(),
		hookChain:      nil,
		constraintStore: budgetStore,
	}
}

func (e *ConstraintEvaluator) Evaluate(ctx context.Context, constraints *domain.Constraints, currentCostUSD float64) *EvaluationResult {
	result := &EvaluationResult{
		Decision:        domain.PolicyDecisionAllow,
		ChecksPassed:    []string{},
		ChecksFailed:    []string{},
		ConstraintsUsed: &domain.ConstraintsApplied{},
	}

	if constraints == nil {
		return result
	}

	if constraints.Budget != nil {
		budgetResult := e.budgetService.CheckBudget(ctx, constraints.Budget.LimitUSD, currentCostUSD)
		if !budgetResult.Allowed {
			if constraints.Budget.Strategy == domain.BudgetStrategyDegradeModel {
				result.Decision = domain.PolicyDecisionDegrade
				result.ChecksPassed = append(result.ChecksPassed, "budget_check_degraded")
				switchModelAction := domain.DegradeActionSwitchModel
				result.DegradeAction = &switchModelAction
				result.ConstraintsUsed.DegradeAction = &switchModelAction
			} else if constraints.Budget.Strategy == domain.BudgetStrategySoftAlert {
				result.Decision = domain.PolicyDecisionAllow
				result.ChecksPassed = append(result.ChecksPassed, "budget_check_soft_alert")
			} else {
				result.Decision = domain.PolicyDecisionBlock
				result.ChecksFailed = append(result.ChecksFailed, "budget_check")
				result.Error = &domain.ErrorInfo{
					Code:    "SENTORIS_BUDGET_EXCEEDED",
					Message: "Budget limit exceeded",
					Details: budgetResult.GetSafeErrorDetails(),
				}
			}
		} else {
			result.ChecksPassed = append(result.ChecksPassed, "budget_check")
		}
		result.ConstraintsUsed.BudgetLimitUSD = constraints.Budget.LimitUSD
		result.ConstraintsUsed.BudgetStrategy = constraints.Budget.Strategy
		result.ConstraintsUsed.BudgetActualUSD = currentCostUSD
	}

	if constraints.Privacy != nil {
		result.ConstraintsUsed.PrivacyLevel = constraints.Privacy.Level
		result.ConstraintsUsed.PrivacyMaskedFields = constraints.Privacy.MaskedFields
		result.ChecksPassed = append(result.ChecksPassed, "privacy_check")
	}

	if constraints.Reproducibility != nil {
		result.ConstraintsUsed.ReproducibilityMode = constraints.Reproducibility.Mode
		result.ConstraintsUsed.ReproducibilitySeed = constraints.Reproducibility.Seed
		result.ChecksPassed = append(result.ChecksPassed, "reproducibility_check")
	}

	return result
}

func (e *ConstraintEvaluator) CheckBudget(ctx context.Context, limitUSD, currentCostUSD float64) *BudgetResult {
	if e.budgetService == nil {
		e.budgetService = NewBudgetService(nil)
	}
	return e.budgetService.CheckBudget(ctx, limitUSD, currentCostUSD)
}

func (e *ConstraintEvaluator) ValidateConstraintCombination(constraints *domain.Constraints) error {
	if constraints == nil {
		return nil
	}

	if constraints.Budget != nil {
		if constraints.Budget.LimitUSD < 0 {
			return fmt.Errorf("budget limit must be non-negative")
		}
		if constraints.Budget.Strategy != domain.BudgetStrategyHardStop &&
			constraints.Budget.Strategy != domain.BudgetStrategyDegradeModel &&
			constraints.Budget.Strategy != domain.BudgetStrategySoftAlert {
			return fmt.Errorf("invalid budget strategy: %s", constraints.Budget.Strategy)
		}
	}

	if constraints.Privacy != nil {
		if constraints.Privacy.Level != domain.PrivacyRaw &&
			constraints.Privacy.Level != domain.PrivacyMasked &&
			constraints.Privacy.Level != domain.PrivacyHashOnly {
			return fmt.Errorf("invalid privacy level: %s", constraints.Privacy.Level)
		}
	}

	if constraints.Privacy != nil && constraints.Reproducibility != nil {
		if constraints.Privacy.Level == domain.PrivacyHashOnly && constraints.Reproducibility.Mode == domain.ReproducibilityStrict {
			return fmt.Errorf("hash_only privacy level conflicts with strict reproducibility")
		}
	}

	if constraints.Reproducibility != nil {
		if constraints.Reproducibility.Mode != domain.ReproducibilityNone &&
			constraints.Reproducibility.Mode != domain.ReproducibilityBounded &&
			constraints.Reproducibility.Mode != domain.ReproducibilityStrict {
			return fmt.Errorf("invalid reproducibility mode: %s", constraints.Reproducibility.Mode)
		}
	}

	return nil
}

func (e *ConstraintEvaluator) EvaluateWithReproducibility(ctx context.Context, constraints *domain.Constraints, currentCostUSD float64) *EvaluationResult {
	result := &EvaluationResult{
		Decision:        domain.PolicyDecisionAllow,
		ChecksPassed:    []string{},
		ChecksFailed:    []string{},
		ConstraintsUsed: &domain.ConstraintsApplied{},
	}

	if constraints == nil {
		return result
	}

	if constraints.Budget != nil {
		budgetResult := e.budgetService.CheckBudget(ctx, constraints.Budget.LimitUSD, currentCostUSD)
		if !budgetResult.Allowed {
			if constraints.Budget.Strategy == domain.BudgetStrategyDegradeModel {
				result.Decision = domain.PolicyDecisionDegrade
				result.ChecksPassed = append(result.ChecksPassed, "budget_check_degraded")
				switchModelAction := domain.DegradeActionSwitchModel
				result.DegradeAction = &switchModelAction
				result.ConstraintsUsed.DegradeAction = &switchModelAction
			} else if constraints.Budget.Strategy == domain.BudgetStrategySoftAlert {
				result.Decision = domain.PolicyDecisionAllow
				result.ChecksPassed = append(result.ChecksPassed, "budget_check_soft_alert")
			} else {
				result.Decision = domain.PolicyDecisionBlock
				result.ChecksFailed = append(result.ChecksFailed, "budget_exceeded")
				result.Error = &domain.ErrorInfo{
					Code:    "SENTORIS_BUDGET_EXCEEDED",
					Message: "Budget limit exceeded",
					Details: budgetResult.GetSafeErrorDetails(),
				}
			}
		} else {
			result.ChecksPassed = append(result.ChecksPassed, "budget_ok")
		}
		result.ConstraintsUsed.BudgetLimitUSD = constraints.Budget.LimitUSD
		result.ConstraintsUsed.BudgetStrategy = constraints.Budget.Strategy
		result.ConstraintsUsed.BudgetActualUSD = currentCostUSD
	}

	if constraints.Privacy != nil {
		result.ConstraintsUsed.PrivacyLevel = constraints.Privacy.Level
		result.ConstraintsUsed.PrivacyMaskedFields = constraints.Privacy.MaskedFields
		result.ChecksPassed = append(result.ChecksPassed, "privacy_constraint_applied")
	}

	if constraints.Reproducibility != nil {
		result.ConstraintsUsed.ReproducibilityMode = constraints.Reproducibility.Mode
		result.ConstraintsUsed.ReproducibilitySeed = constraints.Reproducibility.Seed
		result.ChecksPassed = append(result.ChecksPassed, "reproducibility_constraint_applied")
	}

	return result
}

func (e *ConstraintEvaluator) ValidateConstraintCombinationWithReproducibility(constraints *domain.Constraints) error {
	return e.ValidateConstraintCombination(constraints)
}

func (e *ConstraintEvaluator) ApplyPrivacyMaskingWithStrategy(content any, constraints *domain.ConstraintsApplied, strategy MaskingStrategy) (any, error) {
	if constraints == nil || constraints.PrivacyLevel == domain.PrivacyRaw {
		return content, nil
	}

	result, err := e.privacyService.ApplyMaskingWithStrategy(context.Background(), constraints.PrivacyLevel, strategy, content, constraints.PrivacyMaskedFields)
	if err != nil {
		return content, err
	}

	return result.MaskedContent, nil
}

func (e *ConstraintEvaluator) GetBudgetService() *BudgetService {
	if e.budgetService == nil {
		e.budgetService = NewBudgetService(nil)
	}
	return e.budgetService
}

func (e *ConstraintEvaluator) GetPrivacyService() *PrivacyService {
	if e.privacyService == nil {
		e.privacyService = NewPrivacyService()
	}
	return e.privacyService
}