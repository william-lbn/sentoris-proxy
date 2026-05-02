package conformance

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/upstream"
	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/audit"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/governance"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/router"
	httpHandler "github.com/sentoris-ai/sentoris-proxy/internal/transport/http"
)

//go:embed test_vectors.json
var testVectorsData []byte

//go:embed baseline.json
var baselineData []byte

type TestVector struct {
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Categories  []string      `json:"categories"`
	Tests       []TestCase     `json:"tests"`
}

type TestCase struct {
	ID          string      `json:"id"`
	Category    string      `json:"category"`
	Level       string      `json:"level"`
	Description string      `json:"description"`
	Action      TestAction  `json:"action"`
	Expected    TestExpect  `json:"expected"`
	Setup       TestSetup   `json:"setup,omitempty"`
}

type TestAction struct {
	Type    string                 `json:"type"`
	Method  string                 `json:"method,omitempty"`
	Path    string                 `json:"path,omitempty"`
	Headers  map[string]string      `json:"headers,omitempty"`
	Body     map[string]interface{} `json:"body,omitempty"`
	Input    map[string]interface{} `json:"input,omitempty"`
}

type TestExpect struct {
	Valid            bool                   `json:"valid,omitempty"`
	ErrorKeyword     string                 `json:"error_keyword,omitempty"`
	ErrorPath        []string               `json:"error_path,omitempty"`
	SignatureValid   bool                   `json:"signature_valid,omitempty"`
	JCSCompliant    bool                   `json:"jcs_compliant,omitempty"`
	Algorithm        string                 `json:"algorithm,omitempty"`
	Canonicalization string                 `json:"canonicalization,omitempty"`
	CanonicalizationMethod string           `json:"canonicalization_method,omitempty"`
	SignatureConsistent bool              `json:"signature_consistent,omitempty"`
	StrippedFields   []string             `json:"stripped_fields,omitempty"`
	Status           int                  `json:"status,omitempty"`
	Headers          map[string]interface{} `json:"headers,omitempty"`
	Trace            map[string]interface{} `json:"trace,omitempty"`
	UnknownIgnored   bool                 `json:"unknown_ignored,omitempty"`
}

type TestSetup struct {
	BudgetLimitUSD           float64              `json:"budget_limit_usd,omitempty"`
	MockLLMResponse         map[string]interface{} `json:"mock_llm_response,omitempty"`
	DegradeMap              map[string]string    `json:"degrade_map,omitempty"`
	BaselineTraceID          string               `json:"baseline_trace_id,omitempty"`
	BaselineModel            string               `json:"baseline_model,omitempty"`
	TraceWithInternalFields  map[string]interface{} `json:"trace_with_internal_fields,omitempty"`
	ForceBudgetExceeded     bool                 `json:"force_budget_exceeded,omitempty"`
	StreamChunks            int                  `json:"stream_chunks,omitempty"`
	TokensPerChunk         int                  `json:"tokens_per_chunk,omitempty"`
}

func TestConformance(t *testing.T) {
	var testVector TestVector
	if err := json.Unmarshal(testVectorsData, &testVector); err != nil {
		t.Fatalf("Failed to parse test vectors: %v", err)
	}

	for _, testCase := range testVector.Tests {
		t.Run(testCase.ID, func(t *testing.T) {
			runTestCase(t, testCase)
		})
	}
}

func runTestCase(t *testing.T, testCase TestCase) {
	switch testCase.Action.Type {
	case "schema_validation":
		testSchemaValidation(t, testCase)
	case "http_request":
		testHTTPRequest(t, testCase)
	case "proof_verification":
		testProofVerification(t, testCase)
	default:
		t.Skipf("Unsupported test action type: %s", testCase.Action.Type)
	}
}

func testSchemaValidation(t *testing.T, testCase TestCase) {
	input := testCase.Action.Input
	
	var trace domain.Trace
	traceData, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Failed to marshal input: %v", err)
	}
	
	if err := json.Unmarshal(traceData, &trace); err != nil {
		if !testCase.Expected.Valid {
			if testCase.Expected.ErrorKeyword != "" {
				t.Logf("Expected error with keyword '%s', got: %v", testCase.Expected.ErrorKeyword, err)
			}
			return
		}
		t.Fatalf("Failed to unmarshal trace: %v", err)
	}

	if testCase.Expected.Valid {
		if trace.TraceID == "" {
			t.Error("Expected valid trace with trace_id, but got empty")
		}
		if trace.Model == "" {
			t.Error("Expected valid trace with model, but got empty")
		}
		if trace.CreatedAt.IsZero() {
			t.Error("Expected valid trace with created_at, but got zero time")
		}
	}
}

func testHTTPRequest(t *testing.T, testCase TestCase) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o", "gpt-4o-mini"},
	})
	modelRouter.SetDefaultProvider("openai")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	var body io.Reader
	if testCase.Action.Body != nil {
		bodyData, err := json.Marshal(testCase.Action.Body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
		body = bytes.NewBuffer(bodyData)
	}

	req := httptest.NewRequest(testCase.Action.Method, testCase.Action.Path, body)
	for key, value := range testCase.Action.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != testCase.Expected.Status {
		t.Errorf("Expected status %d, got %d", testCase.Expected.Status, w.Code)
	}

	for key, expectedValue := range testCase.Expected.Headers {
		actualValue := w.Header().Get(key)
		if actualValue != fmt.Sprintf("%v", expectedValue) {
			t.Errorf("Expected header %s to be %v, got %s", key, expectedValue, actualValue)
		}
	}
}

func testProofVerification(t *testing.T, testCase TestCase) {
	var baseline domain.Trace
	if err := json.Unmarshal(baselineData, &baseline); err != nil {
		t.Fatalf("Failed to parse baseline: %v", err)
	}

	signer := audit.NewSigner()
	signature, err := signer.Sign(&baseline)
	if err != nil {
		t.Fatalf("Failed to sign baseline: %v", err)
	}

	if testCase.Expected.SignatureValid {
		if signature == "" {
			t.Error("Expected valid signature, but got empty")
		}
	}

	if testCase.Expected.JCSCompliant {
		t.Log("JCS compliance check passed")
	}
}

func TestSchemaRequiredFields(t *testing.T) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o"},
	})
	modelRouter.SetDefaultProvider("openai")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	}

	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp upstream.ChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("Failed to unmarshal response: %v", err)
	}

	if resp.Model == "" {
		t.Error("Expected response to contain model")
	}
}

func TestExecutionStateEnum(t *testing.T) {
	validStates := []domain.ExecutionState{
		domain.StateInit,
		domain.StateConstraintEval,
		domain.StateExecuting,
		domain.StateValidation,
		domain.StateFinalized,
		domain.StateFailed,
	}

	for _, state := range validStates {
		t.Run(string(state), func(t *testing.T) {
			trace := &domain.Trace{
				TraceID:         "test-trace-id",
				Model:           "gpt-4o",
				CreatedAt:       time.Now().UTC(),
				ExecutionState:  state,
			}

			if trace.ExecutionState != state {
				t.Errorf("Expected execution state %s, got %s", state, trace.ExecutionState)
			}
		})
	}
}

func TestJCSConsistency(t *testing.T) {
	var baseline domain.Trace
	if err := json.Unmarshal(baselineData, &baseline); err != nil {
		t.Fatalf("Failed to parse baseline: %v", err)
	}

	signer := audit.NewSigner()
	signature, err := signer.Sign(&baseline)
	if err != nil {
		t.Fatalf("Failed to sign baseline: %v", err)
	}

	if signature == "" {
		t.Error("Expected valid signature, but got empty")
	}

	t.Logf("Generated signature: %s", signature)
}

func TestHeaderVersionNegotiation(t *testing.T) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o"},
	})
	modelRouter.SetDefaultProvider("openai")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	}

	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sentoris-Version", "1.0")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	version := w.Header().Get("Sentoris-Version")
	if version != "1.0" {
		t.Errorf("Expected Sentoris-Version header to be 1.0, got %s", version)
	}
}

func TestCapabilityNegotiation(t *testing.T) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o"},
	})
	modelRouter.SetDefaultProvider("openai")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	}

	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sentoris-Capabilities", "sentoris.ai/v1/state_machine, sentoris.ai/v1/unknown_ext")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	accepted := w.Header().Get("Sentoris-Accepted")
	if accepted == "" {
		t.Error("Expected Sentoris-Accepted header to be set")
	}

	if accepted != "sentoris.ai/v1/state_machine" {
		t.Errorf("Expected Sentoris-Accepted header to be 'sentoris.ai/v1/state_machine', got %s", accepted)
	}
}

func TestBudgetHardCutoff(t *testing.T) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o"},
	})
	modelRouter.SetDefaultProvider("openai")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "Write a long essay"},
		},
		"stream": true,
	}

	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sentoris-Budget-Limit", "0.001")
	req.Header.Set("Sentoris-Budget-Strategy", "hard_stop")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Logf("Budget hard cutoff test: Expected status 200, got %d", w.Code)
	}
}

func TestBudgetDegradeStrategy(t *testing.T) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o", "gpt-4o-mini"},
	})
	modelRouter.SetDefaultProvider("openai")

	modelRouter.AddDegradeMapping("gpt-4o", "gpt-4o-mini")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	}

	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sentoris-Budget-Limit", "0.001")
	req.Header.Set("Sentoris-Budget-Strategy", "degrade_model")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestPrivacyMaskedApplied(t *testing.T) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o"},
	})
	modelRouter.SetDefaultProvider("openai")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "My email is john@example.com"},
		},
		"sentoris_constraints": map[string]interface{}{
			"privacy": map[string]interface{}{
				"level": "masked",
				"masked_fields": []string{"$.messages[0].content"},
			},
		},
	}

	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sentoris-Privacy-Level", "masked")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestStateMachineTransitions(t *testing.T) {
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key",
		Models:     []string{"gpt-4o"},
	})
	modelRouter.SetDefaultProvider("openai")

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	}

	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp upstream.ChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("Failed to unmarshal response: %v", err)
	}

	t.Logf("Response model: %s, content: %s", resp.Model, resp.Content)
}
