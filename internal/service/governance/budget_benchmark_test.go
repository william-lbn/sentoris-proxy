package governance

import (
	"context"
	"testing"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
)

// BenchmarkCheckBudget benchmarks budget checking performance
func BenchmarkCheckBudget(b *testing.B) {
	budgetStore := storage.NewMemoryBudgetStore()
	constraintEval := NewConstraintEvaluator(budgetStore)

	ctx := context.Background()
	limitUSD := 10.0
	estimatedCost := 0.0025

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = constraintEval.CheckBudget(ctx, limitUSD, estimatedCost)
	}
}

// BenchmarkEvaluateWithReproducibility benchmarks constraint evaluation
func BenchmarkEvaluateWithReproducibility(b *testing.B) {
	budgetStore := storage.NewMemoryBudgetStore()
	constraintEval := NewConstraintEvaluator(budgetStore)

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = constraintEval.EvaluateWithReproducibility(ctx, nil, 0.0025)
	}
}

// BenchmarkApplyMasking benchmarks privacy masking performance
func BenchmarkApplyMasking(b *testing.B) {
	privacyService := NewPrivacyService()

	ctx := context.Background()
	messages := []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "user", Content: "Hello, my email is test@example.com and my phone is 123-456-7890"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = privacyService.ApplyMaskingWithStrategy(ctx, "masked", "default", messages, nil)
	}
}
