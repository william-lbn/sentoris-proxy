package audit

import (
	"testing"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

// BenchmarkSign benchmarks signature generation performance
func BenchmarkSign(b *testing.B) {
	signer := NewSigner()

	trace := &domain.Trace{
		TraceID:        "test-trace-id-12345",
		Model:          "gpt-4o",
		ExecutionState: domain.StateFinalized,
		Input: domain.Input{
			Prompt: 1000,
		},
		Output: domain.Output{
			Response:     strPtr("This is a test response"),
			Truncated:    false,
			FinishReason: domain.FinishReasonStop,
		},
		Observations: domain.Observations{
			TokensCount:      1050,
			CostEstimatedUSD: 0.0025,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = signer.Sign(trace)
	}
}

// BenchmarkEd25519Sign benchmarks Ed25519 signature generation
func BenchmarkEd25519Sign(b *testing.B) {
	privateKey := make([]byte, 32)
	for i := range privateKey {
		privateKey[i] = byte(i)
	}

	edSigner, err := NewEd25519Signer(privateKey)
	if err != nil {
		b.Fatal(err)
	}

	data := map[string]interface{}{
		"trace_id": "test-trace-id",
		"model":    "gpt-4o",
		"state":    "finalized",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = edSigner.Sign(data)
	}
}

func strPtr(s string) *string {
	return &s
}
