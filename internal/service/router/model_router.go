package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/upstream"
)

// ProviderConfig 表示一个LLM提供商的配置
type ProviderConfig struct {
	BaseURL    string   `yaml:"base_url"`
	AuthHeader string   `yaml:"auth_header"`
	Models     []string `yaml:"models"`
	Pricing    struct {
		InputPer1K  float64 `yaml:"input_per_1k"`
		OutputPer1K float64 `yaml:"output_per_1k"`
	} `yaml:"pricing"`
}

// ModelRouter 用于根据模型名称路由到对应的上游客户端
type ModelRouter struct {
	providers        map[string]*ProviderConfig
	clients          map[string]upstream.LLMClient
	defaultProvider  string
	degradeMap       map[string]string // 模型降级映射
	providerStore    storage.ProviderStore
}

// NewModelRouter 创建一个新的模型路由器
func NewModelRouter() *ModelRouter {
	return &ModelRouter{
		providers:   make(map[string]*ProviderConfig),
		clients:     make(map[string]upstream.LLMClient),
		degradeMap:  make(map[string]string),
	}
}

// NewModelRouterWithStore 创建一个带持久化存储的模型路由器
func NewModelRouterWithStore(store storage.ProviderStore) *ModelRouter {
	return &ModelRouter{
		providers:     make(map[string]*ProviderConfig),
		clients:       make(map[string]upstream.LLMClient),
		degradeMap:    make(map[string]string),
		providerStore: store,
	}
}

// LoadFromStore 从持久化存储加载所有Provider配置
func (r *ModelRouter) LoadFromStore(ctx context.Context) error {
	if r.providerStore == nil {
		return fmt.Errorf("provider store not set")
	}

	providers, err := r.providerStore.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to load providers from store: %w", err)
	}

	for _, provider := range providers {
		config := &ProviderConfig{
			BaseURL:    provider.BaseURL,
			AuthHeader: provider.AuthHeader,
			Models:     provider.Models,
		}
		config.Pricing.InputPer1K = provider.InputPricePer1K
		config.Pricing.OutputPer1K = provider.OutputPricePer1K

		r.providers[provider.Name] = config
		
		// 创建对应的客户端
		realClient := upstream.NewRealUpstreamClient(provider.Name, provider.BaseURL, provider.AuthHeader, "")
		r.clients[provider.Name] = upstream.NewFaultTolerantUpstreamClient(realClient)

		if provider.IsDefault {
			r.defaultProvider = provider.Name
		}
	}

	return nil
}

// AddProvider 添加一个LLM提供商
func (r *ModelRouter) AddProvider(name string, config *ProviderConfig) {
	r.providers[name] = config
	// 创建对应的客户端
	realClient := upstream.NewRealUpstreamClient(name, config.BaseURL, config.AuthHeader, "")
	r.clients[name] = upstream.NewFaultTolerantUpstreamClient(realClient)

	// 如果有持久化存储，保存到数据库
	if r.providerStore != nil {
		providerConfig := &storage.ProviderConfig{
			Name:             name,
			BaseURL:          config.BaseURL,
			AuthHeader:       config.AuthHeader,
			Models:           config.Models,
			InputPricePer1K:  config.Pricing.InputPer1K,
			OutputPricePer1K: config.Pricing.OutputPer1K,
			IsDefault:        name == r.defaultProvider,
		}
		ctx := context.Background()
		r.providerStore.Save(ctx, providerConfig)
	}
}

// SetDefaultProvider 设置默认提供商
func (r *ModelRouter) SetDefaultProvider(name string) {
	oldDefault := r.defaultProvider
	r.defaultProvider = name

	// 如果有持久化存储，更新数据库
	if r.providerStore != nil {
		ctx := context.Background()
		// 先清除旧的默认标记
		if oldDefault != "" {
			provider, err := r.providerStore.GetByName(ctx, oldDefault)
			if err == nil {
				provider.IsDefault = false
				r.providerStore.Save(ctx, provider)
			}
		}
		// 设置新的默认标记
		provider, err := r.providerStore.GetByName(ctx, name)
		if err == nil {
			provider.IsDefault = true
			r.providerStore.Save(ctx, provider)
		}
	}
}

// GetClient 根据模型名称获取对应的上游客户端
func (r *ModelRouter) GetClient(model string) (upstream.LLMClient, error) {
	// 检查是否有直接匹配的模型
	for providerName, config := range r.providers {
		for _, m := range config.Models {
			if m == model {
				return r.clients[providerName], nil
			}
		}
	}

	// 检查模型名称是否包含提供商前缀
	parts := strings.Split(model, "/")
	if len(parts) == 2 {
		providerName := parts[0]
		if client, ok := r.clients[providerName]; ok {
			return client, nil
		}
	}

	return nil, fmt.Errorf("no provider found for model: %s", model)
}

// GetPricing 根据模型名称获取定价
func (r *ModelRouter) GetPricing(model string) (float64, float64, error) {
	// 检查是否有直接匹配的模型
	for _, config := range r.providers {
		for _, m := range config.Models {
			if m == model {
				return config.Pricing.InputPer1K, config.Pricing.OutputPer1K, nil
			}
		}
	}

	// 检查模型名称是否包含提供商前缀
	parts := strings.Split(model, "/")
	if len(parts) == 2 {
		providerName := parts[0]
		if config, ok := r.providers[providerName]; ok {
			return config.Pricing.InputPer1K, config.Pricing.OutputPer1K, nil
		}
	}

	// 使用默认提供商
	if r.defaultProvider != "" {
		if config, ok := r.providers[r.defaultProvider]; ok {
			return config.Pricing.InputPer1K, config.Pricing.OutputPer1K, nil
		}
	}

	// 返回默认定价
	return 0.001, 0.002, fmt.Errorf("no pricing found for model: %s, using default", model)
}

// AddDegradeMapping 添加模型降级映射
func (r *ModelRouter) AddDegradeMapping(highCostModel, lowCostModel string) {
	r.degradeMap[highCostModel] = lowCostModel
}

// GetDegradeModel 获取降级模型
func (r *ModelRouter) GetDegradeModel(model string) (string, bool) {
	degradeModel, ok := r.degradeMap[model]
	return degradeModel, ok
}

// GetAllProviders 获取所有提供商
func (r *ModelRouter) GetAllProviders() map[string]*ProviderConfig {
	return r.providers
}

// GetProvider 获取指定提供商
func (r *ModelRouter) GetProvider(name string) (*ProviderConfig, bool) {
	config, ok := r.providers[name]
	return config, ok
}

// GetDefaultProvider 获取默认提供商
func (r *ModelRouter) GetDefaultProvider() string {
	return r.defaultProvider
}

// GetAllModels 获取所有模型
func (r *ModelRouter) GetAllModels() []string {
	var models []string
	seen := make(map[string]bool)
	for _, config := range r.providers {
		for _, model := range config.Models {
			if !seen[model] {
				seen[model] = true
				models = append(models, model)
			}
		}
	}
	return models
}

// RemoveProvider 移除LLM提供商
func (r *ModelRouter) RemoveProvider(name string) error {
	if _, ok := r.providers[name]; !ok {
		return fmt.Errorf("provider not found: %s", name)
	}
	delete(r.providers, name)
	delete(r.clients, name)
	if r.defaultProvider == name {
		r.defaultProvider = ""
	}

	// 如果有持久化存储，从数据库删除
	if r.providerStore != nil {
		ctx := context.Background()
		r.providerStore.Delete(ctx, name)
	}

	return nil
}