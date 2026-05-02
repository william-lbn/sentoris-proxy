package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type RealUpstreamClient struct {
	baseURL    string
	authHeader string
	client     *http.Client
	provider   string
	model      string
	pricing    map[string]ModelPricing
}

func NewRealUpstreamClient(provider, baseURL, authHeader, model string) *RealUpstreamClient {
	client := &RealUpstreamClient{
		baseURL:    baseURL,
		authHeader: authHeader,
		provider:   provider,
		model:      model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		pricing: make(map[string]ModelPricing),
	}
	client.initPricing()
	return client
}

func (c *RealUpstreamClient) initPricing() {
	c.pricing["gpt-4o"] = ModelPricing{InputPricePer1K: 0.005, OutputPricePer1K: 0.015}
	c.pricing["gpt-4o-mini"] = ModelPricing{InputPricePer1K: 0.00015, OutputPricePer1K: 0.0006}
	c.pricing["gpt-4-turbo"] = ModelPricing{InputPricePer1K: 0.01, OutputPricePer1K: 0.03}
	c.pricing["gpt-3.5-turbo"] = ModelPricing{InputPricePer1K: 0.0005, OutputPricePer1K: 0.0015}
	c.pricing["deepseek-chat"] = ModelPricing{InputPricePer1K: 0.0001, OutputPricePer1K: 0.0001}
	c.pricing["qwen-max"] = ModelPricing{InputPricePer1K: 0.0003, OutputPricePer1K: 0.0003}
	c.pricing["qwen-plus"] = ModelPricing{InputPricePer1K: 0.0002, OutputPricePer1K: 0.0002}
	c.pricing["qwen-turbo"] = ModelPricing{InputPricePer1K: 0.0001, OutputPricePer1K: 0.0001}
}

func (c *RealUpstreamClient) ProviderName() string {
	return c.provider
}

func (c *RealUpstreamClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages cannot be empty")
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", c.authHeader)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned non-200 status: %d, body: %s", resp.StatusCode, string(body))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens     int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("upstream returned no choices")
	}

	usage := openAIResp.Usage
	pricing := c.pricing[req.Model]
	cost := float64(usage.PromptTokens)*pricing.InputPricePer1K/1000 +
		float64(usage.CompletionTokens)*pricing.OutputPricePer1K/1000

	return &ChatResponse{
		Model:        req.Model,
		Content:      openAIResp.Choices[0].Message.Content,
		FinishReason: openAIResp.Choices[0].FinishReason,
		TokensUsed:   usage.TotalTokens,
		CostUSD:      cost,
	}, nil
}

func (c *RealUpstreamClient) StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	// 使用带缓冲区的通道，避免阻塞
	outCh := make(chan StreamEvent, 100)

	reqBody, err := json.Marshal(req)
	if err != nil {
		close(outCh)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		close(outCh)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", c.authHeader)
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")
	httpReq.Header.Set("Connection", "keep-alive")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		close(outCh)
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		close(outCh)
		return nil, fmt.Errorf("upstream returned non-200 status: %d, body: %s", resp.StatusCode, string(body))
	}

	go func() {
		defer func() {
			resp.Body.Close()
			close(outCh)
		}()

		// 实现SSE解码和流式处理
		sseReader := NewSSEReader(resp.Body)
		buffer := make([]byte, 4096) // 更大的缓冲区，减少读取次数

		for {
			select {
			case <-ctx.Done():
				// 上下文取消，退出
				return
			default:
				// 设置读取超时，防止上游卡住
				if deadlineSetter, ok := resp.Body.(interface{ SetReadDeadline(time.Time) error }); ok {
					err := deadlineSetter.SetReadDeadline(time.Now().Add(30 * time.Second))
					if err != nil {
						// 发送错误事件
						select {
						case <-ctx.Done():
						case outCh <- StreamEvent{
							Content: "",
							Done:    true,
							Error:   fmt.Errorf("failed to set read deadline: %w", err),
						}:
						}
						return
					}
				}

				n, err := sseReader.Read(buffer)
				if err != nil {
					if err == io.EOF {
						// 正常结束
						return
					} else if ctx.Err() != nil {
						// 上下文取消，退出
						return
					} else {
						// 发送错误事件
						select {
						case <-ctx.Done():
						case outCh <- StreamEvent{
							Content: "",
							Done:    true,
							Error:   fmt.Errorf("failed to read stream: %w", err),
						}:
						}
						return
					}
				}

				if n > 0 {
					// 处理SSE事件
					event, err := sseReader.ParseEvent(buffer[:n])
					if err != nil {
						// 解析错误，发送警告但继续处理
						select {
						case <-ctx.Done():
						case outCh <- StreamEvent{
							Content: "",
							Done:    false,
							Error:   fmt.Errorf("failed to parse SSE event: %w", err),
						}:
						}
						continue
					}

					// 发送解析后的事件
					select {
					case <-ctx.Done():
						return
					case outCh <- event:
					}
				}
			}
		}
	}()

	return outCh, nil
}

func (c *RealUpstreamClient) GetPricing(model string) (float64, error) {
	if pricing, ok := c.pricing[model]; ok {
		return pricing.InputPricePer1K, nil
	}
	return 0.002, fmt.Errorf("pricing not found for model: %s", model)
}

func (c *RealUpstreamClient) GetModel() string {
	return c.model
}
