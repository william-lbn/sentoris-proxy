package upstream

import (
	"context"
	"fmt"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/service/tokenizer"
)

type ModelPricing struct {
	InputPricePer1K  float64
	OutputPricePer1K float64
}

type LLMClient interface {
	ProviderName() string
	StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	GetPricing(model string) (float64, error)
	GetModel() string
}

type ChatRequest struct {
	Model    string
	Messages []Message
	Stream   bool
	Params   map[string]any
	Seed     *int64
}

type Message struct {
	Role    string
	Content string
}

type ChatResponse struct {
	Model        string
	Content      string
	FinishReason string
	TokensUsed   int
	CostUSD      float64
}

type StreamEvent struct {
	Content string
	Done    bool
	Error   error
	Usage   *Usage
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64
}

type OpenAIClient struct {
	baseURL   string
	apiKey    string
	model     string
	pricing   map[string]ModelPricing
	tokenizer *tokenizer.OpenAITokenizer
}

func NewOpenAIClient(baseURL, apiKey, defaultModel string) *OpenAIClient {
	tok, err := tokenizer.NewOpenAITokenizer(defaultModel)
	if err != nil {
		// 如果创建tokenizer失败，使用默认tokenizer
		tok, _ = tokenizer.DefaultTokenizer()
	}

	return &OpenAIClient{
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     defaultModel,
		tokenizer: tok,
		pricing: map[string]ModelPricing{
			"gpt-4o":        {InputPricePer1K: 0.005, OutputPricePer1K: 0.015},
			"gpt-4o-mini":   {InputPricePer1K: 0.00015, OutputPricePer1K: 0.0006},
			"gpt-4-turbo":   {InputPricePer1K: 0.01, OutputPricePer1K: 0.03},
			"gpt-3.5-turbo": {InputPricePer1K: 0.0005, OutputPricePer1K: 0.0015},
		},
	}
}

func (c *OpenAIClient) ProviderName() string {
	return "openai"
}

func (c *OpenAIClient) GetModel() string {
	return c.model
}

func (c *OpenAIClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	// 使用tokenizer计算token数量
	tokensUsed, err := c.tokenizer.CountTokens(lastMsg.Content)
	if err != nil {
		// 如果tokenizer失败，回退到字符数/4的估算
		tokensUsed = len(lastMsg.Content) / 4
	}
	pricing, _ := c.GetPricing(req.Model)
	costUSD := float64(tokensUsed) * pricing
	return &ChatResponse{
		Model:        req.Model,
		Content:      fmt.Sprintf("Echo: %s", lastMsg.Content),
		FinishReason: "stop",
		TokensUsed:   tokensUsed,
		CostUSD:      costUSD,
	}, nil
}

func (c *OpenAIClient) StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	ch := make(chan StreamEvent, 10)

	go func() {
		defer close(ch)

		lastMsg := req.Messages[len(req.Messages)-1]
		words := splitIntoWords(lastMsg.Content)

		totalTokens := 0
		for i, word := range words {
			event := StreamEvent{
				Content: word,
				Done:    false,
			}

			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}

			// 使用tokenizer计算token数量
			tokenCount, err := c.tokenizer.CountTokens(word)
			if err != nil {
				// 如果tokenizer失败，回退到字符数/4的估算
				tokenCount = len(word) / 4
			}
			totalTokens += tokenCount

			if i == len(words)-1 {
				pricing, _ := c.GetPricing(req.Model)
				event.Done = true
				event.Usage = &Usage{
					TotalTokens: totalTokens,
					CostUSD:     pricing * float64(totalTokens),
				}
				ch <- event
			}
		}
	}()

	return ch, nil
}

func (c *OpenAIClient) GetPricing(model string) (float64, error) {
	if p, ok := c.pricing[model]; ok {
		return (p.InputPricePer1K + p.OutputPricePer1K) / 2 / 1000, nil
	}
	return 0.00001, nil
}

func splitIntoWords(s string) []string {
	if s == "" {
		return []string{}
	}

	result := []string{}
	word := ""
	for _, c := range s {
		if c == ' ' || c == '\n' {
			if word != "" {
				result = append(result, word)
				word = ""
			}
			if c == '\n' {
				result = append(result, "\n")
			}
		} else {
			word += string(c)
		}
	}
	if word != "" {
		result = append(result, word)
	}

	if len(result) == 0 {
		return []string{s}
	}

	return result
}

type ClientFactory struct {
	clients map[string]LLMClient
}

func NewClientFactory() *ClientFactory {
	return &ClientFactory{
		clients: make(map[string]LLMClient),
	}
}

func (f *ClientFactory) Register(name string, client LLMClient) {
	f.clients[name] = client
}

func (f *ClientFactory) Get(name string) (LLMClient, bool) {
	client, ok := f.clients[name]
	return client, ok
}

func (f *ClientFactory) CreateOpenAI(baseURL, apiKey, model string) *OpenAIClient {
	client := NewOpenAIClient(baseURL, apiKey, model)
	f.Register("openai", client)
	return client
}

// CreateCircuitBreakerClient 创建一个带有熔断和重试机制的客户端
func (f *ClientFactory) CreateCircuitBreakerClient(client LLMClient, maxRetries int, retryDelay time.Duration) *CircuitBreakerClient {
	cbClient := NewCircuitBreakerClient(client, maxRetries, retryDelay)
	f.Register(client.ProviderName()+"-circuit-breaker", cbClient)
	return cbClient
}

func (f *ClientFactory) CreateRealClient(provider, baseURL, apiKey, model string) *RealUpstreamClient {
	client := NewRealUpstreamClient(provider, baseURL, apiKey, model)
	f.Register(provider, client)
	return client
}

func (f *ClientFactory) CreateOpenAIClient(baseURL, apiKey, model string) *RealUpstreamClient {
	return f.CreateRealClient("openai", baseURL, "Bearer "+apiKey, model)
}

func (f *ClientFactory) CreateDeepSeekClient(baseURL, apiKey, model string) *RealUpstreamClient {
	return f.CreateRealClient("deepseek", baseURL, "Bearer "+apiKey, model)
}

func (f *ClientFactory) CreateQwenClient(baseURL, apiKey, model string) *RealUpstreamClient {
	return f.CreateRealClient("qwen", baseURL, "Bearer "+apiKey, model)
}
