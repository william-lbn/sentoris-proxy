package jcs

import (
	"bytes"
	"encoding/json"
	"testing"
)

// BenchmarkJCSCanonicalize benchmarks JCS canonicalization performance
func BenchmarkJCSCanonicalize(b *testing.B) {
	// Generate a realistic Trace-like JSON structure
	traceJSON := []byte(`{
		"trace_id": "abc123def456",
		"model": "gpt-4o",
		"execution_state": "finalized",
		"input": {
			"prompt": 1000,
			"messages": [
				{"role": "user", "content": "Hello, this is a test message that is somewhat longer to test performance"}
			]
		},
		"output": {
			"response": "This is a sample response from the LLM model",
			"truncated": false,
			"finish_reason": "stop"
		},
		"observations": {
			"tokens_count": 1050,
			"cost_estimated_usd": 0.0025,
			"latency_ms": 150
		},
		"constraints_applied": {
			"privacy_level": "raw",
			"budget_limit_usd": 10.0
		},
		"proofs": {
			"audit_signature": "sha256-abc123...",
			"proof_type": "hash_chain"
		},
		"state_transitions": [
			{"from": "init", "to": "constraint_eval", "timestamp": "2024-01-01T00:00:00Z"},
			{"from": "constraint_eval", "to": "executing", "timestamp": "2024-01-01T00:00:01Z"},
			{"from": "executing", "timestamp": "2024-01-01T00:00:02Z", "to": "finalized"}
		],
		"extensions": {},
		"created_at": "2024-01-01T00:00:00Z",
		"updated_at": "2024-01-01T00:00:02Z"
	}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var data interface{}
		if err := json.Unmarshal(traceJSON, &data); err != nil {
			b.Fatal(err)
		}
		_, _ = Canonicalize(data)
	}
}

// BenchmarkJCSCanonicalizeLarge benchmarks JCS with larger payload (~50KB)
func BenchmarkJCSCanonicalizeLarge(b *testing.B) {
	var largeData bytes.Buffer
	largeData.WriteString(`{
		"trace_id": "abc123def456",
		"model": "gpt-4o",
		"input": {
			"prompt": 5000,
			"messages": [
				{"role": "user", "content": "` + generateLongString(40000) + `"}
			]
		},
		"observations": {
			"tokens_count": 5200,
			"cost_estimated_usd": 0.01
		},
		"proofs": {"audit_signature": "test"}
	}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var data interface{}
		if err := json.Unmarshal(largeData.Bytes(), &data); err != nil {
			b.Fatal(err)
		}
		_, _ = Canonicalize(data)
	}
}

func generateLongString(length int) string {
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = 'x'
	}
	return string(result)
}

// BenchmarkStripNonSignatureFields benchmarks stripping non-signature fields
func BenchmarkStripNonSignatureFields(b *testing.B) {
	traceJSON := []byte(`{
		"trace_id": "abc123def456",
		"model": "gpt-4o",
		"execution_state": "finalized",
		"_internal_field": "should_be_stripped",
		"extensions": {
			"sentoris.ai/v1/test": {"config": "value"},
			"_experimental_field": "should_be_stripped"
		},
		"proofs": {"audit_signature": "test"}
	}`)

	var data map[string]interface{}
	if err := json.Unmarshal(traceJSON, &data); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = StripNonSignatureFields(data)
	}
}
