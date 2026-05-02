package diff

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

type Differ struct{}

func NewDiffer() *Differ {
	return &Differ{}
}

type DiffResult struct {
	TokenSimilarity float64
	EditDistance    int
	RiskScore       float64
	Recommendation  domain.Recommendation
	RiskLevel       domain.RiskLevel
	Changes         []FieldChange
}

type FieldChange struct {
	Path       string
	OldValue   any
	NewValue   any
	RiskLevel  domain.RiskLevel
	ChangeType domain.ChangeType
}

func (d *Differ) Compare(baseline, candidate *domain.Trace) (*domain.RiskReport, error) {
	report := domain.NewRiskReport(baseline.TraceID, candidate.TraceID)

	report.ModelChanged = baseline.Model != candidate.Model

	if baseline.Output.Response == nil && candidate.Output.Response == nil {
		report.SetTokenSimilarityWithEditDistance(1.0, 0, 0, 0)
		report.SetRiskWithReasons(0, domain.RecommendationApprove, []string{"Both responses are empty"})
		return report, nil
	}

	var baselineText, candidateText string
	if baseline.Output.Response != nil {
		baselineText = *baseline.Output.Response
	}
	if candidate.Output.Response != nil {
		candidateText = *candidate.Output.Response
	}

	editDist := LevenshteinDistance(baselineText, candidateText)
	maxLen := int(math.Max(float64(len(baselineText)), float64(len(candidateText))))
	similarity := 1.0
	if maxLen > 0 {
		similarity = 1.0 - (float64(editDist) / float64(maxLen))
	}

	report.SetTokenSimilarityWithEditDistance(similarity, editDist, len(baselineText), len(candidateText))

	// 分析字段级差异
	d.analyzeFieldDifferences(report, baseline, candidate)

	riskScore := d.calculateRiskScore(similarity, baseline.Model != candidate.Model)
	recommendation, reasons := d.determineRecommendationWithReasons(riskScore, baseline.Model != candidate.Model, report)
	report.SetRiskWithReasons(riskScore, recommendation, reasons)

	// 确保risk.methodology与实际使用的算法一致
	if report.Risk != nil {
		report.Risk.Methodology = "hybrid_edit_distance_model_change_v2"
		report.Risk.ModelChanged = &report.ModelChanged
	}

	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	return report, nil
}

func (d *Differ) analyzeFieldDifferences(report *domain.RiskReport, baseline, candidate *domain.Trace) {
	// 分析模型变更
	if baseline.Model != candidate.Model {
		confidence := 1.0
		report.AddFieldRisk("model", baseline.Model, candidate.Model, domain.RiskLevelMedium, &confidence, domain.ChangeTypeValueChange)
	}

	// 分析输入参数差异
	if baseline.Input.Params != nil || candidate.Input.Params != nil {
		baselineParams := baseline.Input.Params
		if baselineParams == nil {
			baselineParams = make(map[string]any)
		}
		candidateParams := candidate.Input.Params
		if candidateParams == nil {
			candidateParams = make(map[string]any)
		}

		// 检查所有在baseline或candidate中出现的参数
		allKeys := make(map[string]bool)
		for key := range baselineParams {
			allKeys[key] = true
		}
		for key := range candidateParams {
			allKeys[key] = true
		}

		for key := range allKeys {
			oldValue := baselineParams[key]
			newValue := candidateParams[key]
			if !d.valuesEqual(oldValue, newValue) {
				changeType := domain.ChangeTypeValueChange
				if _, exists := baselineParams[key]; !exists {
					changeType = domain.ChangeTypeAdded
				} else if _, exists := candidateParams[key]; !exists {
					changeType = domain.ChangeTypeRemoved
				}
				riskLevel := d.assessParamRisk(key, oldValue, newValue)
				confidence := 0.95
				report.AddFieldRisk("input.params."+key, oldValue, newValue, riskLevel, &confidence, changeType)
			}
		}
	}

	// 分析输出响应差异
	if (baseline.Output.Response != nil && candidate.Output.Response != nil && !d.valuesEqual(*baseline.Output.Response, *candidate.Output.Response)) ||
		(baseline.Output.Response != nil && candidate.Output.Response == nil) ||
		(baseline.Output.Response == nil && candidate.Output.Response != nil) {
		oldValue := ""
		if baseline.Output.Response != nil {
			oldValue = *baseline.Output.Response
		}
		newValue := ""
		if candidate.Output.Response != nil {
			newValue = *candidate.Output.Response
		}

		changeType := domain.ChangeTypeValueChange
		if baseline.Output.Response == nil {
			changeType = domain.ChangeTypeAdded
		} else if candidate.Output.Response == nil {
			changeType = domain.ChangeTypeRemoved
		}

		riskLevel := domain.RiskLevelLow
		if len(oldValue) > 0 && len(newValue) > 0 {
			editDist := LevenshteinDistance(oldValue, newValue)
			maxLen := int(math.Max(float64(len(oldValue)), float64(len(newValue))))
			similarity := 1.0
			if maxLen > 0 {
				similarity = 1.0 - (float64(editDist) / float64(maxLen))
			}
			if similarity < 0.3 {
				riskLevel = domain.RiskLevelHigh
			} else if similarity < 0.7 {
				riskLevel = domain.RiskLevelMedium
			}
		} else if (baseline.Output.Response == nil && candidate.Output.Response != nil) ||
			(baseline.Output.Response != nil && candidate.Output.Response == nil) {
			riskLevel = domain.RiskLevelHigh
		}

		confidence := 0.9
		report.AddFieldRisk("output.response", oldValue, newValue, riskLevel, &confidence, changeType)
	}

	// 分析 tokens 差异
	if baseline.Observations.TokensCount != candidate.Observations.TokensCount {
		confidence := 0.98
		tokenDiff := candidate.Observations.TokensCount - baseline.Observations.TokensCount
		riskLevel := domain.RiskLevelLow
		if math.Abs(float64(tokenDiff)) > float64(baseline.Observations.TokensCount)*0.5 {
			riskLevel = domain.RiskLevelMedium
		}
		report.AddFieldRisk("observations.tokens_count", baseline.Observations.TokensCount, candidate.Observations.TokensCount, riskLevel, &confidence, domain.ChangeTypeValueChange)
	}

	// 分析成本差异
	if baseline.Observations.CostEstimatedUSD != candidate.Observations.CostEstimatedUSD {
		confidence := 0.98
		costDiff := candidate.Observations.CostEstimatedUSD - baseline.Observations.CostEstimatedUSD
		riskLevel := domain.RiskLevelLow
		if costDiff > 0 && baseline.Observations.CostEstimatedUSD > 0 {
			increasePercent := costDiff / baseline.Observations.CostEstimatedUSD
			if increasePercent > 0.5 {
				riskLevel = domain.RiskLevelMedium
			}
			if increasePercent > 1.0 {
				riskLevel = domain.RiskLevelHigh
			}
		}
		report.AddFieldRisk("observations.cost_estimated_usd", baseline.Observations.CostEstimatedUSD, candidate.Observations.CostEstimatedUSD, riskLevel, &confidence, domain.ChangeTypeValueChange)
	}

	// 分析 finish reason 差异
	if string(baseline.Output.FinishReason) != string(candidate.Output.FinishReason) {
		confidence := 0.95
		riskLevel := domain.RiskLevelLow
		if string(baseline.Output.FinishReason) == "stop" && string(candidate.Output.FinishReason) == "length" {
			riskLevel = domain.RiskLevelHigh
		} else if string(candidate.Output.FinishReason) == "error" {
			riskLevel = domain.RiskLevelHigh
		}
		report.AddFieldRisk("output.finish_reason", string(baseline.Output.FinishReason), string(candidate.Output.FinishReason), riskLevel, &confidence, domain.ChangeTypeValueChange)
	}
}

// valuesEqual checks if two values are equal
func (d *Differ) valuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Try deep equality check
	if reflect.DeepEqual(a, b) {
		return true
	}

	// Try JSON serialization for complex objects
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	if aErr == nil && bErr == nil {
		return string(aJSON) == string(bJSON)
	}

	return false
}

func (d *Differ) assessParamRisk(key string, oldValue, newValue any) domain.RiskLevel {
	// 关键参数的风险评估
	criticalParams := map[string]bool{
		"temperature":   true,
		"top_p":         true,
		"max_tokens":    true,
		"stop":          true,
		"system_prompt": true,
	}

	if criticalParams[key] {
		return domain.RiskLevelMedium
	}

	return domain.RiskLevelLow
}

func (d *Differ) calculateRiskScore(similarity float64, modelChanged bool) float64 {
	baseRisk := 1.0 - similarity

	if modelChanged {
		baseRisk = baseRisk*0.8 + 0.2
	}

	if baseRisk < 0 {
		baseRisk = 0
	}
	if baseRisk > 1 {
		baseRisk = 1
	}

	return math.Round(baseRisk*100) / 100
}

func (d *Differ) determineRecommendation(riskScore float64, modelChanged bool, report *domain.RiskReport) domain.Recommendation {
	recommendation, _ := d.determineRecommendationWithReasons(riskScore, modelChanged, report)
	return recommendation
}

func (d *Differ) determineRecommendationWithReasons(riskScore float64, modelChanged bool, report *domain.RiskReport) (domain.Recommendation, []string) {
	var reasons []string
	highRiskCount := 0
	mediumRiskCount := 0

	// Count risk levels from fields
	if report.Risk != nil {
		for _, field := range report.Risk.Fields {
			switch field.RiskLevel {
			case domain.RiskLevelHigh:
				highRiskCount++
			case domain.RiskLevelMedium:
				mediumRiskCount++
			}
		}
	}

	// Check for critical conditions first
	if highRiskCount >= 2 {
		reasons = append(reasons, fmt.Sprintf("Multiple high-risk fields detected: %d", highRiskCount))
		return domain.RecommendationBlockRelease, reasons
	}

	if highRiskCount >= 1 {
		reasons = append(reasons, fmt.Sprintf("High-risk field detected: %d", highRiskCount))
		return domain.RecommendationReviewRequired, reasons
	}

	if riskScore >= 0.7 {
		reasons = append(reasons, "Risk score is critical (>= 0.7)")
		return domain.RecommendationBlockRelease, reasons
	}

	if riskScore >= 0.3 {
		reasons = append(reasons, "Risk score requires review (>= 0.3)")
		return domain.RecommendationReviewRequired, reasons
	}

	if mediumRiskCount >= 2 {
		reasons = append(reasons, fmt.Sprintf("Multiple medium-risk fields detected: %d", mediumRiskCount))
		return domain.RecommendationReviewRequired, reasons
	}

	if modelChanged && riskScore >= 0.2 {
		reasons = append(reasons, "Model changed and risk score >= 0.2")
		return domain.RecommendationReviewRequired, reasons
	}

	reasons = append(reasons, "Risk score is acceptable")
	return domain.RecommendationApprove, reasons
}

func LevenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	r1 := []rune(s1)
	r2 := []rune(s2)

	m := len(r1)
	n := len(r2)

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 0; i <= m; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if r1[i-1] == r2[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				minVal := dp[i-1][j]
				if dp[i][j-1] < minVal {
					minVal = dp[i][j-1]
				}
				if dp[i-1][j-1] < minVal {
					minVal = dp[i-1][j-1]
				}
				dp[i][j] = minVal + 1
			}
		}
	}

	return dp[m][n]
}
