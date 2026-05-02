package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/audit"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/governance"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/router"
)

func TestChatCompletion(t *testing.T) {
	// 初始化依赖
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

	h := NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	// 测试基本聊天完成
	t.Run("BasicChatCompletion", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"model": "gpt-4o",
			"messages": []map[string]string{
				{"role": "user", "content": "Hello, how are you?"},
			},
			"max_tokens": 100,
		}

		reqJSON, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if _, ok := resp["id"]; !ok {
			t.Error("Response missing id field")
		}
		if _, ok := resp["object"]; !ok {
			t.Error("Response missing object field")
		}
		if _, ok := resp["created"]; !ok {
			t.Error("Response missing created field")
		}
		if _, ok := resp["model"]; !ok {
			t.Error("Response missing model field")
		}
		if _, ok := resp["choices"]; !ok {
			t.Error("Response missing choices field")
		}
		if _, ok := resp["usage"]; !ok {
			t.Error("Response missing usage field")
		}
	})

	// 测试流式响应
	t.Run("StreamingResponse", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"model": "gpt-4o",
			"messages": []map[string]string{
				{"role": "user", "content": "Tell me a short story"},
			},
			"max_tokens": 100,
			"stream":     true,
		}

		reqJSON, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		if w.Header().Get("Content-Type") != "text/event-stream" {
			t.Errorf("Expected Content-Type text/event-stream, got %s", w.Header().Get("Content-Type"))
		}
	})

	// 测试模型路由
	t.Run("ModelRouting", func(t *testing.T) {
		// 添加DeepSeek模型
		modelRouter.AddProvider("deepseek", &router.ProviderConfig{
			BaseURL:    "https://api.deepseek.com/v1",
			AuthHeader: "Bearer test-key",
			Models:     []string{"deepseek-chat"},
		})

		reqBody := map[string]interface{}{
			"model": "deepseek-chat",
			"messages": []map[string]string{
				{"role": "user", "content": "Hello from DeepSeek"},
			},
		}

		reqJSON, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	// 测试错误处理
	t.Run("ErrorHandling", func(t *testing.T) {
		// 空请求体
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

func TestTraceStorage(t *testing.T) {
	// 初始化依赖
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

	h := NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	// 发送聊天请求
	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "Test trace storage"},
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// 验证Trace存储
	traces, err := traceStore.ListBySession(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("Failed to list traces: %v", err)
	}

	if len(traces) == 0 {
		t.Error("Expected at least one trace in storage")
	}

	for _, trace := range traces {
		if trace.Model != "gpt-4o" {
			t.Errorf("Expected model gpt-4o, got %s", trace.Model)
		}
		if trace.ExecutionState != "FINALIZED" {
			t.Errorf("Expected execution state FINALIZED, got %s", trace.ExecutionState)
		}
		if trace.Output.Response == nil {
			t.Error("Expected output response to be stored")
		}
	}
}

func TestBudgetManagement(t *testing.T) {
	// 初始化依赖
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

	h := NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	// 测试预算消耗
	for i := 0; i < 5; i++ {
		reqBody := map[string]interface{}{
			"model": "gpt-4o",
			"messages": []map[string]string{
				{"role": "user", "content": fmt.Sprintf("Test budget %d", i)},
			},
		}

		reqJSON, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBuffer(reqJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	}

	// 验证预算消耗
	remaining, err := budgetStore.GetRemaining(context.Background(), "default")
	if err != nil {
		t.Fatalf("Failed to get remaining budget: %v", err)
	}

	// 预算应该有所减少
	t.Logf("Remaining budget: %f", remaining)
}

func TestHealthCheck(t *testing.T) {
	// 初始化依赖
	budgetStore := storage.NewMemoryBudgetStore()
	traceStore := storage.NewMemoryTraceStore()
	apiKeyStore := storage.NewMemoryAPIKeyStore()
	riskReportStore := storage.NewMemoryRiskReportStore()
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	h := NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	// 测试健康检查
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if status, ok := resp["status"].(string); !ok || status != "ok" {
		t.Error("Expected status ok")
	}
}
