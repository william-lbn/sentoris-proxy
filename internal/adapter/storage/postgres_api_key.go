package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

// PostgresAPIKeyStore 实现了基于PostgreSQL的API密钥存储
type PostgresAPIKeyStore struct {
	db *sql.DB
}

// NewPostgresAPIKeyStore 创建一个新的PostgreSQL API密钥存储
func NewPostgresAPIKeyStore(dsn string) (*PostgresAPIKeyStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// 创建表
	if err := createAPIKeyTable(db); err != nil {
		return nil, fmt.Errorf("failed to create api_keys table: %w", err)
	}

	return &PostgresAPIKeyStore{
		db: db,
	}, nil
}

// createAPIKeyTable 创建API密钥表
func createAPIKeyTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS api_keys (
		id SERIAL PRIMARY KEY,
		key_hash TEXT NOT NULL UNIQUE,
		key_prefix TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NULL,
		expires_at TIMESTAMP NULL,
		is_active BOOLEAN NOT NULL DEFAULT true,
		permissions JSONB NOT NULL DEFAULT '["read", "write"]',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_used_at TIMESTAMP NULL
	);

	CREATE INDEX IF NOT EXISTS idx_api_keys_key_prefix ON api_keys(key_prefix);
	CREATE INDEX IF NOT EXISTS idx_api_keys_is_active ON api_keys(is_active);
	CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys(expires_at);
	`

	_, err := db.Exec(query)
	return err
}

// Create 创建新的API密钥（存储哈希值）
func (s *PostgresAPIKeyStore) Create(ctx context.Context, keyHash, keyPrefix, name, description string, expiresAt *int64, permissions []string) (*APIKey, error) {
	permissionsJSON, err := json.Marshal(permissions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal permissions: %w", err)
	}

	var expiresAtTime sql.NullTime
	if expiresAt != nil {
		expiresAtTime.Time = time.Unix(*expiresAt, 0)
		expiresAtTime.Valid = true
	}

	query := `
	INSERT INTO api_keys (
		key_hash, key_prefix, name, description, 
		expires_at, is_active, permissions, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
	RETURNING id, created_at
	`

	var id int64
	var createdAt time.Time

	err = s.db.QueryRowContext(
		ctx,
		query,
		keyHash,
		keyPrefix,
		name,
		description,
		expiresAtTime,
		true,
		permissionsJSON,
	).Scan(&id, &createdAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create api key: %w", err)
	}

	return &APIKey{
		KeyID:        fmt.Sprintf("%d", id),
		KeyHash:      keyHash,
		KeyPrefix:    keyPrefix,
		Name:         name,
		Description:  description,
		ExpiresAt:    expiresAt,
		IsActive:     true,
		Permissions:  permissions,
		CreatedAt:    createdAt,
	}, nil
}

// GetByID 根据ID获取API密钥
func (s *PostgresAPIKeyStore) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	query := `
	SELECT id, key_hash, key_prefix, name, description, 
	       expires_at, is_active, permissions, created_at, last_used_at
	FROM api_keys
	WHERE id = $1
	`

	row := s.db.QueryRowContext(ctx, query, id)

	return s.scanAPIKey(row)
}

// GetByPrefix 根据前缀获取API密钥
func (s *PostgresAPIKeyStore) GetByPrefix(ctx context.Context, prefix string) (*APIKey, error) {
	query := `
	SELECT id, key_hash, key_prefix, name, description, 
	       expires_at, is_active, permissions, created_at, last_used_at
	FROM api_keys
	WHERE key_prefix = $1
	`

	row := s.db.QueryRowContext(ctx, query, prefix)

	return s.scanAPIKey(row)
}

// Verify 验证API密钥（比较哈希）
func (s *PostgresAPIKeyStore) Verify(ctx context.Context, key string) (*APIKey, error) {
	keyHash := HashAPIKey(key)

	query := `
	SELECT id, key_hash, key_prefix, name, description, 
	       expires_at, is_active, permissions, created_at, last_used_at
	FROM api_keys
	WHERE key_hash = $1 AND is_active = true
	`

	row := s.db.QueryRowContext(ctx, query, keyHash)

	keyObj, err := s.scanAPIKey(row)
	if err != nil {
		return nil, err
	}

	// 检查是否过期
	if keyObj.ExpiresAt != nil && *keyObj.ExpiresAt < time.Now().Unix() {
		return nil, fmt.Errorf("key expired")
	}

	return keyObj, nil
}

// List 获取所有API密钥
func (s *PostgresAPIKeyStore) List(ctx context.Context) ([]*APIKey, error) {
	query := `
	SELECT id, key_hash, key_prefix, name, description, 
	       expires_at, is_active, permissions, created_at, last_used_at
	FROM api_keys
	ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query api keys: %w", err)
	}
	defer rows.Close()

	var list []*APIKey
	for rows.Next() {
		keyObj, err := s.scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, keyObj)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return list, nil
}

// Update 更新API密钥信息
func (s *PostgresAPIKeyStore) Update(ctx context.Context, id int64, name, description string, isActive bool) error {
	query := `
	UPDATE api_keys
	SET name = $1, description = $2, is_active = $3, updated_at = CURRENT_TIMESTAMP
	WHERE id = $4
	`

	result, err := s.db.ExecContext(ctx, query, name, description, isActive, id)
	if err != nil {
		return fmt.Errorf("failed to update api key: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("api key not found")
	}

	return nil
}

// Delete 删除API密钥
func (s *PostgresAPIKeyStore) Delete(ctx context.Context, keyID string) error {
	query := "DELETE FROM api_keys WHERE id = $1"
	_, err := s.db.ExecContext(ctx, query, keyID)
	if err != nil {
		return fmt.Errorf("failed to delete api key: %w", err)
	}
	return nil
}

// UpdateLastUsed 更新最后使用时间
func (s *PostgresAPIKeyStore) UpdateLastUsed(ctx context.Context, id int64) error {
	query := "UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = $1"
	_, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to update last used time: %w", err)
	}
	return nil
}

// scanAPIKey 扫描API密钥行
func (s *PostgresAPIKeyStore) scanAPIKey(row scanner) (*APIKey, error) {
	var keyObj APIKey
	var expiresAt sql.NullTime
	var lastUsedAt sql.NullTime
	var permissionsJSON []byte
	var createdAt int64

	err := row.Scan(
		&keyObj.KeyID,
		&keyObj.KeyHash,
		&keyObj.KeyPrefix,
		&keyObj.Name,
		&keyObj.Description,
		&expiresAt,
		&keyObj.IsActive,
		&permissionsJSON,
		&createdAt,
		&lastUsedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("api key not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan api key: %w", err)
	}

	if err := json.Unmarshal(permissionsJSON, &keyObj.Permissions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
	}

	keyObj.CreatedAt = time.Unix(createdAt, 0)

	if expiresAt.Valid {
		expiresAtUnix := expiresAt.Time.Unix()
		keyObj.ExpiresAt = &expiresAtUnix
	}

	if lastUsedAt.Valid {
		lastUsedAtUnix := lastUsedAt.Time.Unix()
		keyObj.LastUsedAt = &lastUsedAtUnix
	}

	return &keyObj, nil
}

func (s *PostgresAPIKeyStore) Validate(ctx context.Context, keyID string) (bool, error) {
	id, err := strconv.ParseInt(keyID, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid key id")
	}
	query := "SELECT is_active FROM api_keys WHERE id = $1"
	var isActive bool
	err = s.db.QueryRowContext(ctx, query, id).Scan(&isActive)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return isActive, nil
}

// scanner 定义扫描接口
type scanner interface {
	Scan(dest ...interface{}) error
}