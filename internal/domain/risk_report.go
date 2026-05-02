package domain

type RiskReport struct {
	BaselineTraceID  string          `json:"baseline_trace_id"`
	CandidateTraceID string          `json:"candidate_trace_id"`
	ModelChanged     bool            `json:"model_changed"`
	FocusFields      []string        `json:"focus_fields,omitempty"`
	TokenDiff        *TokenDiff      `json:"token_diff,omitempty"`
	Risk             *RiskAssessment `json:"risk"`
	GeneratedAt      string          `json:"generated_at"`
}

type TokenDiff struct {
	SimilarityRatio float64 `json:"similarity_ratio"`
	EditDistance    int     `json:"edit_distance"`
	BaselineLength  int     `json:"baseline_length"`
	CandidateLength int     `json:"candidate_length"`
	Methodology     string  `json:"methodology"`
}

type RiskAssessment struct {
	Score                 float64        `json:"score"`
	Methodology           string         `json:"methodology"`
	Confidence            *float64       `json:"confidence,omitempty"`
	EmbeddingModel        *string        `json:"embedding_model,omitempty"`
	Fields                []FieldRisk    `json:"fields,omitempty"`
	Recommendation        Recommendation `json:"recommendation"`
	RecommendationReasons []string       `json:"recommendation_reasons,omitempty"`
	ModelChanged          *bool          `json:"model_changed,omitempty"`
}

type FieldRisk struct {
	Path       string     `json:"path"`
	OldValue   any        `json:"old_value"`
	NewValue   any        `json:"new_value"`
	RiskLevel  RiskLevel  `json:"risk_level"`
	Confidence *float64   `json:"confidence,omitempty"`
	ChangeType ChangeType `json:"change_type,omitempty"`
}

func NewRiskReport(baselineID, candidateID string) *RiskReport {
	return &RiskReport{
		BaselineTraceID:  baselineID,
		CandidateTraceID: candidateID,
		GeneratedAt:      "2026-01-01T00:00:00Z",
	}
}

func (r *RiskReport) SetTokenSimilarity(ratio float64, baselineLen, candidateLen int) {
	r.TokenDiff = &TokenDiff{
		SimilarityRatio: ratio,
		BaselineLength:  baselineLen,
		CandidateLength: candidateLen,
		Methodology:     "normalized_levenshtein_v2",
	}
}

func (r *RiskReport) SetTokenSimilarityWithEditDistance(ratio float64, editDistance, baselineLen, candidateLen int) {
	r.TokenDiff = &TokenDiff{
		SimilarityRatio: ratio,
		EditDistance:    editDistance,
		BaselineLength:  baselineLen,
		CandidateLength: candidateLen,
		Methodology:     "normalized_levenshtein_v2",
	}
}

func (r *RiskReport) SetRisk(score float64, recommendation Recommendation) {
	r.Risk = &RiskAssessment{
		Score:          score,
		Recommendation: recommendation,
		Methodology:    "embedding_cosine_v3",
	}
}

func (r *RiskReport) SetRiskWithReasons(score float64, recommendation Recommendation, reasons []string) {
	r.Risk = &RiskAssessment{
		Score:                 score,
		Recommendation:        recommendation,
		RecommendationReasons: reasons,
		Methodology:           "embedding_cosine_v3",
	}
}

func (r *RiskReport) AddFieldRisk(path string, oldValue, newValue any, riskLevel RiskLevel, confidence *float64, changeType ChangeType) {
	if r.Risk == nil {
		r.Risk = &RiskAssessment{}
	}
	r.Risk.Fields = append(r.Risk.Fields, FieldRisk{
		Path:       path,
		OldValue:   oldValue,
		NewValue:   newValue,
		RiskLevel:  riskLevel,
		Confidence: confidence,
		ChangeType: changeType,
	})
}
