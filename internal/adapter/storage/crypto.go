package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
)

// GenerateKey 生成AES密钥
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32) // 256-bit key
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// Encrypt 加密数据
func Encrypt(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密数据
func Decrypt(key []byte, ciphertextBase64 string) ([]byte, error) {
	ciphertext, err := base64.URLEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, err
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// HashAPIKey 对API密钥进行哈希处理（使用SHA256）
func HashAPIKey(key string) string {
	h := sha256.New()
	h.Write([]byte(key))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

// GenerateAPIKey 生成随机API密钥
func GenerateAPIKey() (string, error) {
	key := make([]byte, 32) // 256-bit key
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(key), nil
}

// GetKeyPrefix 获取密钥前缀（用于识别）
func GetKeyPrefix(key string) string {
	if len(key) < 8 {
		return key
	}
	return key[:8]
}