package audit

import (
	"testing"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

func TestSigner_Sign(t *testing.T) {
	signer := NewSigner()

	trace := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Proofs: domain.Proofs{
			ProofType:             "hash_chain",
			CanonicalizationMethod: "rfc8785",
		},
	}

	signature, err := signer.Sign(trace)
	if err != nil {
		t.Errorf("Sign() error = %v", err)
		return
	}

	if signature == "" {
		t.Error("Sign() returned empty signature")
	}

	if len(signature) != 64 {
		t.Errorf("Sign() signature length = %d, want 64", len(signature))
	}
}

func TestSigner_Verify(t *testing.T) {
	signer := NewSigner()

	trace := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Proofs: domain.Proofs{
			ProofType:             "hash_chain",
			CanonicalizationMethod: "rfc8785",
		},
	}

	signature, err := signer.Sign(trace)
	if err != nil {
		t.Errorf("Sign() error = %v", err)
		return
	}

	trace.Proofs.AuditSignature = signature

	valid, err := signer.Verify(trace)
	if err != nil {
		t.Errorf("Verify() error = %v", err)
		return
	}

	if !valid {
		t.Error("Verify() returned false, want true")
	}
}

func TestSigner_VerifyTamperedTrace(t *testing.T) {
	signer := NewSigner()

	trace := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Proofs: domain.Proofs{
			ProofType:             "hash_chain",
			CanonicalizationMethod: "rfc8785",
		},
	}

	signature, _ := signer.Sign(trace)
	trace.Proofs.AuditSignature = signature

	trace.Model = "openai/gpt-4o-mini"

	valid, err := signer.Verify(trace)
	if err != nil {
		t.Errorf("Verify() error = %v", err)
		return
	}

	if valid {
		t.Error("Verify() returned true for tampered trace, want false")
	}
}

func TestSigner_VerifyEmptySignature(t *testing.T) {
	signer := NewSigner()

	trace := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Proofs: domain.Proofs{
			AuditSignature: "",
		},
	}

	_, err := signer.Verify(trace)
	if err == nil {
		t.Error("Verify() expected error for empty signature")
	}
}

func TestSigner_SignatureConsistency(t *testing.T) {
	signer := NewSigner()

	trace1 := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Input: domain.Input{
			Prompt: "test prompt",
		},
		Proofs: domain.Proofs{
			ProofType:             "hash_chain",
			CanonicalizationMethod: "rfc8785",
		},
	}

	trace2 := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Input: domain.Input{
			Prompt: "test prompt",
		},
		Proofs: domain.Proofs{
			ProofType:             "hash_chain",
			CanonicalizationMethod: "rfc8785",
		},
	}

	sig1, _ := signer.Sign(trace1)
	sig2, _ := signer.Sign(trace2)

	if sig1 != sig2 {
		t.Error("Sign() should produce consistent signatures for identical traces")
	}
}

func TestSigner_StripsInternalFields(t *testing.T) {
	signer := NewSigner()

	trace := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Observations: domain.Observations{
			TokensCount: 100,
			InternalMetrics: map[string]any{
				"p99_latency": 50,
			},
		},
		Proofs: domain.Proofs{
			ProofType:             "hash_chain",
			CanonicalizationMethod: "rfc8785",
		},
	}

	sig1, _ := signer.Sign(trace)

	trace.Observations.InternalMetrics["p99_latency"] = 999

	sig2, _ := signer.Sign(trace)

	if sig1 != sig2 {
		t.Error("Signatures should be identical after modifying internal_metrics")
	}
}

func TestSigner_StripsExperimentalExtensionFields(t *testing.T) {
	signer := NewSigner()

	trace := &domain.Trace{
		TraceID:     "f47ac10b-58cc-4372-a567-0e02b2c3d480",
		Model:       "openai/gpt-4o",
		CreatedAt:   time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		ExecutionState: domain.StateFinalized,
		Extensions: map[string]any{
			"sentoris.ai/v1/memory_firewall": map[string]any{
				"enabled": true,
				"_experimental_feature": "test",
				"_internal_cache": map[string]any{
					"key": "value",
				},
			},
			"_private_extension": map[string]any{
				"data": "secret",
			},
		},
	}

	sig1, _ := signer.Sign(trace)

	trace.Extensions["sentoris.ai/v1/memory_firewall"].(map[string]any)["_experimental_feature"] = "modified"
	trace.Extensions["sentoris.ai/v1/memory_firewall"].(map[string]any)["_internal_cache"].(map[string]any)["key"] = "changed"

	sig2, _ := signer.Sign(trace)

	if sig1 != sig2 {
		t.Error("Signatures should be identical after modifying _prefixed extension fields")
	}
}
