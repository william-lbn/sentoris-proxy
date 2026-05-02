package domain

type Constraints struct {
	Reproducibility *ReproducibilityConstraint `json:"reproducibility,omitempty"`
	Budget         *BudgetConstraint         `json:"budget,omitempty"`
	Privacy        *PrivacyConstraint        `json:"privacy,omitempty"`
	Extensions     map[string]any            `json:"extensions,omitempty"`
}

type ReproducibilityConstraint struct {
	Mode ReproducibilityMode `json:"mode,omitempty"`
	Seed *int64              `json:"seed,omitempty"`
}

type BudgetConstraint struct {
	LimitUSD  float64        `json:"limit_usd,omitempty"`
	Strategy BudgetStrategy  `json:"strategy,omitempty"`
}

type PrivacyConstraint struct {
	Level        PrivacyLevel `json:"level,omitempty"`
	Strategy    string       `json:"strategy,omitempty"`
	MaskedFields []string    `json:"masked_fields,omitempty"`
	Deterministic bool       `json:"deterministic,omitempty"`
}

func (c *Constraints) GetPrivacyLevel() PrivacyLevel {
	if c.Privacy != nil && c.Privacy.Level != "" {
		return c.Privacy.Level
	}
	return PrivacyRaw
}

func (c *Constraints) GetBudgetLimit() float64 {
	if c.Budget != nil {
		return c.Budget.LimitUSD
	}
	return 0
}

func (c *Constraints) GetBudgetStrategy() BudgetStrategy {
	if c.Budget != nil && c.Budget.Strategy != "" {
		return c.Budget.Strategy
	}
	return BudgetStrategyHardStop
}

func (c *Constraints) GetReproducibilityMode() ReproducibilityMode {
	if c.Reproducibility != nil && c.Reproducibility.Mode != "" {
		return c.Reproducibility.Mode
	}
	return ReproducibilityNone
}
