package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/lib/pq"
)

// PostgresProviderStore 实现了基于PostgreSQL的Provider存储
type PostgresProviderStore struct {
	db *sql.DB
}

// NewPostgresProviderStore 创建一个新的PostgreSQL Provider存储
func NewPostgresProviderStore(dsn string) (*PostgresProviderStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// 创建表
	if err := createProviderTable(db); err != nil {
		return nil, fmt.Errorf("failed to create provider table: %w", err)
	}

	return &PostgresProviderStore{
		db: db,
	}, nil
}

// createProviderTable 创建Provider表
func createProviderTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS providers (
		name TEXT PRIMARY KEY,
		base_url TEXT NOT NULL,
		auth_header TEXT NOT NULL,
		models JSONB NOT NULL,
		input_price_per_1k DECIMAL(10,6) NOT NULL DEFAULT 0,
		output_price_per_1k DECIMAL(10,6) NOT NULL DEFAULT 0,
		is_default BOOLEAN NOT NULL DEFAULT false,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_providers_is_default ON providers(is_default);
	`

	_, err := db.Exec(query)
	return err
}

// Save 保存Provider配置
func (s *PostgresProviderStore) Save(ctx context.Context, config *ProviderConfig) error {
	modelsJSON, err := json.Marshal(config.Models)
	if err != nil {
		return fmt.Errorf("failed to marshal models: %w", err)
	}

	query := `
	INSERT INTO providers (
		name, base_url, auth_header, models, 
		input_price_per_1k, output_price_per_1k, is_default,
		created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT (name) DO UPDATE SET
		base_url = $2,
		auth_header = $3,
		models = $4,
		input_price_per_1k = $5,
		output_price_per_1k = $6,
		is_default = $7,
		updated_at = CURRENT_TIMESTAMP
	`

	_, err = s.db.ExecContext(
		ctx,
		query,
		config.Name,
		config.BaseURL,
		config.AuthHeader,
		modelsJSON,
		config.InputPricePer1K,
		config.OutputPricePer1K,
		config.IsDefault,
	)

	if err != nil {
		return fmt.Errorf("failed to save provider: %w", err)
	}

	return nil
}

// GetByName 根据名称获取Provider
func (s *PostgresProviderStore) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	query := `
	SELECT name, base_url, auth_header, models, 
	       input_price_per_1k, output_price_per_1k, is_default
	FROM providers
	WHERE name = $1
	`

	row := s.db.QueryRowContext(ctx, query, name)

	var config ProviderConfig
	var modelsJSON []byte

	err := row.Scan(
		&config.Name,
		&config.BaseURL,
		&config.AuthHeader,
		&modelsJSON,
		&config.InputPricePer1K,
		&config.OutputPricePer1K,
		&config.IsDefault,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	if err := json.Unmarshal(modelsJSON, &config.Models); err != nil {
		return nil, fmt.Errorf("failed to unmarshal models: %w", err)
	}

	return &config, nil
}

// List 获取所有Provider
func (s *PostgresProviderStore) List(ctx context.Context) ([]*ProviderConfig, error) {
	query := `
	SELECT name, base_url, auth_header, models, 
	       input_price_per_1k, output_price_per_1k, is_default
	FROM providers
	ORDER BY name
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query providers: %w", err)
	}
	defer rows.Close()

	var list []*ProviderConfig
	for rows.Next() {
		var config ProviderConfig
		var modelsJSON []byte

		err := rows.Scan(
			&config.Name,
			&config.BaseURL,
			&config.AuthHeader,
			&modelsJSON,
			&config.InputPricePer1K,
			&config.OutputPricePer1K,
			&config.IsDefault,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan provider: %w", err)
		}

		if err := json.Unmarshal(modelsJSON, &config.Models); err != nil {
			return nil, fmt.Errorf("failed to unmarshal models: %w", err)
		}

		list = append(list, &config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return list, nil
}

// Delete 删除Provider
func (s *PostgresProviderStore) Delete(ctx context.Context, name string) error {
	query := "DELETE FROM providers WHERE name = $1"
	_, err := s.db.ExecContext(ctx, query, name)
	if err != nil {
		return fmt.Errorf("failed to delete provider: %w", err)
	}
	return nil
}

// GetDefault 获取默认Provider
func (s *PostgresProviderStore) GetDefault(ctx context.Context) (*ProviderConfig, error) {
	query := `
	SELECT name, base_url, auth_header, models, 
	       input_price_per_1k, output_price_per_1k, is_default
	FROM providers
	WHERE is_default = true
	LIMIT 1
	`

	row := s.db.QueryRowContext(ctx, query)

	var config ProviderConfig
	var modelsJSON []byte

	err := row.Scan(
		&config.Name,
		&config.BaseURL,
		&config.AuthHeader,
		&modelsJSON,
		&config.InputPricePer1K,
		&config.OutputPricePer1K,
		&config.IsDefault,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no default provider found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get default provider: %w", err)
	}

	if err := json.Unmarshal(modelsJSON, &config.Models); err != nil {
		return nil, fmt.Errorf("failed to unmarshal models: %w", err)
	}

	return &config, nil
}

// SetDefault 设置默认Provider
func (s *PostgresProviderStore) SetDefault(ctx context.Context, name string) error {
	// 先清除所有默认标记
	_, err := s.db.ExecContext(ctx, "UPDATE providers SET is_default = false")
	if err != nil {
		return fmt.Errorf("failed to clear default flags: %w", err)
	}

	// 设置新的默认Provider
	query := "UPDATE providers SET is_default = true WHERE name = $1"
	result, err := s.db.ExecContext(ctx, query, name)
	if err != nil {
		return fmt.Errorf("failed to set default provider: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("provider not found: %s", name)
	}

	return nil
}