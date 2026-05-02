package upstream

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type CircuitState string

const (
	CircuitStateClosed   CircuitState = "closed"
	CircuitStateOpen    CircuitState = "open"
	CircuitStateHalfOpen CircuitState = "half_open"
)

type FaultTolerantUpstreamClient struct {
	client     LLMClient
	breaker    *CircuitBreaker
	maxRetries int
}

func NewFaultTolerantUpstreamClient(client LLMClient) *FaultTolerantUpstreamClient {
	return &FaultTolerantUpstreamClient{
		client:     client,
		breaker:    NewCircuitBreaker(5, 30*time.Second),
		maxRetries: 3,
	}
}

func (c *FaultTolerantUpstreamClient) ProviderName() string {
	return c.client.ProviderName()
}

func (c *FaultTolerantUpstreamClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	result, err := c.breaker.Execute(func() (interface{}, error) {
		var lastErr error
		for i := 0; i < c.maxRetries; i++ {
			resp, err := c.client.Chat(ctx, req)
			if err == nil {
				return resp, nil
			}
			lastErr = err

			if isRetryable(err) {
				time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
				continue
			}
			break
		}
		return nil, lastErr
	})

	if err != nil {
		return nil, err
	}

	return result.(*ChatResponse), nil
}

func (c *FaultTolerantUpstreamClient) StreamChat(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	result, err := c.breaker.Execute(func() (interface{}, error) {
		ch, err := c.client.StreamChat(ctx, req)
		if err != nil {
			return nil, err
		}

		outCh := make(chan StreamEvent)

		go func() {
			defer close(outCh)

			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-ch:
					if !ok {
						return
					}
					select {
					case outCh <- event:
					case <-ctx.Done():
						return
					}
				}
			}
		}()

		return outCh, nil
	})

	if err != nil {
		return nil, err
	}

	return result.(chan StreamEvent), nil
}

func (c *FaultTolerantUpstreamClient) GetPricing(model string) (float64, error) {
	return c.client.GetPricing(model)
}

func (c *FaultTolerantUpstreamClient) GetModel() string {
	return c.client.GetModel()
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		contains(msg, "timeout") ||
		contains(msg, "temporary") ||
		contains(msg, "connection reset")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

type CircuitBreaker struct {
	failureThreshold int
	timeout         time.Duration
	state           CircuitState
	failureCount    int
	lastFailure     time.Time
}

func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: threshold,
		timeout:          timeout,
		state:            CircuitStateClosed,
	}
}

func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	if cb.state == CircuitStateOpen {
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.state = CircuitStateHalfOpen
		} else {
			return nil, fmt.Errorf("circuit breaker is open")
		}
	}

	result, err := fn()

	if err != nil {
		cb.failureCount++
		cb.lastFailure = time.Now()
		if cb.failureCount >= cb.failureThreshold {
			cb.state = CircuitStateOpen
		}
		return result, err
	}

	if cb.state == CircuitStateHalfOpen {
		cb.state = CircuitStateClosed
	}
	cb.failureCount = 0

	return result, nil
}
