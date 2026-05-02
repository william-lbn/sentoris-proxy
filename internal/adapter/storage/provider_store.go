package storage

import (
	"context"
	"fmt"
	"sync"
)

// ProviderConfig 定义Provider配置结构
type ProviderConfig struct {
	Name             string   `json:"name"`
	BaseURL          string   `json:"base_url"`
	AuthHeader       string   `json:"auth_header"`
	Models           []string `json:"models"`
	InputPricePer1K  float64  `json:"input_price_per_1k"`
	OutputPricePer1K float64  `json:"output_price_per_1k"`
	IsDefault        bool     `json:"is_default"`
}

// ProviderStore 定义Provider存储接口
type ProviderStore interface {
	// Save 保存Provider配置
	Save(ctx context.Context, config *ProviderConfig) error
	// GetByName 根据名称获取Provider
	GetByName(ctx context.Context, name string) (*ProviderConfig, error)
	// List 获取所有Provider
	List(ctx context.Context) ([]*ProviderConfig, error)
	// Delete 删除Provider
	Delete(ctx context.Context, name string) error
	// GetDefault 获取默认Provider
	GetDefault(ctx context.Context) (*ProviderConfig, error)
	// SetDefault 设置默认Provider
	SetDefault(ctx context.Context, name string) error
}

// MemoryProviderStore 实现了基于内存的Provider存储
type MemoryProviderStore struct {
	providers map[string]*ProviderConfig
	mu        sync.RWMutex
}

// NewMemoryProviderStore 创建一个新的内存Provider存储
func NewMemoryProviderStore() *MemoryProviderStore {
	return &MemoryProviderStore{
		providers: make(map[string]*ProviderConfig),
	}
}

// Save 保存Provider配置
func (s *MemoryProviderStore) Save(ctx context.Context, config *ProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[config.Name] = config
	return nil
}

// GetByName 根据名称获取Provider
func (s *MemoryProviderStore) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	config, ok := s.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return config, nil
}

// List 获取所有Provider
func (s *MemoryProviderStore) List(ctx context.Context) ([]*ProviderConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*ProviderConfig
	for _, config := range s.providers {
		list = append(list, config)
	}
	return list, nil
}

// Delete 删除Provider
func (s *MemoryProviderStore) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.providers, name)
	return nil
}

// GetDefault 获取默认Provider
func (s *MemoryProviderStore) GetDefault(ctx context.Context) (*ProviderConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, config := range s.providers {
		if config.IsDefault {
			return config, nil
		}
	}
	return nil, fmt.Errorf("no default provider found")
}

// SetDefault 设置默认Provider
func (s *MemoryProviderStore) SetDefault(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 先清除所有默认标记
	for _, config := range s.providers {
		config.IsDefault = false
	}
	// 设置新的默认Provider
	config, ok := s.providers[name]
	if !ok {
		return fmt.Errorf("provider not found: %s", name)
	}
	config.IsDefault = true
	return nil
}
