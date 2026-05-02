package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/upstream"
	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/audit"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/diff"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/extensions"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/governance"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/hooks"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/router"
	"github.com/sentoris-ai/sentoris-proxy/pkg/errors"
	"github.com/sentoris-ai/sentoris-proxy/pkg/logger"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Handler struct {
	modelRouter       *router.ModelRouter
	signer            *audit.Signer
	constraintEval    *governance.ConstraintEvaluator
	traceStore        storage.TraceStore
	budgetStore       storage.BudgetStore
	apiKeyStore       storage.APIKeyStore
	riskReportStore   storage.RiskReportStore
	differ            *diff.Differ
	hookChain         *hooks.HookChain
	extensionRegistry *extensions.ExtensionRegistry
	upstreamClients   map[string]*upstreamClient
	privacyService    *governance.PrivacyService
}

type upstreamClient struct {
	baseURL string
	client  *http.Client
}

func NewUpstreamClient(baseURL string) *upstreamClient {
	return &upstreamClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *upstreamClient) Chat(ctx context.Context, req *upstream.ChatRequest) (*upstream.ChatResponse, error) {
	openaiReq := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   req.Stream,
	}
	if req.Params != nil {
		if maxTokens, ok := req.Params["max_tokens"]; ok {
			openaiReq["max_tokens"] = maxTokens
		}
		if temperature, ok := req.Params["temperature"]; ok {
			openaiReq["temperature"] = temperature
		}
	}
	if req.Seed != nil {
		openaiReq["seed"] = *req.Seed
	}

	reqBytes, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer mock-key")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if req.Stream {
		return c.parseStreamResponse(resp)
	}

	var llmResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return nil, err
	}

	var content, finishReason string
	var tokensUsed int
	if len(llmResp.Choices) > 0 {
		content = llmResp.Choices[0].Message.Content
		finishReason = llmResp.Choices[0].FinishReason
	}
	tokensUsed = llmResp.Usage.TotalTokens

	upstreamResp := &upstream.ChatResponse{
		Model:        llmResp.Model,
		Content:      content,
		FinishReason: finishReason,
		TokensUsed:   tokensUsed,
		CostUSD:      float64(tokensUsed) * 0.00001,
	}

	return upstreamResp, nil
}

func (c *upstreamClient) parseStreamResponse(resp *http.Response) (*upstream.ChatResponse, error) {
	var content strings.Builder
	var model string
	var totalTokens int

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Model != "" {
			model = chunk.Model
		}
		if len(chunk.Choices) > 0 {
			content.WriteString(chunk.Choices[0].Delta.Content)
			totalTokens++
		}
	}

	return &upstream.ChatResponse{
		Model:        model,
		Content:      content.String(),
		FinishReason: "stop",
		TokensUsed:   totalTokens * 2,
		CostUSD:      float64(totalTokens*2) * 0.00001,
	}, nil
}

func NewHandler(modelRouter *router.ModelRouter, signer *audit.Signer, constraintEval *governance.ConstraintEvaluator, traceStore storage.TraceStore, budgetStore storage.BudgetStore, apiKeyStore storage.APIKeyStore, riskReportStore storage.RiskReportStore) *Handler {
	return &Handler{
		modelRouter:     modelRouter,
		signer:          signer,
		constraintEval:  constraintEval,
		differ:          diff.NewDiffer(),
		traceStore:      traceStore,
		budgetStore:     budgetStore,
		apiKeyStore:     apiKeyStore,
		riskReportStore: riskReportStore,
		hookChain:       hooks.NewHookChain(""),
		privacyService:  governance.NewPrivacyService(),
	}
}

func (h *Handler) SetHookChain(hookChain *hooks.HookChain) {
	h.hookChain = hookChain
}

func (h *Handler) SetExtensionRegistry(registry *extensions.ExtensionRegistry) {
	h.extensionRegistry = registry
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	path := r.URL.Path

	isMonitorPath := (path == "/health" ||
		path == "/v1/monitor" ||
		path == "/v1/monitor/metrics" ||
		path == "/v1/monitor/models" ||
		path == "/v1/monitor/extensions" ||
		path == "/v1/monitor/traces" ||
		path == "/v1/monitor/traces-list" ||
		path == "/v1/monitor/budget" ||
		path == "/v1/monitor/risk-reports")

	_ = isMonitorPath
	_ = strings.HasPrefix(path, "/v1/admin/")

	switch path {
	case "/v1/chat/completions":
		h.handleChatCompletions(w, r)
	case "/v1/models":
		h.handleModels(w, r)
	case "/health":
		h.handleHealthCheck(w, r)
	case "/v1/monitor":
		h.handleMonitor(w, r)
	case "/v1/monitor/metrics":
		h.handleMonitorMetrics(w, r)
	case "/v1/monitor/models":
		h.handleMonitorModels(w, r)
	case "/v1/monitor/extensions":
		h.handleMonitorExtensions(w, r)
	case "/v1/monitor/traces":
		h.handleMonitorTraces(w, r)
	case "/v1/monitor/traces-list":
		h.handleTracesList(w, r)
	case "/v1/monitor/budget":
		h.handleMonitorBudget(w, r)
	case "/v1/monitor/risk-reports":
		h.handleMonitorRiskReports(w, r)
	case "/v1/replay-eval":
		h.handleReplayEval(w, r)
	case "/v1/admin/providers":
		h.handleAdminProviders(w, r)
	case "/v1/admin/providers/test":
		h.handleTestProvider(w, r)
	case "/v1/admin/providers/set-default":
		h.handleSetDefaultProvider(w, r)
	case "/v1/admin/extensions":
		h.handleAdminExtensions(w, r)
	case "/v1/admin/budget":
		h.handleAdminBudget(w, r)
	case "/v1/admin/api-keys":
		h.handleAdminAPIKeys(w, r)
	case "/v1/verify/trace":
		h.handleVerifyTrace(w, r)
	default:
		if strings.HasPrefix(path, "/v1/verify/trace/") {
			traceID := strings.TrimPrefix(path, "/v1/verify/trace/")
			if traceID != "" {
				h.handleVerifyTraceByID(w, r, traceID)
				return
			}
		}
		h.handleError(w, "Not found", http.StatusNotFound, errors.ErrBaselineNotFound)
	}

	logger.Debug("Request processed", "path", path, "method", r.Method, "duration", time.Since(start))
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
		return
	}

	sentorisHeaders := ParseSentorisHeaders(r)

	if !sentorisHeaders.IsVersionSupported() {
		h.handleError(w, fmt.Sprintf("Protocol version not supported: %s. Supported versions: %v", sentorisHeaders.Version, supportedVersions), http.StatusBadRequest, errors.ErrInvalidInput)
		return
	}

	var chatReq upstream.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
		return
	}

	if sentorisHeaders.Seed != nil {
		chatReq.Seed = sentorisHeaders.Seed
	}

	ctx := r.Context()
	stateRecorder := NewStateTransitionRecorder()
	stateRecorder.RecordTransition("", domain.StateInit)

	traceID := generateTraceID()
	sessionID := generateSessionID()

	modelToUse := chatReq.Model
	if sentorisHeaders.OverrideModel != "" {
		modelToUse = sentorisHeaders.OverrideModel
	}

	trace := &domain.Trace{
		TraceID:        traceID,
		SessionID:      &sessionID,
		Model:          modelToUse,
		ExecutionState: domain.StateInit,
		CreatedAt:      time.Now(),
		Input: domain.Input{
			Prompt:   chatReq.Messages,
			Params:   chatReq.Params,
			Metadata: map[string]any{},
		},
	}

	if err := h.traceStore.Save(ctx, trace); err != nil {
		logger.Error("Failed to save trace", "error", err)
	}

	if h.hookChain != nil {
		if err := h.hookChain.PreExecute(ctx, trace); err != nil {
			logger.Error("PreExecute hook failed", "error", err)
		}
	}

	stateRecorder.RecordTransition(domain.StateInit, domain.StateConstraintEval)
	trace.ExecutionState = domain.StateConstraintEval
	_ = h.traceStore.Update(ctx, trace)

	constraints := sentorisHeaders.ToConstraints()
	currentCostUSD := 0.0
	if h.constraintEval == nil {
		h.constraintEval = governance.NewConstraintEvaluator(nil)
	}
	evaluationResult := h.constraintEval.Evaluate(ctx, constraints, currentCostUSD)

	if evaluationResult.Decision == domain.PolicyDecisionBlock {
		stateRecorder.RecordTransition(domain.StateConstraintEval, domain.StateFailed)
		trace.ExecutionState = domain.StateFailed
		trace.Observations.Error = evaluationResult.Error
		if evaluationResult.ConstraintsUsed != nil {
			trace.ConstraintsApplied = *evaluationResult.ConstraintsUsed
		}
		trace.ConstraintsApplied.PolicyEvaluation = &domain.PolicyEvaluation{
			Decision:     evaluationResult.Decision,
			ChecksPassed: evaluationResult.ChecksPassed,
		}
		stateRecorder.ApplyToObservations(&trace.Observations)
		_ = h.traceStore.Update(ctx, trace)

		errCode := errors.ErrBudgetExceeded
		if evaluationResult.Error != nil && evaluationResult.Error.Code == string(errors.ErrInvalidConstraint) {
			errCode = errors.ErrInvalidConstraint
		}
		h.handleError(w, evaluationResult.Error.Message, http.StatusTooManyRequests, errCode)
		return
	}

	if evaluationResult.Decision == domain.PolicyDecisionDegrade && evaluationResult.DegradeAction != nil {
		trace.Observations.DegradeAction = evaluationResult.DegradeAction
		if evaluationResult.ConstraintsUsed != nil {
			trace.ConstraintsApplied = *evaluationResult.ConstraintsUsed
			trace.ConstraintsApplied.DegradeAction = evaluationResult.DegradeAction
		}
		trace.ConstraintsApplied.PolicyEvaluation = &domain.PolicyEvaluation{
			Decision:     evaluationResult.Decision,
			ChecksPassed: evaluationResult.ChecksPassed,
		}
	} else if evaluationResult.ConstraintsUsed != nil {
		trace.ConstraintsApplied = *evaluationResult.ConstraintsUsed
		trace.ConstraintsApplied.PolicyEvaluation = &domain.PolicyEvaluation{
			Decision:     evaluationResult.Decision,
			ChecksPassed: evaluationResult.ChecksPassed,
		}
	}

	stateRecorder.RecordTransition(domain.StateConstraintEval, domain.StateExecuting)
	trace.ExecutionState = domain.StateExecuting
	_ = h.traceStore.Update(ctx, trace)

	var providerConfig *router.ProviderConfig
	providers := h.modelRouter.GetAllProviders()
	for _, config := range providers {
		for _, m := range config.Models {
			if m == modelToUse {
				providerConfig = config
				break
			}
		}
		if providerConfig != nil {
			break
		}
	}

	if providerConfig == nil {
		defaultProvider := h.modelRouter.GetDefaultProvider()
		if defaultProvider != "" {
			if config, ok := h.modelRouter.GetProvider(defaultProvider); ok {
				providerConfig = config
			}
		}
	}

	if providerConfig == nil {
		stateRecorder.RecordTransition(trace.ExecutionState, domain.StateFailed)
		trace.ExecutionState = domain.StateFailed
		trace.Observations.Error = &domain.ErrorInfo{
			Code:    "PROVIDER_NOT_FOUND",
			Message: "Model not found",
		}
		stateRecorder.ApplyToObservations(&trace.Observations)
		_ = h.traceStore.Update(ctx, trace)
		h.handleError(w, "Model not found", http.StatusNotFound, errors.ErrProviderNotFound)
		return
	}

	// 如果有预算限制，先预留预算
	var reservedAmount float64
	var didReserve bool
	if sentorisHeaders.BudgetLimit > 0 {
		// 估算最大可能的费用 - 假设输入有1000 tokens，输出有4000 tokens
		inputPrice := providerConfig.Pricing.InputPer1K
		outputPrice := providerConfig.Pricing.OutputPer1K
		if inputPrice == 0 {
			inputPrice = 0.001 // 默认输入价格
		}
		if outputPrice == 0 {
			outputPrice = 0.002 // 默认输出价格
		}

		// 保守估算：1000 input tokens + 4000 output tokens
		reservedAmount = float64(1000)*inputPrice/1000 + float64(4000)*outputPrice/1000

		// 如果没有预算存储，我们用内存存储模拟
		if h.budgetStore != nil {
			// 设置预算
			if sessionID != "" {
				if err := h.budgetStore.SetBudget(ctx, sessionID, sentorisHeaders.BudgetLimit); err != nil {
					logger.Error("Failed to set budget", "error", err)
				}
				// 预留预算
				ok, err := h.budgetStore.Reserve(ctx, sessionID, reservedAmount)
				if err != nil {
					logger.Error("Failed to reserve budget", "error", err)
				} else if !ok {
					// 预算不足
					stateRecorder.RecordTransition(trace.ExecutionState, domain.StateFailed)
					trace.ExecutionState = domain.StateFailed
					trace.Observations.Error = &domain.ErrorInfo{
						Code:    "BUDGET_EXCEEDED",
						Message: "Budget limit exceeded",
					}
					stateRecorder.ApplyToObservations(&trace.Observations)
					_ = h.traceStore.Update(ctx, trace)
					h.handleError(w, "Budget limit exceeded", http.StatusTooManyRequests, errors.ErrBudgetExceeded)
					return
				}
				didReserve = true
			}
		}
	}

	client := NewUpstreamClient(providerConfig.BaseURL)
	resp, err := client.Chat(ctx, &chatReq)
	if err != nil {
		// 回滚预算
		if didReserve && h.budgetStore != nil && sessionID != "" {
			if rollbackErr := h.budgetStore.Rollback(ctx, sessionID, reservedAmount); rollbackErr != nil {
				logger.Error("Failed to rollback budget", "error", rollbackErr)
			}
		}

		stateRecorder.RecordTransition(trace.ExecutionState, domain.StateFailed)
		trace.ExecutionState = domain.StateFailed
		trace.Observations.Error = &domain.ErrorInfo{
			Code:    "UPSTREAM_ERROR",
			Message: err.Error(),
		}
		stateRecorder.ApplyToObservations(&trace.Observations)

		if h.hookChain != nil {
			if err := h.hookChain.OnFailure(ctx, trace); err != nil {
				logger.Error("OnFailure hook failed", "error", err)
			}
		}

		_ = h.traceStore.Update(ctx, trace)
		h.setErrorResponseHeaders(w, traceID, errors.ErrUpstreamError)
		h.handleErrorWithMessage(w, "Upstream request failed", http.StatusBadGateway, err)
		return
	}

	// 提交实际的预算消耗
	if didReserve && h.budgetStore != nil && sessionID != "" {
		if commitErr := h.budgetStore.Commit(ctx, sessionID, resp.CostUSD); commitErr != nil {
			logger.Error("Failed to commit budget", "error", commitErr)
		}
	}

	if sentorisHeaders.PrivacyLevel != domain.PrivacyRaw {
		maskingResult, err := h.privacyService.ApplyMasking(ctx, sentorisHeaders.PrivacyLevel, resp.Content, nil)
		if err == nil && maskingResult != nil {
			resp.Content = maskingResult.MaskedContent.(string)
		}
	}

	stateRecorder.RecordTransition(trace.ExecutionState, domain.StateValidation)
	stateRecorder.RecordTransition(domain.StateValidation, domain.StateFinalized)
	trace.ExecutionState = domain.StateFinalized
	trace.Output = domain.Output{
		Response:     &resp.Content,
		Truncated:    false,
		FinishReason: domain.FinishReason(resp.FinishReason),
	}
	trace.Observations = domain.Observations{
		TokensCount:      resp.TokensUsed,
		CostEstimatedUSD: resp.CostUSD,
		LatencyMs:        int(time.Since(trace.CreatedAt).Milliseconds()),
	}
	stateRecorder.ApplyToObservations(&trace.Observations)

	if sentorisHeaders.BudgetLimit > 0 {
		trace.ConstraintsApplied.BudgetLimitUSD = sentorisHeaders.BudgetLimit
		trace.ConstraintsApplied.BudgetActualUSD = resp.CostUSD
	}
	trace.ConstraintsApplied.PrivacyLevel = sentorisHeaders.PrivacyLevel

	if sentorisHeaders.BudgetLimit > 0 {
		var budgetRemaining *float64
		if h.budgetStore != nil && sessionID != "" {
			if remaining, err := h.budgetStore.GetRemaining(ctx, sessionID); err == nil {
				budgetRemaining = &remaining
			}
		}
		if budgetRemaining == nil {
			// 如果获取失败，使用计算的剩余
			rem := sentorisHeaders.BudgetLimit - resp.CostUSD
			budgetRemaining = &rem
		}
		acceptedCaps := sentorisHeaders.GetNegotiatedCapabilities([]string{"streaming", "tracing", "audit"})
		h.setResponseHeaders(w, traceID, resp.CostUSD, budgetRemaining, acceptedCaps)
	} else {
		acceptedCaps := sentorisHeaders.GetNegotiatedCapabilities([]string{"streaming", "tracing", "audit"})
		h.setResponseHeaders(w, traceID, resp.CostUSD, nil, acceptedCaps)
	}

	if chatReq.Stream {
		h.handleStreamingResponse(w, trace, resp, stateRecorder)
		return
	}

	// 构建OpenAI兼容的响应格式
	openAIResponse := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%s", traceID[:8]),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   resp.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": resp.Content,
				},
				"finish_reason": resp.FinishReason,
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     resp.TokensUsed / 2,
			"completion_tokens": resp.TokensUsed / 2,
			"total_tokens":      resp.TokensUsed,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openAIResponse); err != nil {
		logger.Error("Failed to encode response", "error", err)
	}

	if h.signer != nil {
		sig, err := h.signer.Sign(trace)
		if err == nil {
			trace.Proofs.AuditSignature = sig
		}
	}

	if h.hookChain != nil {
		if err := h.hookChain.PostStream(ctx, trace); err != nil {
			logger.Error("PostStream hook failed", "error", err)
		}
	}

	_ = h.traceStore.Update(ctx, trace)
}

func (h *Handler) handleVerifyTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
		return
	}

	traceID := r.URL.Query().Get("trace_id")
	if traceID == "" {
		h.handleError(w, "trace_id is required", http.StatusBadRequest, errors.ErrInvalidInput)
		return
	}

	trace, err := h.traceStore.Get(r.Context(), traceID)
	if err != nil {
		h.handleError(w, "Trace not found", http.StatusNotFound, errors.ErrBaselineNotFound)
		return
	}

	if trace.Proofs.AuditSignature == "" {
		h.handleError(w, "No audit signature found", http.StatusBadRequest, errors.ErrInvalidInput)
		return
	}

	valid, err := h.signer.Verify(trace)
	if err != nil {
		h.handleErrorWithMessage(w, "Verification failed", http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trace_id":    traceID,
		"valid":       valid,
		"verified_at": time.Now().UTC(),
	})
}

func (h *Handler) handleVerifyTraceByID(w http.ResponseWriter, r *http.Request, traceID string) {
	if r.Method != http.MethodGet {
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
		return
	}

	trace, err := h.traceStore.Get(r.Context(), traceID)
	if err != nil {
		h.handleError(w, "Trace not found", http.StatusNotFound, errors.ErrBaselineNotFound)
		return
	}

	if trace.Proofs.AuditSignature == "" {
		h.handleError(w, "No audit signature found", http.StatusBadRequest, errors.ErrInvalidInput)
		return
	}

	valid, err := h.signer.Verify(trace)
	if err != nil {
		h.handleErrorWithMessage(w, "Verification failed", http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trace_id":    traceID,
		"valid":       valid,
		"verified_at": time.Now().UTC(),
	})
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	providers := h.modelRouter.GetAllProviders()
	models := make([]map[string]interface{}, 0)

	for name, config := range providers {
		models = append(models, map[string]interface{}{
			"id":       name,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": name,
		})
		_ = config
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

func (h *Handler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

func (h *Handler) handleMonitor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	var requestCount int64 = 0
	if h.traceStore != nil {
		requestCount, _ = h.traceStore.Count(r.Context())
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "running",
		"request_count": requestCount,
	})
}

func (h *Handler) handleMonitorMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	var traceStats map[string]interface{} = map[string]interface{}{
		"total": 0,
		"by_state": map[string]int64{
			"FINALIZED": 0,
			"FAILED":    0,
		},
		"by_model":   map[string]int64{},
		"total_cost": 0.0,
	}

	if h.traceStore != nil {
		count, err := h.traceStore.Count(r.Context())
		if err == nil {
			recentTraces, err := h.traceStore.ListRecent(r.Context(), 1000)
			if err == nil {
				var totalCost float64 = 0
				byState := map[string]int64{
					"FINALIZED": 0,
					"FAILED":    0,
					"EXECUTING": 0,
				}
				byModel := map[string]int64{}
				for _, trace := range recentTraces {
					state := string(trace.ExecutionState)
					if state == "" {
						state = "FINALIZED"
					}
					byState[state]++
					if trace.Model != "" {
						byModel[trace.Model]++
					}
					totalCost += trace.Observations.CostEstimatedUSD
				}
				traceStats = map[string]interface{}{
					"total":      count,
					"by_state":   byState,
					"by_model":   byModel,
					"total_cost": totalCost,
				}
			}
		}
	}

	providerCount := 0
	if h.modelRouter != nil {
		providers := h.modelRouter.GetAllProviders()
		providerCount = len(providers)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"request_count": traceStats["total"],
		"uptime":        "0h",
		"trace": map[string]interface{}{
			"stats": traceStats,
		},
		"router": map[string]interface{}{
			"models_count":    providerCount,
			"providers_count": providerCount,
		},
	})
}

func (h *Handler) handleMonitorModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	modelsList := []map[string]interface{}{}
	if h.modelRouter != nil {
		providers := h.modelRouter.GetAllProviders()
		defaultProvider := h.modelRouter.GetDefaultProvider()
		for providerName, config := range providers {
			for _, modelName := range config.Models {
				isDefault := providerName == defaultProvider
				modelsList = append(modelsList, map[string]interface{}{
					"id":           modelName,
					"provider":     providerName,
					"is_default":   isDefault,
					"input_price":  config.Pricing.InputPer1K,
					"output_price": config.Pricing.OutputPer1K,
				})
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"models": modelsList,
	})
}

func (h *Handler) handleMonitorExtensions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	extensions := []map[string]interface{}{}
	if h.extensionRegistry != nil {
		registered := h.extensionRegistry.List()
		for _, ext := range registered {
			extensions = append(extensions, map[string]interface{}{
				"name":    ext.Title,
				"version": ext.Version,
				"enabled": true,
			})
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"extensions": extensions,
	})
}

func (h *Handler) handleMonitorTraces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	traceID := r.URL.Query().Get("trace_id")
	if traceID == "" {
		h.handleError(w, "trace_id is required", http.StatusBadRequest, errors.ErrInvalidInput)
		return
	}

	trace, err := h.traceStore.Get(r.Context(), traceID)
	if err != nil {
		h.handleError(w, "Trace not found", http.StatusNotFound, errors.ErrBaselineNotFound)
		return
	}

	json.NewEncoder(w).Encode(trace)
}

func (h *Handler) handleTracesList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	traces, err := h.traceStore.ListRecent(r.Context(), limit)
	if err != nil {
		h.handleErrorWithMessage(w, "Failed to list traces", http.StatusInternalServerError, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"traces": traces,
	})
}

func (h *Handler) handleMonitorBudget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	budgetInfo := map[string]interface{}{
		"total_budget":     0.0,
		"used_budget":      0.0,
		"remaining_budget": 0.0,
	}

	if h.budgetStore != nil {
		budget, err := h.budgetStore.Get(r.Context())
		if err == nil {
			budgetInfo = map[string]interface{}{
				"total_budget":     budget.TotalBudget,
				"used_budget":      budget.UsedBudget,
				"remaining_budget": budget.TotalBudget - budget.UsedBudget,
			}
		}
	}

	json.NewEncoder(w).Encode(budgetInfo)
}

func (h *Handler) handleMonitorRiskReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	traceID := r.URL.Query().Get("trace_id")
	if traceID != "" {
		report, err := h.riskReportStore.Get(r.Context(), traceID)
		if err != nil {
			h.handleError(w, "Risk report not found", http.StatusNotFound, errors.ErrBaselineNotFound)
			return
		}
		json.NewEncoder(w).Encode(report)
		return
	}

	reports, err := h.riskReportStore.List(r.Context(), 100)
	if err != nil {
		h.handleErrorWithMessage(w, "Failed to list risk reports", http.StatusInternalServerError, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"reports": reports,
	})
}

func (h *Handler) handleReplayEval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
		return
	}

	sentorisHeaders := ParseSentorisHeaders(r)

	var req struct {
		BaselineTraceID string         `json:"baseline_trace_id"`
		Model           string         `json:"model,omitempty"`
		FocusFields     []string       `json:"focus_fields,omitempty"`
		Params          map[string]any `json:"params,omitempty"`
		Reproducibility string         `json:"reproducibility,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
		return
	}

	baselineTrace, err := h.traceStore.Get(r.Context(), req.BaselineTraceID)
	if err != nil {
		h.handleError(w, "Baseline trace not found", http.StatusNotFound, errors.ErrBaselineNotFound)
		return
	}

	modelToUse := baselineTrace.Model
	if req.Model != "" {
		modelToUse = req.Model
	} else if sentorisHeaders.OverrideModel != "" {
		modelToUse = sentorisHeaders.OverrideModel
	}

	focusFields := req.FocusFields
	if len(focusFields) == 0 && len(sentorisHeaders.FocusFields) > 0 {
		focusFields = sentorisHeaders.FocusFields
	}

	var providerConfig *router.ProviderConfig
	providers := h.modelRouter.GetAllProviders()
	for _, config := range providers {
		for _, m := range config.Models {
			if m == modelToUse {
				providerConfig = config
				break
			}
		}
		if providerConfig != nil {
			break
		}
	}

	if providerConfig == nil {
		defaultProvider := h.modelRouter.GetDefaultProvider()
		if defaultProvider != "" {
			if config, ok := h.modelRouter.GetProvider(defaultProvider); ok {
				providerConfig = config
			}
		}
	}

	if providerConfig == nil {
		h.handleError(w, "Failed to get provider", http.StatusInternalServerError, errors.ErrInternalError)
		return
	}

	upstreamClient := NewUpstreamClient(providerConfig.BaseURL)

	chatReq := &upstream.ChatRequest{
		Model:    modelToUse,
		Messages: []upstream.Message{},
		Stream:   false,
		Params:   baselineTrace.Input.Params,
	}

	if req.Params != nil {
		chatReq.Params = req.Params
	}

	if baselineTrace.Input.Prompt != nil {
		prompt, ok := baselineTrace.Input.Prompt.([]upstream.Message)
		if ok {
			chatReq.Messages = prompt
		} else {
			chatReq.Messages = []upstream.Message{
				{Role: "user", Content: fmt.Sprintf("%v", baselineTrace.Input.Prompt)},
			}
		}
	}

	// 设置种子以确保可重现性
	reproducibility := req.Reproducibility
	if reproducibility == "" {
		reproducibility = "bounded"
	}
	var reproducibilityMode domain.ReproducibilityMode
	switch reproducibility {
	case "none":
		reproducibilityMode = domain.ReproducibilityNone
	case "bounded":
		reproducibilityMode = domain.ReproducibilityBounded
	case "strict":
		reproducibilityMode = domain.ReproducibilityStrict
	default:
		reproducibilityMode = domain.ReproducibilityBounded
	}
	if reproducibility != "none" {
		var seed int64 = 42 // 默认种子
		if baselineTrace.Input.Seed != nil {
			seed = *baselineTrace.Input.Seed
		}
		chatReq.Seed = &seed
	}

	resp, err := upstreamClient.Chat(r.Context(), chatReq)
	if err != nil {
		h.handleError(w, "Failed to execute LLM request", http.StatusInternalServerError, errors.ErrInternalError)
		return
	}

	candidateTraceID := generateTraceID()
	candidateSessionID := generateSessionID()
	candidateTrace := &domain.Trace{
		TraceID:        candidateTraceID,
		ParentID:       &baselineTrace.TraceID,
		SessionID:      &candidateSessionID,
		Model:          modelToUse,
		ExecutionState: domain.StateFinalized,
		Input: domain.Input{
			Prompt:   baselineTrace.Input.Prompt,
			Params:   chatReq.Params,
			Metadata: baselineTrace.Input.Metadata,
			Seed:     chatReq.Seed,
		},
		Output: domain.Output{
			Response:     &resp.Content,
			FinishReason: domain.FinishReason(resp.FinishReason),
		},
		Observations: domain.Observations{
			TokensCount:      resp.TokensUsed,
			CostEstimatedUSD: resp.CostUSD,
		},
		ConstraintsApplied: domain.ConstraintsApplied{
			ReproducibilityMode: reproducibilityMode,
		},
		CreatedAt: time.Now().UTC(),
	}
	candidateTrace.FillDefaults()

	if h.signer != nil {
		sig, err := h.signer.Sign(candidateTrace)
		if err == nil {
			candidateTrace.Proofs.AuditSignature = sig
		}
	}
	if err := h.traceStore.Save(r.Context(), candidateTrace); err != nil {
		logger.Error("Failed to save candidate trace", "error", err)
	}

	riskReport, err := h.differ.Compare(baselineTrace, candidateTrace)
	if err != nil {
		h.handleError(w, "Failed to compare traces", http.StatusInternalServerError, errors.ErrInternalError)
		return
	}

	// 设置焦点字段
	if len(focusFields) > 0 {
		riskReport.FocusFields = focusFields
	}

	if h.riskReportStore != nil {
		if err := h.riskReportStore.Save(r.Context(), riskReport); err != nil {
			logger.Error("Failed to save risk report", "error", err)
		}
	}

	h.setReplayResponseHeaders(w, candidateTraceID, baselineTrace.TraceID, modelToUse != baselineTrace.Model)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(riskReport)
}

func (h *Handler) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		providers := h.modelRouter.GetAllProviders()
		defaultProvider := h.modelRouter.GetDefaultProvider()
		providerList := make([]map[string]interface{}, 0)

		for name, config := range providers {
			isDefault := name == defaultProvider
			providerList = append(providerList, map[string]interface{}{
				"name":                name,
				"base_url":            config.BaseURL,
				"auth_header":         config.AuthHeader,
				"models":              config.Models,
				"input_price_per_1k":  config.Pricing.InputPer1K,
				"output_price_per_1k": config.Pricing.OutputPer1K,
				"is_default":          isDefault,
				"status":              "active",
			})
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"providers": providerList,
		})

	case http.MethodPost:
		var req struct {
			Name             string   `json:"name"`
			BaseURL          string   `json:"base_url"`
			AuthHeader       string   `json:"auth_header"`
			Models           []string `json:"models"`
			InputPricePer1K  float64  `json:"input_price_per_1k"`
			OutputPricePer1K float64  `json:"output_price_per_1k"`
			IsDefault        bool     `json:"is_default"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
			return
		}

		if req.Name == "" || req.BaseURL == "" {
			h.handleError(w, "Name and base_url are required", http.StatusBadRequest, errors.ErrInvalidInput)
			return
		}

		config := &router.ProviderConfig{
			BaseURL:    req.BaseURL,
			AuthHeader: req.AuthHeader,
			Models:     req.Models,
		}
		config.Pricing.InputPer1K = req.InputPricePer1K
		config.Pricing.OutputPer1K = req.OutputPricePer1K

		h.modelRouter.AddProvider(req.Name, config)

		if req.IsDefault {
			h.modelRouter.SetDefaultProvider(req.Name)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Provider added successfully",
		})

	case http.MethodPut:
		var req struct {
			Name             string   `json:"name"`
			BaseURL          string   `json:"base_url,omitempty"`
			AuthHeader       string   `json:"auth_header,omitempty"`
			Models           []string `json:"models,omitempty"`
			InputPricePer1K  float64  `json:"input_price_per_1k,omitempty"`
			OutputPricePer1K float64  `json:"output_price_per_1k,omitempty"`
			IsDefault        bool     `json:"is_default"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
			return
		}

		if req.Name == "" {
			h.handleError(w, "Name is required", http.StatusBadRequest, errors.ErrInvalidInput)
			return
		}

		if req.IsDefault {
			h.modelRouter.SetDefaultProvider(req.Name)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Default provider updated",
			})
			return
		}

		existingConfig, ok := h.modelRouter.GetProvider(req.Name)
		if !ok {
			h.handleError(w, "Provider not found", http.StatusNotFound, errors.ErrProviderNotFound)
			return
		}

		if req.BaseURL != "" {
			existingConfig.BaseURL = req.BaseURL
		}
		if req.AuthHeader != "" {
			existingConfig.AuthHeader = req.AuthHeader
		}
		if len(req.Models) > 0 {
			existingConfig.Models = req.Models
		}
		if req.InputPricePer1K > 0 {
			existingConfig.Pricing.InputPer1K = req.InputPricePer1K
		}
		if req.OutputPricePer1K > 0 {
			existingConfig.Pricing.OutputPer1K = req.OutputPricePer1K
		}

		h.modelRouter.AddProvider(req.Name, existingConfig)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Provider updated successfully",
		})

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			h.handleError(w, "Name is required", http.StatusBadRequest, errors.ErrInvalidInput)
			return
		}

		if err := h.modelRouter.RemoveProvider(name); err != nil {
			h.handleErrorWithMessage(w, "Failed to remove provider", http.StatusInternalServerError, err)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Provider removed successfully",
		})

	default:
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
	}
}

func (h *Handler) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
		return
	}

	if req.Name == "" {
		h.handleError(w, "Name is required", http.StatusBadRequest, errors.ErrInvalidInput)
		return
	}

	config, ok := h.modelRouter.GetProvider(req.Name)
	if !ok {
		h.handleError(w, "Provider not found", http.StatusNotFound, errors.ErrProviderNotFound)
		return
	}

	client := NewUpstreamClient(config.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testReq := &upstream.ChatRequest{
		Model: "test-model",
		Messages: []upstream.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	_, err := client.Chat(ctx, testReq)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Connection test failed: %v", err),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Connection test successful",
	})
}

func (h *Handler) handleSetDefaultProvider(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
		return
	}

	if req.Name == "" {
		h.handleError(w, "Name is required", http.StatusBadRequest, errors.ErrInvalidInput)
		return
	}

	config, ok := h.modelRouter.GetProvider(req.Name)
	if !ok {
		h.handleError(w, "Provider not found", http.StatusNotFound, errors.ErrProviderNotFound)
		return
	}

	h.modelRouter.SetDefaultProvider(req.Name)
	_ = config

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Default provider set successfully",
	})
}

func (h *Handler) handleAdminExtensions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.extensionRegistry == nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"extensions": []interface{}{},
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		extensions := h.extensionRegistry.List()
		extList := make([]map[string]interface{}, 0)
		for _, ext := range extensions {
			extList = append(extList, map[string]interface{}{
				"name":    ext.Title,
				"version": ext.Version,
				"enabled": true,
			})
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"extensions": extList,
		})

	default:
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
	}
}

func (h *Handler) handleAdminBudget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		budgetInfo := map[string]interface{}{
			"total_budget":     0.0,
			"used_budget":      0.0,
			"remaining_budget": 0.0,
		}

		if h.budgetStore != nil {
			budget, err := h.budgetStore.Get(r.Context())
			if err == nil {
				budgetInfo = map[string]interface{}{
					"total_budget":     budget.TotalBudget,
					"used_budget":      budget.UsedBudget,
					"remaining_budget": budget.TotalBudget - budget.UsedBudget,
				}
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(budgetInfo)

	case http.MethodPost:
		var req struct {
			TotalBudget float64 `json:"total_budget"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
			return
		}

		if h.budgetStore != nil {
			if err := h.budgetStore.Set(r.Context(), req.TotalBudget); err != nil {
				h.handleErrorWithMessage(w, "Failed to set budget", http.StatusInternalServerError, err)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Budget updated successfully",
		})

	default:
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
	}
}

func (h *Handler) handleAdminAPIKeys(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		keys := []map[string]interface{}{}
		if h.apiKeyStore != nil {
			storedKeys, err := h.apiKeyStore.List(r.Context())
			if err == nil {
				for _, key := range storedKeys {
					keys = append(keys, map[string]interface{}{
						"key_id":    key.KeyID,
						"name":      key.Name,
						"created":   key.CreatedAt,
						"is_active": key.IsActive,
					})
				}
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": keys,
		})

	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.handleErrorWithMessage(w, "Invalid request body", http.StatusBadRequest, err)
			return
		}

		if h.apiKeyStore != nil {
			_, err := h.apiKeyStore.Create(r.Context(), "", "", req.Name, "", nil, []string{"read", "write"})
			if err != nil {
				h.handleErrorWithMessage(w, "Failed to create API key", http.StatusInternalServerError, err)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "API key created successfully",
		})

	case http.MethodDelete:
		keyID := r.URL.Query().Get("key_id")
		if keyID == "" {
			h.handleError(w, "key_id is required", http.StatusBadRequest, errors.ErrInvalidInput)
			return
		}

		if h.apiKeyStore != nil {
			if err := h.apiKeyStore.Delete(r.Context(), keyID); err != nil {
				h.handleErrorWithMessage(w, "Failed to delete API key", http.StatusInternalServerError, err)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "API key deleted successfully",
		})

	default:
		h.handleError(w, "Method not allowed", http.StatusMethodNotAllowed, errors.ErrInvalidMethod)
	}
}

func (h *Handler) handleError(w http.ResponseWriter, message string, statusCode int, errCode errors.ErrorCode) {
	sentorisErr := errors.NewSentorisError(errCode, message)
	h.handleSentorisError(w, sentorisErr)
}

func (h *Handler) handleErrorWithMessage(w http.ResponseWriter, message string, statusCode int, err error) {
	var sentorisErr *errors.SentorisError

	if se, ok := err.(*errors.SentorisError); ok {
		sentorisErr = se
	} else if ec, ok := err.(errors.ErrorCode); ok {
		sentorisErr = errors.NewSentorisError(ec, message)
	} else {
		sentorisErr = errors.WrapError(err, errors.ErrInternalError, message)
	}

	h.handleSentorisError(w, sentorisErr)
}

// handleSentorisError handles errors using the standard SentorisError structure
func (h *Handler) handleSentorisError(w http.ResponseWriter, sentorisErr *errors.SentorisError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(sentorisErr.HTTPStatusCode())

	// Use SentorisError's ToMap() method for standard format
	json.NewEncoder(w).Encode(sentorisErr.ToMap())
}

func generateTraceID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, 16)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return "trace_" + string(result)
}

func generateSessionID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, 16)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return "sess_" + string(result)
}

// truncateErrorMessage truncates error message to reasonable length
func truncateErrorMessage(message string, maxLen int) string {
	if len(message) <= maxLen {
		return message
	}
	return message[:maxLen] + "..."
}

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, trace *domain.Trace, resp *upstream.ChatResponse, stateRecorder *StateTransitionRecorder) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Sentoris-Trace-ID", trace.TraceID)

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.handleError(w, "Streaming not supported", http.StatusInternalServerError, errors.ErrInternalError)
		return
	}

	// Send initial content chunk
	streamData := map[string]interface{}{
		"id":     trace.TraceID,
		"object": "chat.completion.chunk",
		"model":  trace.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"content": resp.Content,
				},
				"finish_reason": nil,
			},
		},
	}

	jsonBytes, _ := json.Marshal(streamData)
	fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
	flusher.Flush()

	// Send finish chunk
	finishData := map[string]interface{}{
		"id":     trace.TraceID,
		"object": "chat.completion.chunk",
		"model":  trace.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": resp.FinishReason,
			},
		},
	}

	finishBytes, _ := json.Marshal(finishData)
	fmt.Fprintf(w, "data: %s\n\n", string(finishBytes))
	flusher.Flush()

	// Send done marker
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h *Handler) handleSSEError(w http.ResponseWriter, code errors.ErrorCode, message string, traceID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Sentoris-Trace-ID", traceID)

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.handleError(w, "Streaming not supported", http.StatusInternalServerError, errors.ErrInternalError)
		return
	}

	// Truncate error message to reasonable length (1000 chars)
	truncatedMessage := truncateErrorMessage(message, 1000)

	// Standard Sentoris SSE error format
	errorData := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    string(code),
			"message": truncatedMessage,
		},
		"sentoris": map[string]interface{}{
			"trace_id": traceID,
			"version":  "1.0.0",
		},
	}

	jsonBytes, _ := json.Marshal(errorData)
	fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
	flusher.Flush()

	// Send done marker
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h *Handler) handleStreamError(w http.ResponseWriter, code errors.ErrorCode, message string, traceID string) {
	h.handleSSEError(w, code, message, traceID)
}
