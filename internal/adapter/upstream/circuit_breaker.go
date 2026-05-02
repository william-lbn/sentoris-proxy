package upstream

import (
	"context"
	"fmt"
	"time"

	"github.com/sony/gobreaker"
)

// CircuitBreakerClient 是一个带有熔断和重试机制的客户端包装器
type CircuitBreakerClient struct {
	client     LLMClient
	breaker    *gobreaker.CircuitBreaker
	maxRetries int
	retryDelay time.Duration
}

// NewCircuitBreakerClient 创建一个新的熔断和重试客户端
func NewCircuitBreakerClient(client LLMClient, maxRetries int, retryDelay time.Duration) *CircuitBreakerClient {
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "upstream-" + client.ProviderName(),
		MaxRequests: 5,
		Interval:    time.Minute,
		Timeout:     time.Minute * 5,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			fmt.Printf("Circuit breaker %s changed from %s to %s\n", name, from, to)
		},
	})

	return &CircuitBreakerClient{
		client:     client,
		breaker:    cb,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}
}

// ProviderName 返回客户端的提供商名称
func (c *CircuitBreakerClient) ProviderName() string {
	return c.client.ProviderName()
}

// GetModel 返回客户端的模型名称
func (c *CircuitBreakerClient) GetModel() string {
	return c.client.GetModel()
}

// GetPricing 获取模型的定价
func (c *CircuitBreakerClient) GetPricing(model string) (float64, error) {
	var result float64
	var err error

	retry := 0
	for retry <= c.maxRetries {
		result, err = c.client.GetPricing(model)
		if err == nil {
			return result, nil
		}

		retry++
		if retry <= c.maxRetries {
			time.Sleep(c.retryDelay)
		}
	}

	return 0, fmt.Errorf("failed to get pricing after %d retries: %w", c.maxRetries, err)
}

// Chat 调用客户端的Chat方法，带有熔断和重试
func (c *CircuitBreakerClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	var err error

	retry := 0
	for retry <= c.maxRetries {
		// 使用熔断器
		value, cbErr := c.breaker.Execute(func() (interface{}, error) {
			resp, err := c.client.Chat(ctx, req)
			if err != nil {
				return nil, err
			}
			return resp, nil
		})

		if cbErr == nil {
			if resp, ok := value.(*ChatResponse); ok {
				return resp, nil
			}
		}

		err = cbErr
		retry++
		if retry <= c.maxRetries {
			time.Sleep(c.retryDelay)
		}
	}

	return nil, fmt.Errorf("failed to chat after %d retries: %w", c.maxRetries, err)
}

// StreamChat 调用客户端的StreamChat方法，带有熔断和重试
func (c *CircuitBreakerClient) StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	var err error

	retry := 0
	for retry <= c.maxRetries {
		// 使用熔断器
		value, cbErr := c.breaker.Execute(func() (interface{}, error) {
			ch, err := c.client.StreamChat(ctx, req)
			if err != nil {
				return nil, err
			}
			return ch, nil
		})

		if cbErr == nil {
			if ch, ok := value.(<-chan StreamEvent); ok {
				return ch, nil
			}
		}

		err = cbErr
		retry++
		if retry <= c.maxRetries {
			time.Sleep(c.retryDelay)
		}
	}

	return nil, fmt.Errorf("failed to stream chat after %d retries: %w", c.maxRetries, err)
}
