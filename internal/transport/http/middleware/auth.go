package middleware

import (
	"net/http"
	"strings"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/security"
)

// AuthMiddleware 创建一个认证中间件
func AuthMiddleware(keyManager *security.KeyManager, apiKeyStore storage.APIKeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查Authorization头
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			// 检查认证类型
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 {
				http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
				return
			}

			authType := parts[0]
			tokenString := parts[1]

			switch authType {
			case "Bearer":
				// JWT认证
				_, err := keyManager.ValidateJWT(tokenString)
				if err != nil {
					http.Error(w, "Invalid JWT token", http.StatusUnauthorized)
					return
				}
			case "ApiKey":
				// API Key认证 - 首先尝试数据库验证
				valid := false
				if apiKeyStore != nil {
					if _, err := apiKeyStore.Verify(r.Context(), tokenString); err == nil {
						valid = true
					}
				}
				// 如果数据库验证失败，尝试keyManager验证（兼容配置文件中的密钥）
				if !valid && keyManager.ValidateAPIKey(tokenString) {
					valid = true
				}
				if !valid {
					http.Error(w, "Invalid API Key", http.StatusUnauthorized)
					return
				}
			default:
				http.Error(w, "Unsupported authorization type", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// APIKeyMiddleware 创建一个API Key中间件
func APIKeyMiddleware(validAPIKeys []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查X-API-Key头
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				http.Error(w, "X-API-Key header required", http.StatusUnauthorized)
				return
			}

			// 验证API Key
			valid := false
			for _, key := range validAPIKeys {
				if apiKey == key {
					valid = true
					break
				}
			}

			if !valid {
				http.Error(w, "Invalid API Key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
