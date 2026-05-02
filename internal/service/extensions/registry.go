package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

// ExtensionHandler 定义了扩展处理器接口
type ExtensionHandler interface {
	// Validate 解析并验证扩展参数
	Validate(params json.RawMessage) error
	
	// OnConstraintEval 在约束评估阶段执行
	OnConstraintEval(ctx context.Context, trace *domain.Trace, params json.RawMessage) error
	
	// OnExecuting 在执行阶段执行
	OnExecuting(ctx context.Context, trace *domain.Trace, params json.RawMessage) error
	
	// Close 释放资源
	Close() error
}

// MaintainerInfo 维护者信息
type MaintainerInfo struct {
	Name        string `json:"name"`
	ContactURI  string `json:"contact_uri"`
	GitHubOrg   string `json:"github_org,omitempty"`
}

// ExtensionRegistryEntry 扩展注册表条目，符合 schema/v1/extension-registry-schema.json
type ExtensionRegistryEntry struct {
	Namespace           string              `json:"namespace"`
	Version             string              `json:"version"`
	Title               string              `json:"title"`
	Status              string              `json:"status"`
	Maintainer          MaintainerInfo      `json:"maintainer"`
	SpecificationURI    string              `json:"specification_uri"`
	MinProtocolVersion  string              `json:"min_protocol_version,omitempty"`
	Dependencies        []string            `json:"dependencies,omitempty"`
	ConflictsWith       []string            `json:"conflicts_with,omitempty"`
	CapabilitiesRequired []string           `json:"capabilities_required,omitempty"`
	SchemaRef           string              `json:"schema_ref,omitempty"`
	Tags                []string            `json:"tags,omitempty"`
	CreatedAt           string              `json:"created_at,omitempty"`
	UpdatedAt           string              `json:"updated_at,omitempty"`
	HandlerClass        string              `json:"handler_class,omitempty"`
	Handler             ExtensionHandler    `json:"-"`
}

// IsValidNamespace 验证命名空间格式是否符合 sentoris.ai/v{major}/{feature} 格式
func IsValidNamespace(namespace string) bool {
	pattern := `^sentoris\.ai/v[0-9]+/[a-z0-9_-]+$`
	match, _ := regexp.MatchString(pattern, namespace)
	return match
}

// IsValidVersion 验证版本号是否符合语义化版本格式
func IsValidVersion(version string) bool {
	pattern := `^[0-9]+\.[0-9]+\.[0-9]+$`
	match, _ := regexp.MatchString(pattern, version)
	return match
}

// Validate 验证注册表条目是否符合 schema
func (e *ExtensionRegistryEntry) Validate() error {
	if !IsValidNamespace(e.Namespace) {
		return fmt.Errorf("invalid namespace format: %s", e.Namespace)
	}
	if !IsValidVersion(e.Version) {
		return fmt.Errorf("invalid version format: %s", e.Version)
	}
	if e.Title == "" {
		return fmt.Errorf("title is required")
	}
	if e.Status == "" {
		return fmt.Errorf("status is required")
	}
	if e.Maintainer.Name == "" {
		return fmt.Errorf("maintainer.name is required")
	}
	if e.Maintainer.ContactURI == "" {
		return fmt.Errorf("maintainer.contact_uri is required")
	}
	if e.SpecificationURI == "" {
		return fmt.Errorf("specification_uri is required")
	}
	return nil
}

// ExtensionRegistry 管理扩展的注册和发现（带持久化支持）
type ExtensionRegistry struct {
	entries    map[string]*ExtensionRegistryEntry
	persistent bool
}

// NewExtensionRegistry 创建一个新的扩展注册表
func NewExtensionRegistry() *ExtensionRegistry {
	return &ExtensionRegistry{
		entries: make(map[string]*ExtensionRegistryEntry),
	}
}

// Register 注册一个扩展
func (r *ExtensionRegistry) Register(entry *ExtensionRegistryEntry) error {
	if err := entry.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	
	// 检查冲突
	for _, conflict := range entry.ConflictsWith {
		if _, exists := r.entries[conflict]; exists {
			return fmt.Errorf("extension %s conflicts with already registered extension %s", entry.Namespace, conflict)
		}
	}
	
	// 检查依赖
	for _, dep := range entry.Dependencies {
		if _, exists := r.entries[dep]; !exists {
			return fmt.Errorf("extension %s requires %s which is not registered", entry.Namespace, dep)
		}
	}
	
	r.entries[entry.Namespace] = entry
	return nil
}

// Get 获取指定命名空间的扩展
func (r *ExtensionRegistry) Get(namespace string) (*ExtensionRegistryEntry, bool) {
	entry, ok := r.entries[namespace]
	return entry, ok
}

// List 获取所有扩展
func (r *ExtensionRegistry) List() []*ExtensionRegistryEntry {
	entries := make([]*ExtensionRegistryEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	return entries
}

// Remove 移除指定命名空间的扩展
func (r *ExtensionRegistry) Remove(namespace string) error {
	if _, ok := r.entries[namespace]; !ok {
		return fmt.Errorf("extension %s not found", namespace)
	}
	
	// 检查是否有其他扩展依赖此扩展
	for _, entry := range r.entries {
		for _, dep := range entry.Dependencies {
			if dep == namespace {
				return fmt.Errorf("cannot remove %s: it is required by %s", namespace, entry.Namespace)
			}
		}
	}
	
	delete(r.entries, namespace)
	return nil
}

// Close 关闭所有扩展
func (r *ExtensionRegistry) Close() error {
	for _, entry := range r.entries {
		if entry.Handler != nil {
			if err := entry.Handler.Close(); err != nil {
				return fmt.Errorf("failed to close extension %s: %w", entry.Namespace, err)
			}
		}
	}
	return nil
}

// MarshalJSON 序列化注册表
func (r *ExtensionRegistry) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.entries)
}

// UnmarshalJSON 反序列化注册表
func (r *ExtensionRegistry) UnmarshalJSON(data []byte) error {
	var entries map[string]*ExtensionRegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	r.entries = entries
	return nil
}

// LoadFromConfig 从配置数据加载扩展注册表
func (r *ExtensionRegistry) LoadFromConfig(configData []byte) error {
	var entries []*ExtensionRegistryEntry
	if err := json.Unmarshal(configData, &entries); err != nil {
		return fmt.Errorf("failed to unmarshal extension registry config: %w", err)
	}
	
	for _, entry := range entries {
		if err := r.Register(entry); err != nil {
			return fmt.Errorf("failed to register extension %s: %w", entry.Namespace, err)
		}
	}
	
	return nil
}

// GetCapabilities 获取所有已注册扩展需要的能力列表
func (r *ExtensionRegistry) GetCapabilities() []string {
	capabilities := make([]string, 0)
	seen := make(map[string]bool)
	
	for _, entry := range r.entries {
		for _, cap := range entry.CapabilitiesRequired {
			if !seen[cap] {
				capabilities = append(capabilities, cap)
				seen[cap] = true
			}
		}
	}
	
	return capabilities
}

// FilterByCapability 根据能力过滤扩展
func (r *ExtensionRegistry) FilterByCapability(capability string) []*ExtensionRegistryEntry {
	var result []*ExtensionRegistryEntry
	for _, entry := range r.entries {
		for _, cap := range entry.CapabilitiesRequired {
			if cap == capability {
				result = append(result, entry)
				break
			}
		}
	}
	return result
}

// GetCompatibleExtensions 获取与指定协议版本兼容的扩展
func (r *ExtensionRegistry) GetCompatibleExtensions(protocolVersion string) []*ExtensionRegistryEntry {
	var result []*ExtensionRegistryEntry
	for _, entry := range r.entries {
		minVersion := entry.MinProtocolVersion
		if minVersion == "" {
			minVersion = "1.0.0"
		}
		if compareVersions(protocolVersion, minVersion) >= 0 {
			result = append(result, entry)
		}
	}
	return result
}

// compareVersions 比较两个版本字符串
func compareVersions(v1, v2 string) int {
	var major1, minor1, patch1, major2, minor2, patch2 int
	
	fmt.Sscanf(v1, "%d.%d.%d", &major1, &minor1, &patch1)
	fmt.Sscanf(v2, "%d.%d.%d", &major2, &minor2, &patch2)
	
	if major1 != major2 {
		return major1 - major2
	}
	if minor1 != minor2 {
		return minor1 - minor2
	}
	return patch1 - patch2
}

// GetExtensionHandler 获取扩展处理器
func (r *ExtensionRegistry) GetExtensionHandler(namespace string) (ExtensionHandler, bool) {
	entry, ok := r.entries[namespace]
	if !ok || entry.Handler == nil {
		return nil, false
	}
	return entry.Handler, true
}

// ExecuteExtensionOnConstraintEval 在约束评估阶段执行扩展
func (r *ExtensionRegistry) ExecuteExtensionOnConstraintEval(ctx context.Context, trace *domain.Trace, namespace string, params json.RawMessage) error {
	entry, ok := r.entries[namespace]
	if !ok {
		return fmt.Errorf("extension %s not found", namespace)
	}
	if entry.Handler == nil {
		return fmt.Errorf("extension %s has no handler", namespace)
	}
	return entry.Handler.OnConstraintEval(ctx, trace, params)
}

// ExecuteExtensionOnExecuting 在执行阶段执行扩展
func (r *ExtensionRegistry) ExecuteExtensionOnExecuting(ctx context.Context, trace *domain.Trace, namespace string, params json.RawMessage) error {
	entry, ok := r.entries[namespace]
	if !ok {
		return fmt.Errorf("extension %s not found", namespace)
	}
	if entry.Handler == nil {
		return fmt.Errorf("extension %s has no handler", namespace)
	}
	return entry.Handler.OnExecuting(ctx, trace, params)
}

// MemoryFirewallExtension 内存防火墙扩展
type MemoryFirewallExtension struct{}

// Validate 验证参数
func (e *MemoryFirewallExtension) Validate(params json.RawMessage) error {
	return nil
}

// OnConstraintEval 在约束评估阶段执行
func (e *MemoryFirewallExtension) OnConstraintEval(ctx context.Context, trace *domain.Trace, params json.RawMessage) error {
	return nil
}

// OnExecuting 在执行阶段执行
func (e *MemoryFirewallExtension) OnExecuting(ctx context.Context, trace *domain.Trace, params json.RawMessage) error {
	return nil
}

// Close 释放资源
func (e *MemoryFirewallExtension) Close() error {
	return nil
}

// NewMemoryFirewallExtension 创建一个内存防火墙扩展
func NewMemoryFirewallExtension() *MemoryFirewallExtension {
	return &MemoryFirewallExtension{}
}

// CustomRuleExtension 自定义规则扩展
type CustomRuleExtension struct{}

// Validate 验证参数
func (e *CustomRuleExtension) Validate(params json.RawMessage) error {
	return nil
}

// OnConstraintEval 在约束评估阶段执行
func (e *CustomRuleExtension) OnConstraintEval(ctx context.Context, trace *domain.Trace, params json.RawMessage) error {
	return nil
}

// OnExecuting 在执行阶段执行
func (e *CustomRuleExtension) OnExecuting(ctx context.Context, trace *domain.Trace, params json.RawMessage) error {
	return nil
}

// Close 释放资源
func (e *CustomRuleExtension) Close() error {
	return nil
}

// NewCustomRuleExtension 创建一个自定义规则扩展
func NewCustomRuleExtension() *CustomRuleExtension {
	return &CustomRuleExtension{}
}