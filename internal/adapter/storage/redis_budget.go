package storage

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/redis/go-redis/v9"
)

// RedisBudgetStore 实现了基于Redis的预算存储
type RedisBudgetStore struct {
	client redis.UniversalClient
}

// NewRedisBudgetStore 创建一个新的Redis预算存储
func NewRedisBudgetStore(addr, password string, db int, mode, master string, nodes []string) *RedisBudgetStore {
	var client redis.UniversalClient

	switch mode {
	case "sentinel":
		// Sentinel模式
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    master,
			SentinelAddrs: nodes,
			Password:      password,
			DB:            db,
		})
	case "cluster":
		// Cluster模式
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    nodes,
			Password: password,
		})
	default:
		// 单节点模式
		client = redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		})
	}

	return &RedisBudgetStore{
		client: client,
	}
}

// Close 关闭Redis连接
func (s *RedisBudgetStore) Close() error {
	return s.client.Close()
}

// Reserve 预留预算
func (s *RedisBudgetStore) Reserve(ctx context.Context, sessionID string, amount float64) (bool, error) {
	// 创建tracer
	tracer := otel.Tracer("redis-budget")

	// 创建span
	ctx, span := tracer.Start(ctx, "ReserveBudget")
	defer span.End()

	// 使用Lua脚本确保原子操作
	script := `
	local budget_key = KEYS[1]
	local reserved_key = KEYS[2]
	local amount = tonumber(ARGV[1])
	local budget = tonumber(redis.call('get', budget_key) or '0')
	local reserved = tonumber(redis.call('get', reserved_key) or '0')
	local available = budget - reserved
	if available >= amount then
		redis.call('incrbyfloat', reserved_key, amount)
		return 1
	else
		return 0
	end
	`

	result, err := s.client.Eval(ctx, script, []string{fmt.Sprintf("budget:%s", sessionID), fmt.Sprintf("budget:%s:reserved", sessionID)}, amount).Result()
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to reserve budget: %w", err)
	}

	return result == int64(1), nil
}

// Commit 提交预算扣减
func (s *RedisBudgetStore) Commit(ctx context.Context, sessionID string, amount float64) error {
	// 创建tracer
	tracer := otel.Tracer("redis-budget")

	// 创建span
	ctx, span := tracer.Start(ctx, "CommitBudget")
	defer span.End()

	// 从预留金额中减去实际消费，剩余的预留金额需要回滚
	script := `
	local reserved_key = KEYS[1]
	local used_key = KEYS[2]
	local amount = tonumber(ARGV[1])
	local reserved = tonumber(redis.call('get', reserved_key) or '0')
	local used = tonumber(redis.call('get', used_key) or '0')
	local to_rollback = reserved - amount
	if to_rollback > 0 then
		redis.call('decrbyfloat', reserved_key, to_rollback)
	end
	redis.call('incrbyfloat', used_key, amount)
	return 'OK'
	`

	_, err := s.client.Eval(ctx, script, []string{fmt.Sprintf("budget:%s:reserved", sessionID), fmt.Sprintf("budget:%s:used", sessionID)}, amount).Result()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to commit budget: %w", err)
	}

	return nil
}

// Rollback 回滚预算扣减
func (s *RedisBudgetStore) Rollback(ctx context.Context, sessionID string, amount float64) error {
	// 创建tracer
	tracer := otel.Tracer("redis-budget")

	// 创建span
	ctx, span := tracer.Start(ctx, "RollbackBudget")
	defer span.End()

	// 回滚预留金额
	script := `
	local reserved_key = KEYS[1]
	local amount = tonumber(ARGV[1])
	local reserved = tonumber(redis.call('get', reserved_key) or '0')
	local new_reserved = reserved - amount
	if new_reserved < 0 then
		new_reserved = 0
	end
	redis.call('set', reserved_key, new_reserved)
	return 'OK'
	`

	_, err := s.client.Eval(ctx, script, []string{fmt.Sprintf("budget:%s:reserved", sessionID)}, amount).Result()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to rollback budget: %w", err)
	}

	return nil
}

// GetRemaining 获取剩余预算
func (s *RedisBudgetStore) GetRemaining(ctx context.Context, sessionID string) (float64, error) {
	// 创建tracer
	tracer := otel.Tracer("redis-budget")

	// 创建span
	ctx, span := tracer.Start(ctx, "GetRemainingBudget")
	defer span.End()

	script := `
	local budget_key = KEYS[1]
	local reserved_key = KEYS[2]
	local used_key = KEYS[3]
	local budget = tonumber(redis.call('get', budget_key) or '0')
	local reserved = tonumber(redis.call('get', reserved_key) or '0')
	local used = tonumber(redis.call('get', used_key) or '0')
	return budget - reserved - used
	`

	result, err := s.client.Eval(ctx, script, []string{
		fmt.Sprintf("budget:%s", sessionID),
		fmt.Sprintf("budget:%s:reserved", sessionID),
		fmt.Sprintf("budget:%s:used", sessionID),
	}).Result()
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to get remaining budget: %w", err)
	}

	remaining, ok := result.(float64)
	if !ok {
		if resStr, ok := result.(string); ok {
			remaining, err = strconv.ParseFloat(resStr, 64)
			if err != nil {
				return 0, nil
			}
		} else {
			return 0, nil
		}
	}

	return remaining, nil
}

// SetBudget 设置预算
func (s *RedisBudgetStore) SetBudget(ctx context.Context, sessionID string, amount float64) error {
	// 创建tracer
	tracer := otel.Tracer("redis-budget")

	// 创建span
	ctx, span := tracer.Start(ctx, "SetBudget")
	defer span.End()

	script := `
	local budget_key = KEYS[1]
	local reserved_key = KEYS[2]
	local used_key = KEYS[3]
	local amount = tonumber(ARGV[1])
	redis.call('set', budget_key, amount)
	redis.call('del', reserved_key)
	redis.call('del', used_key)
	return 'OK'
	`

	_, err := s.client.Eval(ctx, script, []string{
		fmt.Sprintf("budget:%s", sessionID),
		fmt.Sprintf("budget:%s:reserved", sessionID),
		fmt.Sprintf("budget:%s:used", sessionID),
	}, amount).Result()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to set budget: %w", err)
	}

	return nil
}

func (s *RedisBudgetStore) Get(ctx context.Context) (*Budget, error) {
	tracer := otel.Tracer("redis-budget")
	ctx, span := tracer.Start(ctx, "GetBudget")
	defer span.End()

	key := "budget:global"
	result, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return &Budget{TotalBudget: 0, UsedBudget: 0}, nil
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get budget: %w", err)
	}

	value, err := strconv.ParseFloat(result, 64)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to parse budget: %w", err)
	}

	return &Budget{TotalBudget: value, UsedBudget: 0}, nil
}

func (s *RedisBudgetStore) Set(ctx context.Context, amount float64) error {
	tracer := otel.Tracer("redis-budget")
	ctx, span := tracer.Start(ctx, "SetBudgetGlobal")
	defer span.End()

	key := "budget:global"
	_, err := s.client.Set(ctx, key, amount, 24*time.Hour).Result()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to set budget: %w", err)
	}

	return nil
}
