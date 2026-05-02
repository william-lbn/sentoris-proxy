package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/sentoris-ai/sentoris-proxy/internal/service/router"
)

// Config 表示应用程序的配置
type Config struct {
	Port            int                               `yaml:"port"`
	StorageMode     string                            `yaml:"storage_mode"` // "postgres_redis", "sqlite", "memory"
	Upstreams       map[string]*router.ProviderConfig `yaml:"upstreams"`
	DefaultProvider string                            `yaml:"default_provider"`
	Budget          struct {
		BufferRatio float64           `yaml:"buffer_ratio"` // 预算缓冲比例
		Strategy    string            `yaml:"strategy"`     // 默认预算策略
		DegradeMap  map[string]string `yaml:"degrade_map"`  // 模型降级映射
	} `yaml:"budget"`
	Redis struct {
		Addr     string   `yaml:"addr"`
		Password string   `yaml:"password"`
		DB       int      `yaml:"db"`
		Mode     string   `yaml:"mode"`   // "single", "sentinel", "cluster"
		Master   string   `yaml:"master"` // 用于Sentinel模式
		Nodes    []string `yaml:"nodes"`  // 用于Cluster模式
	} `yaml:"redis"`
	PostgreSQL struct {
		DSN string `yaml:"dsn"`
	} `yaml:"postgresql"`
	SQLite struct {
		DatabasePath string `yaml:"database_path"`
	} `yaml:"sqlite"`
	FileCache struct {
		Enabled   bool   `yaml:"enabled"`
		CachePath string `yaml:"cache_path"`
		MaxSizeMB int    `yaml:"max_size_mb"`
	} `yaml:"file_cache"`
	JWT struct {
		Secret  string   `yaml:"secret"`
		APIKeys []string `yaml:"api_keys"`
	} `yaml:"jwt"`
	Hooks struct {
		Enabled []struct {
			Name     string `yaml:"name"`
			Priority int    `yaml:"priority"`
		} `yaml:"enabled"`
		Strategy string `yaml:"strategy"` // "short-circuit" 或 "all-execute"
	} `yaml:"hooks"`
	Extensions struct {
		Registered []struct {
			Namespace    string `yaml:"namespace"`
			Version      string `yaml:"version"`
			HandlerClass string `yaml:"handler_class"`
		} `yaml:"registered"`
		UnknownStrategy string `yaml:"unknown_strategy"` // "ignore", "warn", "reject"
	} `yaml:"extensions"`
	Observability struct {
		OpenTelemetry struct {
			Enabled  bool   `yaml:"enabled"`
			Endpoint string `yaml:"endpoint"`
		} `yaml:"opentelemetry"`
		Prometheus struct {
			Enabled bool   `yaml:"enabled"`
			Path    string `yaml:"path"`
		} `yaml:"prometheus"`
		LogLevel string `yaml:"log_level"` // 日志级别
	} `yaml:"observability"`
}

// ConfigManager 管理配置的加载和热更新
type ConfigManager struct {
	config      *Config
	path        string
	mu          sync.RWMutex
	lastModTime time.Time
}

// NewConfigManager 创建一个新的配置管理器
func NewConfigManager(path string) (*ConfigManager, error) {
	cm := &ConfigManager{
		path: path,
	}

	// 首次加载配置
	if err := cm.loadConfig(); err != nil {
		return nil, err
	}

	// 启动配置监听
	go cm.watchConfig()

	return cm, nil
}

// GetConfig 获取当前配置
func (cm *ConfigManager) GetConfig() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// loadConfig 加载配置
func (cm *ConfigManager) loadConfig() error {
	// 读取文件内容
	content, err := os.ReadFile(cm.path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// 解析YAML
	var config Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 替换环境变量
	replaceEnvVars(&config)

	// 设置默认值
	setDefaults(&config)

	// 更新配置
	cm.mu.Lock()
	cm.config = &config
	// 更新修改时间
	if info, err := os.Stat(cm.path); err == nil {
		cm.lastModTime = info.ModTime()
	}
	cm.mu.Unlock()

	fmt.Printf("Config loaded from %s\n", cm.path)
	return nil
}

// watchConfig 监听配置文件变化
func (cm *ConfigManager) watchConfig() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		if info, err := os.Stat(cm.path); err == nil {
			if info.ModTime().After(cm.lastModTime) {
				fmt.Printf("Config file changed, reloading...\n")
				if err := cm.loadConfig(); err != nil {
					fmt.Printf("Error reloading config: %v\n", err)
				}
			}
		}
	}
}

// LoadConfig 从文件加载配置
func LoadConfig(path string) (*Config, error) {
	// 读取文件内容
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 解析YAML
	var config Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 替换环境变量
	replaceEnvVars(&config)

	// 设置默认值
	setDefaults(&config)

	return &config, nil
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	config := &Config{}
	setDefaults(config)
	return config
}

// replaceEnvVars 替换配置中的环境变量
func replaceEnvVars(config *Config) {
	// 替换提供商配置中的环境变量
	for _, provider := range config.Upstreams {
		provider.AuthHeader = replaceEnvInString(provider.AuthHeader)
	}

	// 替换Redis配置中的环境变量
	config.Redis.Password = replaceEnvInString(config.Redis.Password)

	// 替换PostgreSQL配置中的环境变量
	config.PostgreSQL.DSN = replaceEnvInString(config.PostgreSQL.DSN)

	// 替换JWT配置中的环境变量
	config.JWT.Secret = replaceEnvInString(config.JWT.Secret)
}

// replaceEnvInString 替换字符串中的环境变量
func replaceEnvInString(s string) string {
	// 查找 ${VAR} 格式的环境变量
	for {
		start := strings.Index(s, "${")
		if start == -1 {
			break
		}

		end := strings.Index(s[start:], "}")
		if end == -1 {
			break
		}

		varName := s[start+2 : start+end]
		varValue := os.Getenv(varName)

		s = s[:start] + varValue + s[start+end+1:]
	}

	return s
}

// setDefaults 设置默认值
func setDefaults(config *Config) {
	if config.Port == 0 {
		config.Port = 8080
	}

	// 设置存储模式默认值
	if config.StorageMode == "" {
		config.StorageMode = "" // 空表示自动检测
	}

	// 设置预算配置默认值
	if config.Budget.BufferRatio == 0 {
		config.Budget.BufferRatio = 0.1 // 默认10%的缓冲
	}
	if config.Budget.Strategy == "" {
		config.Budget.Strategy = "hard_stop" // 默认硬停止策略
	}
	if config.Budget.DegradeMap == nil {
		config.Budget.DegradeMap = make(map[string]string)
		// 默认降级映射
		config.Budget.DegradeMap["gpt-4o"] = "gpt-4o-mini"
		config.Budget.DegradeMap["gpt-4-turbo"] = "gpt-3.5-turbo"
	}

	if config.Redis.Addr == "" {
		config.Redis.Addr = "localhost:6379"
	}

	if config.PostgreSQL.DSN == "" {
		config.PostgreSQL.DSN = "postgres://postgres:postgres@localhost:5432/sentoris?sslmode=disable"
	}

	// 设置SQLite默认路径
	if config.SQLite.DatabasePath == "" {
		config.SQLite.DatabasePath = "./data/sentoris.db"
	}

	// 设置文件缓存默认值
	if !config.FileCache.Enabled {
		config.FileCache.Enabled = false
	}
	if config.FileCache.CachePath == "" {
		config.FileCache.CachePath = "./data/cache/"
	}
	if config.FileCache.MaxSizeMB == 0 {
		config.FileCache.MaxSizeMB = 100
	}

	if config.JWT.Secret == "" {
		config.JWT.Secret = "default-secret-key"
	}

	// 设置钩子配置默认值
	if config.Hooks.Strategy == "" {
		config.Hooks.Strategy = "short-circuit"
	}

	// 设置扩展配置默认值
	if config.Extensions.UnknownStrategy == "" {
		config.Extensions.UnknownStrategy = "ignore"
	}

	// 设置可观测性默认值
	if !config.Observability.OpenTelemetry.Enabled {
		config.Observability.OpenTelemetry.Enabled = false
	}

	if config.Observability.Prometheus.Enabled {
		if config.Observability.Prometheus.Path == "" {
			config.Observability.Prometheus.Path = "/metrics"
		}
	}

	if config.Observability.LogLevel == "" {
		config.Observability.LogLevel = "info" // 默认info级别
	}
}

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	// 检查命令行参数
	// 这里可以添加命令行参数解析

	// 检查当前目录
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}

	// 检查config目录
	if _, err := os.Stat("config/config.yaml"); err == nil {
		return "config/config.yaml"
	}

	// 检查用户主目录
	home, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(home, ".sentoris", "config.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
	}

	// 返回默认路径
	return "config.yaml"
}
