package governance

import (
	"context"
	"fmt"
	"math"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/router"
)

// StreamBudgetInterceptor 实现基于内容长度估算的逐chunk预算扣除
type StreamBudgetInterceptor struct {
	budgetStore    storage.BudgetStore
	modelRouter    *router.ModelRouter
	budgetLimit    float64
	strategy       string
	model          string
	sessionID      string
	reservedAmount float64
	tokenEstimator *TokenEstimator
}

// TokenEstimator 估算token数
type TokenEstimator struct{}

// NewTokenEstimator 创建一个新的token估算器
func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{}
}

// EstimateTokens 基于内容长度估算token数
// 参考：1 token ≈ 4 个字符（英文），1 token ≈ 2 个字符（中文）
func (e *TokenEstimator) EstimateTokens(content string) int {
	// 简单估算：英文按4字符/token，中文按2字符/token
	chineseChars := 0
	englishChars := 0

	for _, r := range content {
		if r >= 0x4e00 && r <= 0x9fff {
			chineseChars++
		} else {
			englishChars++
		}
	}

	// 计算token数
	tokenCount := float64(chineseChars)/2 + float64(englishChars)/4
	return int(math.Ceil(tokenCount))
}

// NewStreamBudgetInterceptor 创建一个新的流式预算拦截器
func NewStreamBudgetInterceptor(budgetStore storage.BudgetStore, modelRouter *router.ModelRouter, budgetLimit float64, strategy string, model string, sessionID string) *StreamBudgetInterceptor {
	return &StreamBudgetInterceptor{
		budgetStore:    budgetStore,
		modelRouter:    modelRouter,
		budgetLimit:    budgetLimit,
		strategy:       strategy,
		model:          model,
		sessionID:      sessionID,
		reservedAmount: 0,
		tokenEstimator: NewTokenEstimator(),
	}
}

// Intercept 拦截并检查预算
func (i *StreamBudgetInterceptor) Intercept(ctx context.Context, content string) (bool, error) {
	// 估算token数
	tokenCount := i.tokenEstimator.EstimateTokens(content)

	// 估算成本
	cost, err := i.estimateCost(tokenCount)
	if err != nil {
		return false, err
	}

	// 检查预算
	if i.budgetLimit > 0 {
		// 使用原子操作预扣预算
		reserved, err := i.budgetStore.Reserve(ctx, i.sessionID, cost)
		if err != nil {
			return false, fmt.Errorf("failed to reserve budget: %w", err)
		}

		if !reserved {
			// 预算不足，根据策略处理
			return i.handleBudgetExceeded(ctx)
		}

		// 记录已预留的金额
		i.reservedAmount += cost
	}

	return true, nil
}

// estimateCost 估算成本
func (i *StreamBudgetInterceptor) estimateCost(tokenCount int) (float64, error) {
	// 获取模型定价
	inputPrice, outputPrice, err := i.modelRouter.GetPricing(i.model)
	if err != nil || inputPrice == 0 || outputPrice == 0 {
		// 如果获取定价失败或定价为0，使用默认值
		inputPrice = 0.002 // 默认 $0.002 per 1k tokens
		outputPrice = 0.002
	}

	// 计算成本（使用平均价格）
	avgPrice := (inputPrice + outputPrice) / 2
	return float64(tokenCount) * avgPrice / 1000, nil
}

// handleBudgetExceeded 处理预算超支
func (i *StreamBudgetInterceptor) handleBudgetExceeded(ctx context.Context) (bool, error) {
	switch i.strategy {
	case "hard_stop":
		// 硬停止策略，回滚已预留的预算
		if i.reservedAmount > 0 {
			_ = i.budgetStore.Rollback(ctx, i.sessionID, i.reservedAmount)
		}
		return false, fmt.Errorf("budget exceeded")
	case "degrade":
		// 降级策略：尝试切换到低成本模型
		if degradeModel, ok := i.modelRouter.GetDegradeModel(i.model); ok {
			// 记录模型切换
			// 注意：这里只是记录，实际的模型切换需要在调用层面处理
			i.model = degradeModel
		}
		return true, nil
	case "soft_alert":
		// 软告警策略
		return true, nil
	default:
		// 默认硬停止，回滚已预留的预算
		if i.reservedAmount > 0 {
			_ = i.budgetStore.Rollback(ctx, i.sessionID, i.reservedAmount)
		}
		return false, fmt.Errorf("budget exceeded")
	}
}

// Commit 提交预算扣减
func (i *StreamBudgetInterceptor) Commit(ctx context.Context) error {
	if i.reservedAmount > 0 {
		err := i.budgetStore.Commit(ctx, i.sessionID, i.reservedAmount)
		if err != nil {
			return fmt.Errorf("failed to commit budget: %w", err)
		}
		i.reservedAmount = 0
	}
	return nil
}

// Rollback 回滚预算扣减
func (i *StreamBudgetInterceptor) Rollback(ctx context.Context) error {
	if i.reservedAmount > 0 {
		err := i.budgetStore.Rollback(ctx, i.sessionID, i.reservedAmount)
		if err != nil {
			return fmt.Errorf("failed to rollback budget: %w", err)
		}
		i.reservedAmount = 0
	}
	return nil
}

// GetCurrentCost 获取当前成本
func (i *StreamBudgetInterceptor) GetCurrentCost() float64 {
	return i.reservedAmount
}
