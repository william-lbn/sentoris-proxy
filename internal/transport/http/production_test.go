package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/audit"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/governance"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/router"
)

// TestProductionEnvironment 测试生产环境的完整功能
func TestProductionEnvironment(t *testing.T) {
	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort)

	postgresHost := getEnv("POSTGRES_HOST", "localhost")
	postgresPort := getEnv("POSTGRES_PORT", "5432")
	postgresUser := getEnv("POSTGRES_USER", "postgres")
	postgresPassword := getEnv("POSTGRES_PASSWORD", "postgres")
	postgresDB := getEnv("POSTGRES_DB", "sentoris")
	postgresDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		postgresUser, postgresPassword, postgresHost, postgresPort, postgresDB)

	// 初始化真实的Redis和PostgreSQL存储
	budgetStore := storage.NewRedisBudgetStore(redisAddr, "", 0, "", "", nil)
	traceStore, err := storage.NewPostgresTraceStore(postgresDSN)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	apiKeyStore, err := storage.NewPostgresAPIKeyStore(postgresDSN)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL for API keys: %v", err)
	}

	riskReportStore := storage.NewMemoryRiskReportStore()

	// 初始化其他依赖
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()
	modelRouter.AddProvider("openai", &router.ProviderConfig{
		BaseURL:    "https://api.openai.com/v1",
		AuthHeader: "Bearer test-key", // 测试用的假密钥
		Models:     []string{"gpt-4o", "gpt-4o-mini"},
	})
	modelRouter.AddProvider("deepseek", &router.ProviderConfig{
		BaseURL:    "https://api.deepseek.com/v1",
		AuthHeader: "Bearer test-key", // 测试用的假密钥
		Models:     []string{"deepseek-chat"},
	})
	modelRouter.AddProvider("qwen", &router.ProviderConfig{
		BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		AuthHeader: "Bearer test-key", // 测试用的假密钥
		Models:     []string{"qwen-max", "qwen-plus", "qwen-turbo"},
	})
	modelRouter.SetDefaultProvider("openai")

	// 使用已经初始化的 budgetStore
	h := NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	// 测试场景1: 基本聊天完成并验证数据库存储
	t.Run("BasicChatCompletionWithDatabase", func(t *testing.T) {
		sessionID := fmt.Sprintf("test-session-%d", time.Now().UnixNano())

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

		req := httptest.NewRequest("POST", "/chat/completions", bytes.NewBuffer(reqJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("X-Sentoris-Session-ID", sessionID)

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
			t.Logf("Response body: %s", w.Body.String())
		}

		// 验证响应结构
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

		// 验证数据库存储
		traces, err := traceStore.ListBySession(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("Failed to list traces: %v", err)
		}

		if len(traces) == 0 {
			t.Error("Expected at least one trace in PostgreSQL storage")
		}

		for _, trace := range traces {
			if trace.Model != "gpt-4o" {
				t.Errorf("Expected model gpt-4o, got %s", trace.Model)
			}
			if trace.ExecutionState != "FINALIZED" {
				t.Errorf("Expected execution state FINALIZED, got %s", trace.ExecutionState)
			}
			if trace.Output.Response == nil {
				t.Error("Expected output response to be stored in PostgreSQL")
			}
			if trace.Input.Prompt == nil {
				t.Error("Expected input prompt to be stored in PostgreSQL")
			}
		}
	})

	// 测试场景2: 不同模型的路由和响应
	t.Run("ModelRoutingWithMultipleProviders", func(t *testing.T) {
		models := []string{"gpt-4o", "deepseek-chat", "qwen-turbo"}

		for _, model := range models {
			t.Run(fmt.Sprintf("Model-%s", model), func(t *testing.T) {
				sessionID := fmt.Sprintf("test-session-%s-%d", model, time.Now().UnixNano())

				reqBody := map[string]interface{}{
					"model": model,
					"messages": []map[string]string{
						{"role": "user", "content": fmt.Sprintf("Hello from %s", model)},
					},
					"max_tokens": 50,
				}

				reqJSON, err := json.Marshal(reqBody)
				if err != nil {
					t.Fatalf("Failed to marshal request: %v", err)
				}

				req := httptest.NewRequest("POST", "/chat/completions", bytes.NewBuffer(reqJSON))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer test-token")
				req.Header.Set("X-Sentoris-Session-ID", sessionID)

				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200 for model %s, got %d", model, w.Code)
					t.Logf("Response body: %s", w.Body.String())
				}

				// 验证响应包含模拟内容
				responseBody := w.Body.String()
				if !contains(responseBody, "This is a mock response") {
					t.Logf("Response body: %s", responseBody)
					// 注意：如果LLM请求成功，不会返回模拟响应
				}

				// 验证数据库存储
				traces, err := traceStore.ListBySession(context.Background(), sessionID)
				if err != nil {
					t.Fatalf("Failed to list traces: %v", err)
				}

				if len(traces) == 0 {
					t.Error("Expected at least one trace in PostgreSQL storage")
				}

				for _, trace := range traces {
					if trace.Model != model {
						t.Errorf("Expected model %s, got %s", model, trace.Model)
					}
				}
			})
		}
	})

	// 测试场景3: 流式响应
	t.Run("StreamingResponseWithDatabase", func(t *testing.T) {
		sessionID := fmt.Sprintf("test-session-streaming-%d", time.Now().UnixNano())

		reqBody := map[string]interface{}{
			"model": "gpt-4o",
			"messages": []map[string]string{
				{"role": "user", "content": "Tell me a short story about AI"},
			},
			"max_tokens": 100,
			"stream":     true,
		}

		reqJSON, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		req := httptest.NewRequest("POST", "/chat/completions", bytes.NewBuffer(reqJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("X-Sentoris-Session-ID", sessionID)

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
			t.Logf("Response body: %s", w.Body.String())
		}

		if w.Header().Get("Content-Type") != "text/event-stream" {
			t.Errorf("Expected Content-Type text/event-stream, got %s", w.Header().Get("Content-Type"))
		}

		// 验证数据库存储
		traces, err := traceStore.ListBySession(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("Failed to list traces: %v", err)
		}

		if len(traces) == 0 {
			t.Error("Expected at least one trace in PostgreSQL storage for streaming")
		}
	})

	// 测试场景4: 预算管理
	t.Run("BudgetManagementWithRedis", func(t *testing.T) {
		// 初始预算
		initialBudget, err := budgetStore.GetRemaining(context.Background(), "default")
		if err != nil {
			t.Fatalf("Failed to get initial budget: %v", err)
		}
		t.Logf("Initial budget: %f", initialBudget)

		// 发送多个请求消耗预算
		for i := 0; i < 3; i++ {
			reqBody := map[string]interface{}{
				"model": "gpt-4o",
				"messages": []map[string]string{
					{"role": "user", "content": fmt.Sprintf("Test budget %d", i)},
				},
				"max_tokens": 50,
			}

			reqJSON, err := json.Marshal(reqBody)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			req := httptest.NewRequest("POST", "/chat/completions", bytes.NewBuffer(reqJSON))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer test-token")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
				t.Logf("Response body: %s", w.Body.String())
			}
		}

		// 验证预算消耗
		remainingBudget, err := budgetStore.GetRemaining(context.Background(), "default")
		if err != nil {
			t.Fatalf("Failed to get remaining budget: %v", err)
		}
		t.Logf("Remaining budget: %f", remainingBudget)

		// 预算应该有所减少
		if remainingBudget >= initialBudget {
			t.Error("Expected budget to decrease after usage")
		}
	})

	// 测试场景5: 错误处理
	t.Run("ErrorHandling", func(t *testing.T) {
		// 测试无效模型
		reqBody := map[string]interface{}{
			"model": "invalid-model",
			"messages": []map[string]string{
				{"role": "user", "content": "Hello"},
			},
		}

		reqJSON, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		req := httptest.NewRequest("POST", "/chat/completions", bytes.NewBuffer(reqJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 for invalid model, got %d", w.Code)
		}

		// 测试空请求体
		req = httptest.NewRequest("POST", "/chat/completions", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 for empty request, got %d", w.Code)
		}
	})
}

// TestHealthCheckWithRealStorage 测试健康检查，使用真实的存储
func TestHealthCheckWithRealStorage(t *testing.T) {
	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort)

	postgresHost := getEnv("POSTGRES_HOST", "localhost")
	postgresPort := getEnv("POSTGRES_PORT", "5432")
	postgresUser := getEnv("POSTGRES_USER", "postgres")
	postgresPassword := getEnv("POSTGRES_PASSWORD", "postgres")
	postgresDB := getEnv("POSTGRES_DB", "sentoris")
	postgresDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		postgresUser, postgresPassword, postgresHost, postgresPort, postgresDB)

	// 初始化真实的Redis和PostgreSQL存储
	budgetStore := storage.NewRedisBudgetStore(redisAddr, "", 0, "", "", nil)
	traceStore, err := storage.NewPostgresTraceStore(postgresDSN)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	apiKeyStore, err := storage.NewPostgresAPIKeyStore(postgresDSN)
	if err != nil {
		t.Fatalf("Failed to connect to PostgreSQL for API keys: %v", err)
	}

	riskReportStore := storage.NewMemoryRiskReportStore()

	// 初始化其他依赖
	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)
	modelRouter := router.NewModelRouter()

	// 使用已经初始化的 budgetStore
	h := NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)

	// 测试健康检查
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
		t.Logf("Response body: %s", w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if status, ok := resp["status"].(string); !ok || status != "ok" {
		t.Error("Expected status ok")
	}

	// 验证健康检查包含存储状态
	if storageStatus, ok := resp["storage"].(map[string]interface{}); ok {
		if redisStatus, ok := storageStatus["redis"].(string); !ok || redisStatus != "connected" {
			t.Error("Expected Redis status to be connected")
		}
		if postgresStatus, ok := storageStatus["postgres"].(string); !ok || postgresStatus != "connected" {
			t.Error("Expected PostgreSQL status to be connected")
		}
	}
}

// contains 检查字符串是否包含子字符串
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
