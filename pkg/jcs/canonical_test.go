package jcs

import (
	"math"
	"testing"
)

func TestJCSConsistency_StandardVectors(t *testing.T) {
	testCases := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "empty object",
			input:    map[string]any{},
			expected: "{}",
		},
		{
			name:     "empty array",
			input:    []any{},
			expected: "[]",
		},
		{
			name:     "null",
			input:    nil,
			expected: "null",
		},
		{
			name:     "true",
			input:    true,
			expected: "true",
		},
		{
			name:     "false",
			input:    false,
			expected: "false",
		},
		{
			name:     "zero",
			input:    0,
			expected: "0",
		},
		{
			name:     "negative zero",
			input:    math.Float64frombits(0x8000000000000000),
			expected: "-0",
		},
		{
			name:     "positive number",
			input:    42,
			expected: "42",
		},
		{
			name:     "negative number",
			input:    -42,
			expected: "-42",
		},
		{
			name:     "float",
			input:    3.14159,
			expected: "3.14159",
		},
		{
			name:     "string",
			input:    "hello",
			expected: "\"hello\"",
		},
		{
			name:     "string with escaped chars",
			input:    "hello\nworld",
			expected: "\"hello\\nworld\"",
		},
		{
			name:     "string with quote",
			input:    "hello\"world",
			expected: "\"hello\\\"world\"",
		},
		{
			name:     "simple object",
			input:    map[string]any{"a": "b"},
			expected: "{\"a\":\"b\"}",
		},
		{
			name:     "object with sorted keys",
			input:    map[string]any{"b": "2", "a": "1"},
			expected: "{\"a\":\"1\",\"b\":\"2\"}",
		},
		{
			name:     "nested object",
			input:    map[string]any{"a": map[string]any{"c": "3", "b": "2"}},
			expected: "{\"a\":{\"b\":\"2\",\"c\":\"3\"}}",
		},
		{
			name:     "array with values",
			input:    []any{1, "two", true},
			expected: "[1,\"two\",true]",
		},
		{
			name:     "nested array",
			input:    []any{[]any{1, 2}, []any{3, 4}},
			expected: "[[1,2],[3,4]]",
		},
		{
			name:     "mixed types",
			input:    map[string]any{"num": 42, "str": "test", "arr": []any{1, 2}, "obj": map[string]any{"nested": true}},
			expected: "{\"arr\":[1,2],\"num\":42,\"obj\":{\"nested\":true},\"str\":\"test\"}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CanonicalizeToString(tc.input)
			if err != nil {
				t.Fatalf("CanonicalizeToString failed: %v", err)
			}
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestJCSConsistency_SpecialNumbers(t *testing.T) {
	testCases := []struct {
		name     string
		input    float64
		expected string
	}{
		{name: "NaN", input: math.NaN(), expected: "null"},
		{name: "positive infinity", input: math.Inf(1), expected: "null"},
		{name: "negative infinity", input: math.Inf(-1), expected: "null"},
		{name: "zero", input: 0.0, expected: "0"},
		{name: "negative zero", input: math.Float64frombits(0x8000000000000000), expected: "-0"},
		{name: "integer", input: 123456789, expected: "123456789"},
		{name: "float with trailing zeros", input: 3.14000, expected: "3.14"},
		{name: "scientific notation", input: 1e10, expected: "10000000000"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := string(canonicalizeNumber(tc.input))
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestJCSConsistency_UnicodeStrings(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty string", input: "", expected: "\"\""},
		{name: "ASCII string", input: "Hello World!", expected: "\"Hello World!\""},
		{name: "Unicode string", input: "Hello 世界", expected: "\"Hello 世界\""},
		{name: "control characters", input: "\x00\x01\x02", expected: "\"\\u0000\\u0001\\u0002\""},
		{name: "backslash", input: "\\", expected: "\"\\\\\""},
		{name: "quote", input: "\"", expected: "\"\\\"\""},
		{name: "newline", input: "\n", expected: "\"\\n\""},
		{name: "tab", input: "\t", expected: "\"\\t\""},
		{name: "carriage return", input: "\r", expected: "\"\\r\""},
		{name: "bell", input: "\x07", expected: "\"\\u0007\""},
		{name: "form feed", input: "\x0c", expected: "\"\\u000c\""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := canonicalizeString(tc.input)
			if err != nil {
				t.Fatalf("canonicalizeString failed: %v", err)
			}
			if string(result) != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, string(result))
			}
		})
	}
}

func TestJCSConsistency_NestedStructures(t *testing.T) {
	complexObj := map[string]any{
		"level1": map[string]any{
			"z": 1,
			"a": map[string]any{
				"nested": "value",
				"array":  []any{1, 2, map[string]any{"key": "val"}},
			},
		},
		"level2": []any{
			map[string]any{"c": 3},
			map[string]any{"b": 2},
			map[string]any{"a": 1},
		},
	}

	expected := "{\"level1\":{\"a\":{\"array\":[1,2,{\"key\":\"val\"}],\"nested\":\"value\"},\"z\":1},\"level2\":[{\"c\":3},{\"b\":2},{\"a\":1}]}"

	result, err := CanonicalizeToString(complexObj)
	if err != nil {
		t.Fatalf("CanonicalizeToString failed: %v", err)
	}

	if result != expected {
		t.Errorf("Complex object canonicalization failed.\nExpected: %q\nGot:      %q", expected, result)
	}
}

func TestJCSConsistency_HashConsistency(t *testing.T) {
	data1 := map[string]any{"a": "b", "c": "d"}
	data2 := map[string]any{"c": "d", "a": "b"}

	hash1, err := CanonicalizeAndHash(data1)
	if err != nil {
		t.Fatalf("CanonicalizeAndHash failed for data1: %v", err)
	}

	hash2, err := CanonicalizeAndHash(data2)
	if err != nil {
		t.Fatalf("CanonicalizeAndHash failed for data2: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("Hashes should be equal for semantically identical objects.\nHash1: %s\nHash2: %s", hash1, hash2)
	}
}

func TestJCSConsistency_StrippingConsistency(t *testing.T) {
	input := map[string]any{
		"trace_id":   "test-trace",
		"model":      "gpt-4o",
		"_internal":  "secret",
		"internal_metrics": map[string]any{
			"latency": 100,
		},
		"extensions": map[string]any{
			"public_ext": map[string]any{
				"enabled":         true,
				"_experimental":   "hidden",
				"_internal_data": 42,
			},
			"_private_ext": map[string]any{
				"data": "secret",
			},
		},
	}

	stripped := StripNonSignatureFields(input)

	// Check that internal fields are stripped
	if _, exists := stripped["_internal"]; exists {
		t.Error("Expected _internal field to be stripped")
	}

	if _, exists := stripped["internal_metrics"]; exists {
		t.Error("Expected internal_metrics field to be stripped")
	}

	// Check extensions
	ext, exists := stripped["extensions"].(map[string]any)
	if !exists {
		t.Fatal("Expected extensions to exist")
	}

	if _, exists := ext["_private_ext"]; exists {
		t.Error("Expected _private_ext extension to be stripped")
	}

	publicExt, exists := ext["public_ext"].(map[string]any)
	if !exists {
		t.Fatal("Expected public_ext extension to exist")
	}

	if _, exists := publicExt["_experimental"]; exists {
		t.Error("Expected _experimental field to be stripped from extension")
	}

	if _, exists := publicExt["_internal_data"]; exists {
		t.Error("Expected _internal_data field to be stripped from extension")
	}

	if publicExt["enabled"] != true {
		t.Error("Expected enabled field to remain")
	}
}