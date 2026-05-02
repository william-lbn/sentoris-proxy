package security

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/golang-jwt/jwt/v5"
)

// KeyPool 表示一个密钥池
type KeyPool struct {
	keys     []string
	mu       sync.RWMutex
	usage    map[string]int64
	provider string
}

// NewKeyPool 创建一个新的密钥池
func NewKeyPool(provider string) *KeyPool {
	return &KeyPool{
		keys:     make([]string, 0),
		usage:    make(map[string]int64),
		provider: provider,
	}
}

// AddKey 添加密钥到池
func (p *KeyPool) AddKey(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = append(p.keys, key)
	p.usage[key] = 0
}

// Next 获取下一个密钥（轮询策略）
func (p *KeyPool) Next() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.keys) == 0 {
		return "", errors.New("no keys in pool")
	}

	// 简单轮询
	minUsage := int64(^uint64(0) >> 1) // 最大值
	var selectedKey string

	for key, use := range p.usage {
		if use < minUsage {
			minUsage = use
			selectedKey = key
		}
	}

	if selectedKey == "" {
		selectedKey = p.keys[0]
	}

	p.usage[selectedKey]++
	return selectedKey, nil
}

// KeyManager 实现密钥管理
type KeyManager struct {
	privateKey    *rsa.PrivateKey
	publicKey     *rsa.PublicKey
	jwtSecret     []byte
	apiKeys       []string
	keyPools      map[string]*KeyPool
	mutex         sync.RWMutex
}

// NewKeyManager 创建一个新的密钥管理器
func NewKeyManager(jwtSecret string) *KeyManager {
	km := &KeyManager{
		jwtSecret: []byte(jwtSecret),
		apiKeys:   make([]string, 0),
		keyPools:  make(map[string]*KeyPool),
	}

	// 初始化默认密钥池
	km.keyPools["openai"] = NewKeyPool("openai")
	km.keyPools["deepseek"] = NewKeyPool("deepseek")
	km.keyPools["qwen"] = NewKeyPool("qwen")

	// 尝试加载RSA密钥对
	if err := km.loadRSAKeys(); err != nil {
		// 如果加载失败，生成新的密钥对
		if err := km.generateRSAKeys(); err != nil {
			fmt.Printf("Warning: Failed to generate RSA keys: %v\n", err)
		}
	}

	return km
}

// loadRSAKeys 加载RSA密钥对
func (km *KeyManager) loadRSAKeys() error {
	privateKeyPath := filepath.Join(os.TempDir(), "sentoris_private.pem")
	publicKeyPath := filepath.Join(os.TempDir(), "sentoris_public.pem")

	// 加载私钥
	privateKeyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return err
	}

	// 加载公钥
	publicKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return err
	}

	// 解析私钥
	block, _ := pem.Decode(privateKeyData)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return errors.New("failed to decode private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return err
	}

	// 解析公钥
	block, _ = pem.Decode(publicKeyData)
	if block == nil || block.Type != "RSA PUBLIC KEY" {
		return errors.New("failed to decode public key")
	}

	publicKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return err
	}

	km.mutex.Lock()
	defer km.mutex.Unlock()
	km.privateKey = privateKey
	km.publicKey = publicKey

	return nil
}

// generateRSAKeys 生成RSA密钥对
func (km *KeyManager) generateRSAKeys() error {
	// 这里简化实现，实际应使用更安全的密钥生成方法
	// 暂时使用JWT secret作为替代
	return nil
}

// GetPrivateKey 获取私钥
func (km *KeyManager) GetPrivateKey() *rsa.PrivateKey {
	km.mutex.RLock()
	defer km.mutex.RUnlock()
	return km.privateKey
}

// GetPublicKey 获取公钥
func (km *KeyManager) GetPublicKey() *rsa.PublicKey {
	km.mutex.RLock()
	defer km.mutex.RUnlock()
	return km.publicKey
}

// GenerateJWT 生成JWT令牌
func (km *KeyManager) GenerateJWT(claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(km.jwtSecret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// ValidateJWT 验证JWT令牌
func (km *KeyManager) ValidateJWT(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return km.jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// GenerateAuditKey 生成审计密钥
func (km *KeyManager) GenerateAuditKey() ([]byte, error) {
	// 简化实现，实际应使用更安全的密钥生成方法
	return km.jwtSecret, nil
}

// AddProviderKey 添加提供商密钥
func (km *KeyManager) AddProviderKey(provider, key string) {
	km.mutex.Lock()
	defer km.mutex.Unlock()

	if pool, ok := km.keyPools[provider]; ok {
		pool.AddKey(key)
	} else {
		pool = NewKeyPool(provider)
		pool.AddKey(key)
		km.keyPools[provider] = pool
	}
}

// GetProviderKey 获取提供商密钥
func (km *KeyManager) GetProviderKey(provider string) (string, error) {
	km.mutex.RLock()
	pool, ok := km.keyPools[provider]
	km.mutex.RUnlock()

	if !ok {
		return "", fmt.Errorf("provider not found: %s", provider)
	}

	return pool.Next()
}

// ListProviders 列出所有提供商
func (km *KeyManager) ListProviders() []string {
	km.mutex.RLock()
	defer km.mutex.RUnlock()

	providers := make([]string, 0, len(km.keyPools))
	for provider := range km.keyPools {
		providers = append(providers, provider)
	}

	return providers
}

// AddAPIKey 添加API Key
func (km *KeyManager) AddAPIKey(apiKey string) {
	km.mutex.Lock()
	defer km.mutex.Unlock()
	km.apiKeys = append(km.apiKeys, apiKey)
}

// ValidateAPIKey 验证API Key
func (km *KeyManager) ValidateAPIKey(apiKey string) bool {
	km.mutex.RLock()
	defer km.mutex.RUnlock()

	for _, key := range km.apiKeys {
		if key == apiKey {
			return true
		}
	}
	return false
}

// ListAPIKeys 列出所有API Keys
func (km *KeyManager) ListAPIKeys() []string {
	km.mutex.RLock()
	defer km.mutex.RUnlock()

	keys := make([]string, len(km.apiKeys))
	copy(keys, km.apiKeys)
	return keys
}
