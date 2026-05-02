package domain

import (
	"testing"
)

func TestNewRiskReport(t *testing.T) {
	report := NewRiskReport("baseline-1", "candidate-1")

	if report.BaselineTraceID != "baseline-1" {
		t.Errorf("Expected baseline trace ID to be 'baseline-1', got %s", report.BaselineTraceID)
	}

	if report.CandidateTraceID != "candidate-1" {
		t.Errorf("Expected candidate trace ID to be 'candidate-1', got %s", report.CandidateTraceID)
	}

	if report.GeneratedAt == "" {
		t.Error("Expected generated at to be set")
	}

	if report.Risk != nil {
		t.Error("Expected risk to be nil initially")
	}

	if report.TokenDiff != nil {
		t.Error("Expected token diff to be nil initially")
	}
}

func TestRiskReport_SetTokenSimilarity(t *testing.T) {
	report := NewRiskReport("baseline-1", "candidate-1")
	report.SetTokenSimilarity(0.85, 100, 120)

	if report.TokenDiff == nil {
		t.Error("Expected token diff to be set")
	}

	if report.TokenDiff.SimilarityRatio != 0.85 {
		t.Errorf("Expected similarity ratio to be 0.85, got %f", report.TokenDiff.SimilarityRatio)
	}

	if report.TokenDiff.BaselineLength != 100 {
		t.Errorf("Expected baseline length to be 100, got %d", report.TokenDiff.BaselineLength)
	}

	if report.TokenDiff.CandidateLength != 120 {
		t.Errorf("Expected candidate length to be 120, got %d", report.TokenDiff.CandidateLength)
	}

	if report.TokenDiff.Methodology != "normalized_levenshtein_v2" {
		t.Errorf("Expected methodology to be 'normalized_levenshtein_v2', got %s", report.TokenDiff.Methodology)
	}
}

func TestRiskReport_SetTokenSimilarityWithEditDistance(t *testing.T) {
	report := NewRiskReport("baseline-1", "candidate-1")
	report.SetTokenSimilarityWithEditDistance(0.85, 15, 100, 120)

	if report.TokenDiff == nil {
		t.Error("Expected token diff to be set")
	}

	if report.TokenDiff.SimilarityRatio != 0.85 {
		t.Errorf("Expected similarity ratio to be 0.85, got %f", report.TokenDiff.SimilarityRatio)
	}

	if report.TokenDiff.EditDistance != 15 {
		t.Errorf("Expected edit distance to be 15, got %d", report.TokenDiff.EditDistance)
	}

	if report.TokenDiff.BaselineLength != 100 {
		t.Errorf("Expected baseline length to be 100, got %d", report.TokenDiff.BaselineLength)
	}

	if report.TokenDiff.CandidateLength != 120 {
		t.Errorf("Expected candidate length to be 120, got %d", report.TokenDiff.CandidateLength)
	}
}

func TestRiskReport_SetRisk(t *testing.T) {
	report := NewRiskReport("baseline-1", "candidate-1")
	report.SetRisk(0.3, RecommendationApprove)

	if report.Risk == nil {
		t.Error("Expected risk to be set")
	}

	if report.Risk.Score != 0.3 {
		t.Errorf("Expected score to be 0.3, got %f", report.Risk.Score)
	}

	if report.Risk.Recommendation != RecommendationApprove {
		t.Errorf("Expected recommendation to be RecommendationApprove, got %v", report.Risk.Recommendation)
	}

	if report.Risk.Methodology != "embedding_cosine_v3" {
		t.Errorf("Expected methodology to be 'embedding_cosine_v3', got %s", report.Risk.Methodology)
	}
}

func TestRiskReport_SetRiskWithReasons(t *testing.T) {
	report := NewRiskReport("baseline-1", "candidate-1")
	reasons := []string{"Low token difference", "Similar output structure"}
	report.SetRiskWithReasons(0.3, RecommendationApprove, reasons)

	if report.Risk == nil {
		t.Error("Expected risk to be set")
	}

	if report.Risk.Score != 0.3 {
		t.Errorf("Expected score to be 0.3, got %f", report.Risk.Score)
	}

	if report.Risk.Recommendation != RecommendationApprove {
		t.Errorf("Expected recommendation to be RecommendationApprove, got %v", report.Risk.Recommendation)
	}

	if len(report.Risk.RecommendationReasons) != 2 {
		t.Errorf("Expected 2 recommendation reasons, got %d", len(report.Risk.RecommendationReasons))
	}

	if report.Risk.RecommendationReasons[0] != "Low token difference" {
		t.Errorf("Expected first reason to be 'Low token difference', got %s", report.Risk.RecommendationReasons[0])
	}

	if report.Risk.RecommendationReasons[1] != "Similar output structure" {
		t.Errorf("Expected second reason to be 'Similar output structure', got %s", report.Risk.RecommendationReasons[1])
	}
}

func TestRiskReport_AddFieldRisk(t *testing.T) {
	report := NewRiskReport("baseline-1", "candidate-1")
	confidence := 0.95
	report.AddFieldRisk("output.response", "old content", "new content", RiskLevelLow, &confidence, ChangeTypeValueChange)

	if report.Risk == nil {
		t.Error("Expected risk to be set")
	}

	if len(report.Risk.Fields) != 1 {
		t.Errorf("Expected 1 field risk, got %d", len(report.Risk.Fields))
	}

	fieldRisk := report.Risk.Fields[0]
	if fieldRisk.Path != "output.response" {
		t.Errorf("Expected path to be 'output.response', got %s", fieldRisk.Path)
	}

	if fieldRisk.OldValue != "old content" {
		t.Errorf("Expected old value to be 'old content', got %v", fieldRisk.OldValue)
	}

	if fieldRisk.NewValue != "new content" {
		t.Errorf("Expected new value to be 'new content', got %v", fieldRisk.NewValue)
	}

	if fieldRisk.RiskLevel != RiskLevelLow {
		t.Errorf("Expected risk level to be RiskLevelLow, got %v", fieldRisk.RiskLevel)
	}

	if *fieldRisk.Confidence != 0.95 {
		t.Errorf("Expected confidence to be 0.95, got %f", *fieldRisk.Confidence)
	}

	if fieldRisk.ChangeType != ChangeTypeValueChange {
		t.Errorf("Expected change type to be ChangeTypeValueChange, got %v", fieldRisk.ChangeType)
	}
}
