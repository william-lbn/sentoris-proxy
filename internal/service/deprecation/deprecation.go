package deprecation

import (
	"fmt"
	"strings"
	"sync"
)

// DeprecationNotice 表示一个弃用通知
type DeprecationNotice struct {
	DeprecatedSince string // 弃用的版本
	RemovedIn       string // 将要移除的版本
	Replacement     string // 替代方案
	Description     string // 描述
}

// DeprecationManager 管理弃用通知
type DeprecationManager struct {
	notices map[string]*DeprecationNotice
	mu      sync.RWMutex
}

var (
	instance *DeprecationManager
	once     sync.Once
)

// GetDeprecationManager 获取弃用管理器单例
func GetDeprecationManager() *DeprecationManager {
	once.Do(func() {
		instance = &DeprecationManager{
			notices: make(map[string]*DeprecationNotice),
		}
	})
	return instance
}

// RegisterDeprecation 注册一个弃用通知
func (dm *DeprecationManager) RegisterDeprecation(field string, notice *DeprecationNotice) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.notices[field] = notice
}

// GetDeprecationNotice 获取指定字段的弃用通知
func (dm *DeprecationManager) GetDeprecationNotice(field string) *DeprecationNotice {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.notices[field]
}

// GetAllNotices 获取所有弃用通知
func (dm *DeprecationManager) GetAllNotices() map[string]*DeprecationNotice {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	result := make(map[string]*DeprecationNotice)
	for k, v := range dm.notices {
		result[k] = v
	}
	return result
}

// GenerateHeader 生成X-Sentoris-Deprecation-Notice头的值
func (dm *DeprecationManager) GenerateHeader() string {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	if len(dm.notices) == 0 {
		return ""
	}

	var notices []string
	for field, notice := range dm.notices {
		if notice.Replacement != "" {
			notices = append(notices, fmt.Sprintf(
				"Field \"%s\" is deprecated since %s, use \"%s\" instead. Will be removed in %s.",
				field, notice.DeprecatedSince, notice.Replacement, notice.RemovedIn,
			))
		} else {
			notices = append(notices, fmt.Sprintf(
				"Field \"%s\" is deprecated since %s. Will be removed in %s. %s",
				field, notice.DeprecatedSince, notice.RemovedIn, notice.Description,
			))
		}
	}

	return strings.Join(notices, " | ")
}

// InitializeDefaultNotices 初始化默认的弃用通知
func (dm *DeprecationManager) InitializeDefaultNotices() {
	dm.RegisterDeprecation("budget_limit", &DeprecationNotice{
		DeprecatedSince: "v1.1.0",
		RemovedIn:       "v2.0.0",
		Replacement:     "budget.limit_usd",
		Description:     "",
	})

	dm.RegisterDeprecation("privacy_level", &DeprecationNotice{
		DeprecatedSince: "v1.2.0",
		RemovedIn:       "v2.0.0",
		Replacement:     "privacy.level",
		Description:     "",
	})

	dm.RegisterDeprecation("reproducibility_mode", &DeprecationNotice{
		DeprecatedSince: "v1.2.0",
		RemovedIn:       "v2.0.0",
		Replacement:     "reproducibility.mode",
		Description:     "",
	})
}
