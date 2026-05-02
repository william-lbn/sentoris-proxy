package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

// SQLiteStore is a combined storage implementation using SQLite
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite store
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Create data directory
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Set connection pool
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Create tables
	if err := createTables(db); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS traces (
			trace_id TEXT PRIMARY KEY,
			parent_id TEXT,
			session_id TEXT,
			execution_state TEXT,
			model TEXT,
			input_json TEXT,
			output_json TEXT,
			observations_json TEXT,
			proofs_json TEXT,
			constraints_applied_json TEXT,
			created_at DATETIME,
			ttl_expire_at DATETIME,
			extensions_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS risk_reports (
			baseline_trace_id TEXT PRIMARY KEY,
			replay_trace_id TEXT,
			score REAL,
			details_json TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			key_id TEXT PRIMARY KEY,
			name TEXT,
			description TEXT,
			key_hash TEXT,
			key_prefix TEXT,
			created_at DATETIME,
			expires_at INTEGER,
			is_active BOOLEAN,
			permissions_json TEXT,
			last_used_at INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS providers (
			name TEXT PRIMARY KEY,
			base_url TEXT,
			auth_header TEXT,
			models_json TEXT,
			pricing_json TEXT,
			input_price_per_1k REAL,
			output_price_per_1k REAL,
			is_default BOOLEAN,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS budget_store (
			session_id TEXT PRIMARY KEY,
			total_budget REAL,
			used_budget REAL,
			reserved_budget REAL,
			updated_at DATETIME
		)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
	}

	return nil
}

// ========== TraceStore implementation ==========

func (s *SQLiteStore) SaveTrace(ctx context.Context, trace *domain.Trace) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO traces (
			trace_id, parent_id, session_id, execution_state, model,
			input_json, output_json, observations_json, proofs_json,
			constraints_applied_json, created_at, ttl_expire_at, extensions_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trace.TraceID,
		trace.ParentID,
		trace.SessionID,
		trace.ExecutionState,
		trace.Model,
		mustMarshal(trace.Input),
		mustMarshal(trace.Output),
		mustMarshal(trace.Observations),
		mustMarshal(trace.Proofs),
		mustMarshal(trace.ConstraintsApplied),
		trace.CreatedAt,
		trace.TTLExpireAt,
		mustMarshal(trace.Extensions),
	)
	return err
}

func (s *SQLiteStore) GetTrace(ctx context.Context, traceID string) (*domain.Trace, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT trace_id, parent_id, session_id, execution_state, model,
			input_json, output_json, observations_json, proofs_json,
			constraints_applied_json, created_at, ttl_expire_at, extensions_json
		FROM traces WHERE trace_id = ?`, traceID)

	var trace domain.Trace
	var inputJSON, outputJSON, observationsJSON, proofsJSON, constraintsJSON, extensionsJSON string

	err := row.Scan(
		&trace.TraceID,
		&trace.ParentID,
		&trace.SessionID,
		&trace.ExecutionState,
		&trace.Model,
		&inputJSON,
		&outputJSON,
		&observationsJSON,
		&proofsJSON,
		&constraintsJSON,
		&trace.CreatedAt,
		&trace.TTLExpireAt,
		&extensionsJSON,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}
	if err != nil {
		return nil, err
	}

	mustUnmarshal(inputJSON, &trace.Input)
	mustUnmarshal(outputJSON, &trace.Output)
	mustUnmarshal(observationsJSON, &trace.Observations)
	mustUnmarshal(proofsJSON, &trace.Proofs)
	mustUnmarshal(constraintsJSON, &trace.ConstraintsApplied)
	mustUnmarshal(extensionsJSON, &trace.Extensions)

	return &trace, nil
}

func (s *SQLiteStore) UpdateTrace(ctx context.Context, trace *domain.Trace) error {
	return s.SaveTrace(ctx, trace)
}

func (s *SQLiteStore) DeleteTrace(ctx context.Context, traceID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM traces WHERE trace_id = ?", traceID)
	return err
}

func (s *SQLiteStore) ListTracesBySession(ctx context.Context, sessionID string) ([]*domain.Trace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT trace_id, parent_id, session_id, execution_state, model,
			input_json, output_json, observations_json, proofs_json,
			constraints_applied_json, created_at, ttl_expire_at, extensions_json
		FROM traces WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	_ = rows.Close()

	var traces []*domain.Trace
	for rows.Next() {
		var trace domain.Trace
		var inputJSON, outputJSON, observationsJSON, proofsJSON, constraintsJSON, extensionsJSON string

		err := rows.Scan(
			&trace.TraceID,
			&trace.ParentID,
			&trace.SessionID,
			&trace.ExecutionState,
			&trace.Model,
			&inputJSON,
			&outputJSON,
			&observationsJSON,
			&proofsJSON,
			&constraintsJSON,
			&trace.CreatedAt,
			&trace.TTLExpireAt,
			&extensionsJSON,
		)
		if err != nil {
			return nil, err
		}

		mustUnmarshal(inputJSON, &trace.Input)
		mustUnmarshal(outputJSON, &trace.Output)
		mustUnmarshal(observationsJSON, &trace.Observations)
		mustUnmarshal(proofsJSON, &trace.Proofs)
		mustUnmarshal(constraintsJSON, &trace.ConstraintsApplied)
		mustUnmarshal(extensionsJSON, &trace.Extensions)

		traces = append(traces, &trace)
	}

	return traces, nil
}

func (s *SQLiteStore) CleanupExpiredTraces(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM traces WHERE ttl_expire_at IS NOT NULL AND ttl_expire_at < ?", time.Now().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteStore) ListRecentTraces(ctx context.Context, limit int) ([]*domain.Trace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT trace_id, parent_id, session_id, execution_state, model,
			input_json, output_json, observations_json, proofs_json,
			constraints_applied_json, created_at, ttl_expire_at, extensions_json
		FROM traces ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	_ = rows.Close()

	var traces []*domain.Trace
	for rows.Next() {
		var trace domain.Trace
		var inputJSON, outputJSON, observationsJSON, proofsJSON, constraintsJSON, extensionsJSON string

		err := rows.Scan(
			&trace.TraceID,
			&trace.ParentID,
			&trace.SessionID,
			&trace.ExecutionState,
			&trace.Model,
			&inputJSON,
			&outputJSON,
			&observationsJSON,
			&proofsJSON,
			&constraintsJSON,
			&trace.CreatedAt,
			&trace.TTLExpireAt,
			&extensionsJSON,
		)
		if err != nil {
			return nil, err
		}

		mustUnmarshal(inputJSON, &trace.Input)
		mustUnmarshal(outputJSON, &trace.Output)
		mustUnmarshal(observationsJSON, &trace.Observations)
		mustUnmarshal(proofsJSON, &trace.Proofs)
		mustUnmarshal(constraintsJSON, &trace.ConstraintsApplied)
		mustUnmarshal(extensionsJSON, &trace.Extensions)

		traces = append(traces, &trace)
	}

	return traces, nil
}

func (s *SQLiteStore) CountTraces(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM traces").Scan(&count)
	return count, err
}

func (s *SQLiteStore) GetTraceStats(ctx context.Context) (map[string]interface{}, error) {
	var total, completed, failed int64
	err := s.db.QueryRowContext(ctx, `
		SELECT 
			COUNT(*),
			SUM(CASE WHEN execution_state = 'completed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN execution_state = 'failed' THEN 1 ELSE 0 END)
		FROM traces`).Scan(&total, &completed, &failed)
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"total":     total,
		"completed": completed,
		"failed":    failed,
	}
	return stats, nil
}

// ========== RiskReportStore implementation ==========

func (s *SQLiteStore) SaveRiskReport(ctx context.Context, report *domain.RiskReport) error {
	score := 0.0
	if report.Risk != nil {
		score = report.Risk.Score
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO risk_reports (
			baseline_trace_id, replay_trace_id, score, details_json, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		report.BaselineTraceID,
		report.CandidateTraceID,
		score,
		mustMarshal(report),
		time.Now().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetRiskReport(ctx context.Context, traceID string) (*domain.RiskReport, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT details_json
		FROM risk_reports WHERE baseline_trace_id = ?`, traceID)

	var detailsJSON string

	err := row.Scan(&detailsJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("risk report not found: %s", traceID)
	}
	if err != nil {
		return nil, err
	}

	var report domain.RiskReport
	mustUnmarshal(detailsJSON, &report)

	return &report, nil
}

func (s *SQLiteStore) ListRiskReports(ctx context.Context, limit int) ([]*domain.RiskReport, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT details_json
		FROM risk_reports ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	_ = rows.Close()

	var reports []*domain.RiskReport
	for rows.Next() {
		var detailsJSON string

		err := rows.Scan(&detailsJSON)
		if err != nil {
			return nil, err
		}

		var report domain.RiskReport
		mustUnmarshal(detailsJSON, &report)
		reports = append(reports, &report)
	}

	return reports, nil
}

func (s *SQLiteStore) DeleteRiskReport(ctx context.Context, traceID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM risk_reports WHERE baseline_trace_id = ?", traceID)
	return err
}

// ========== APIKeyStore implementation ==========

func (s *SQLiteStore) CreateAPIKey(ctx context.Context, keyHash, keyPrefix, name, description string, expiresAt *int64, permissions []string) (*APIKey, error) {
	keyID := fmt.Sprintf("key_%d", time.Now().UnixNano())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (
			key_id, name, description, key_hash, key_prefix, 
			created_at, expires_at, is_active, permissions_json, last_used_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		keyID,
		name,
		description,
		keyHash,
		keyPrefix,
		time.Now().Format(time.RFC3339),
		expiresAt,
		true,
		mustMarshal(permissions),
		time.Now().Unix(),
	)
	if err != nil {
		return nil, err
	}

	return &APIKey{
		KeyID:       keyID,
		Name:        name,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		ExpiresAt:   expiresAt,
		IsActive:    true,
		Permissions: permissions,
	}, nil
}

func (s *SQLiteStore) GetAPIKey(ctx context.Context, keyID string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT key_id, name, description, key_hash, key_prefix, 
			created_at, expires_at, is_active, permissions_json, last_used_at
		FROM api_keys WHERE key_id = ?`, keyID)

	var keyData APIKey
	var permissionsJSON string

	err := row.Scan(
		&keyData.KeyID,
		&keyData.Name,
		&keyData.Description,
		&keyData.KeyHash,
		&keyData.KeyPrefix,
		&keyData.CreatedAt,
		&keyData.ExpiresAt,
		&keyData.IsActive,
		&permissionsJSON,
		&keyData.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("api key not found: %s", keyID)
	}
	if err != nil {
		return nil, err
	}

	mustUnmarshal(permissionsJSON, &keyData.Permissions)
	return &keyData, nil
}

func (s *SQLiteStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT key_id, name, description, key_hash, key_prefix, 
			created_at, expires_at, is_active, permissions_json, last_used_at
		FROM api_keys WHERE key_hash = ?`, keyHash)

	var keyData APIKey
	var permissionsJSON string

	err := row.Scan(
		&keyData.KeyID,
		&keyData.Name,
		&keyData.Description,
		&keyData.KeyHash,
		&keyData.KeyPrefix,
		&keyData.CreatedAt,
		&keyData.ExpiresAt,
		&keyData.IsActive,
		&permissionsJSON,
		&keyData.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("api key not found")
	}
	if err != nil {
		return nil, err
	}

	mustUnmarshal(permissionsJSON, &keyData.Permissions)
	return &keyData, nil
}

func (s *SQLiteStore) ListAPIKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key_id, name, description, key_hash, key_prefix, 
			created_at, expires_at, is_active, permissions_json, last_used_at
		FROM api_keys`)
	if err != nil {
		return nil, err
	}
	_ = rows.Close()

	var keys []*APIKey
	for rows.Next() {
		var keyData APIKey
		var permissionsJSON string

		err := rows.Scan(
			&keyData.KeyID,
			&keyData.Name,
			&keyData.Description,
			&keyData.KeyHash,
			&keyData.KeyPrefix,
			&keyData.CreatedAt,
			&keyData.ExpiresAt,
			&keyData.IsActive,
			&permissionsJSON,
			&keyData.LastUsedAt,
		)
		if err != nil {
			return nil, err
		}

		mustUnmarshal(permissionsJSON, &keyData.Permissions)
		keys = append(keys, &keyData)
	}

	return keys, nil
}

func (s *SQLiteStore) DeleteAPIKey(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM api_keys WHERE key_id = ?", keyID)
	return err
}

func (s *SQLiteStore) UpdateAPIKey(ctx context.Context, keyID string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	setClauses := []string{}
	args := []interface{}{}

	if name, ok := updates["name"]; ok {
		setClauses = append(setClauses, "name = ?")
		args = append(args, name)
	}
	if description, ok := updates["description"]; ok {
		setClauses = append(setClauses, "description = ?")
		args = append(args, description)
	}
	if expiresAt, ok := updates["expires_at"]; ok {
		setClauses = append(setClauses, "expires_at = ?")
		args = append(args, expiresAt)
	}
	if isActive, ok := updates["is_active"]; ok {
		setClauses = append(setClauses, "is_active = ?")
		args = append(args, isActive)
	}
	if permissions, ok := updates["permissions"]; ok {
		setClauses = append(setClauses, "permissions_json = ?")
		args = append(args, mustMarshal(permissions))
	}

	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, time.Now().Format(time.RFC3339))
	args = append(args, keyID)

	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE key_id = ?", joinStrings(setClauses, ", "))
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *SQLiteStore) UpdateAPIKeyLastUsed(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE api_keys SET last_used_at = ? WHERE key_id = ?", time.Now().Unix(), keyID)
	return err
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// ========== ProviderStore implementation ==========

func (s *SQLiteStore) SaveProvider(ctx context.Context, name string, config *ProviderConfig) error {
	isDefault := false
	if config.IsDefault {
		isDefault = true
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO providers (
			name, base_url, auth_header, models_json, pricing_json, 
			input_price_per_1k, output_price_per_1k, is_default, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name,
		config.BaseURL,
		config.AuthHeader,
		mustMarshal(config.Models),
		"{}",
		config.InputPricePer1K,
		config.OutputPricePer1K,
		isDefault,
		time.Now().Format(time.RFC3339),
		time.Now().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetProvider(ctx context.Context, name string) (*ProviderConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT name, base_url, auth_header, models_json, 
			input_price_per_1k, output_price_per_1k, is_default
		FROM providers WHERE name = ?`, name)

	var config ProviderConfig
	var modelsJSON string

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
		return nil, err
	}

	mustUnmarshal(modelsJSON, &config.Models)
	return &config, nil
}

func (s *SQLiteStore) ListProviders(ctx context.Context) ([]*ProviderConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, base_url, auth_header, models_json, 
			input_price_per_1k, output_price_per_1k, is_default
		FROM providers`)
	if err != nil {
		return nil, err
	}
	_ = rows.Close()

	var providers []*ProviderConfig
	for rows.Next() {
		var config ProviderConfig
		var modelsJSON string

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
			return nil, err
		}

		mustUnmarshal(modelsJSON, &config.Models)
		providers = append(providers, &config)
	}

	return providers, nil
}

func (s *SQLiteStore) DeleteProvider(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM providers WHERE name = ?", name)
	return err
}

func (s *SQLiteStore) GetDefaultProvider(ctx context.Context) (*ProviderConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT name, base_url, auth_header, models_json, 
			input_price_per_1k, output_price_per_1k, is_default
		FROM providers WHERE is_default = true`)

	var config ProviderConfig
	var modelsJSON string

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
		return nil, err
	}

	mustUnmarshal(modelsJSON, &config.Models)
	return &config, nil
}

func (s *SQLiteStore) SetDefaultProvider(ctx context.Context, name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "UPDATE providers SET is_default = false")
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	_, err = tx.ExecContext(ctx, "UPDATE providers SET is_default = true WHERE name = ?", name)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// ========== BudgetStore implementation ==========

func (s *SQLiteStore) ReserveBudget(ctx context.Context, sessionID string, amount float64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}

	var totalBudget, usedBudget, reservedBudget float64
	err = tx.QueryRowContext(ctx, `
		SELECT total_budget, used_budget, reserved_budget 
		FROM budget_store WHERE session_id = ?`, sessionID).Scan(&totalBudget, &usedBudget, &reservedBudget)

	if err == sql.ErrNoRows {
		_ = tx.Rollback()
		return false, fmt.Errorf("no budget found for session: %s", sessionID)
	}
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}

	if usedBudget+reservedBudget+amount > totalBudget {
		_ = tx.Rollback()
		return false, nil
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE budget_store SET reserved_budget = ?, updated_at = ? 
		WHERE session_id = ?`, reservedBudget+amount, time.Now().Format(time.RFC3339), sessionID)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}

	return tx.Commit() == nil, nil
}

func (s *SQLiteStore) CommitBudget(ctx context.Context, sessionID string, amount float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	var reservedBudget float64
	err = tx.QueryRowContext(ctx, `
		SELECT reserved_budget FROM budget_store WHERE session_id = ?`, sessionID).Scan(&reservedBudget)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	if reservedBudget < amount {
		_ = tx.Rollback()
		return fmt.Errorf("insufficient reserved budget")
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE budget_store 
		SET used_budget = used_budget + ?, reserved_budget = reserved_budget - ?, updated_at = ? 
		WHERE session_id = ?`, amount, amount, time.Now().Format(time.RFC3339), sessionID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (s *SQLiteStore) RollbackBudget(ctx context.Context, sessionID string, amount float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	var reservedBudget float64
	err = tx.QueryRowContext(ctx, `
		SELECT reserved_budget FROM budget_store WHERE session_id = ?`, sessionID).Scan(&reservedBudget)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	if reservedBudget < amount {
		_ = tx.Rollback()
		return fmt.Errorf("insufficient reserved budget to rollback")
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE budget_store 
		SET reserved_budget = reserved_budget - ?, updated_at = ? 
		WHERE session_id = ?`, amount, time.Now().Format(time.RFC3339), sessionID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetRemainingBudget(ctx context.Context, sessionID string) (float64, error) {
	var totalBudget, usedBudget, reservedBudget float64
	err := s.db.QueryRowContext(ctx, `
		SELECT total_budget, used_budget, reserved_budget 
		FROM budget_store WHERE session_id = ?`, sessionID).Scan(&totalBudget, &usedBudget, &reservedBudget)

	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no budget found for session: %s", sessionID)
	}
	if err != nil {
		return 0, err
	}

	return totalBudget - usedBudget - reservedBudget, nil
}

func (s *SQLiteStore) SetSessionBudget(ctx context.Context, sessionID string, amount float64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO budget_store (
			session_id, total_budget, used_budget, reserved_budget, updated_at
		) VALUES (?, ?, ?, ?, ?)`,
		sessionID,
		amount,
		0,
		0,
		time.Now().Format(time.RFC3339),
	)
	return err
}

// ========== Wrapper types for interface implementation ==========

// SQLiteTraceStore implements TraceStore interface
type SQLiteTraceStore struct {
	*SQLiteStore
}

func (s *SQLiteTraceStore) Save(ctx context.Context, trace *domain.Trace) error {
	return s.SaveTrace(ctx, trace)
}

func (s *SQLiteTraceStore) Get(ctx context.Context, traceID string) (*domain.Trace, error) {
	return s.GetTrace(ctx, traceID)
}

func (s *SQLiteTraceStore) Update(ctx context.Context, trace *domain.Trace) error {
	return s.UpdateTrace(ctx, trace)
}

func (s *SQLiteTraceStore) Delete(ctx context.Context, traceID string) error {
	return s.DeleteTrace(ctx, traceID)
}

func (s *SQLiteTraceStore) ListBySession(ctx context.Context, sessionID string) ([]*domain.Trace, error) {
	return s.ListTracesBySession(ctx, sessionID)
}

func (s *SQLiteTraceStore) CleanupExpired(ctx context.Context) (int64, error) {
	return s.CleanupExpiredTraces(ctx)
}

func (s *SQLiteTraceStore) ListRecent(ctx context.Context, limit int) ([]*domain.Trace, error) {
	return s.ListRecentTraces(ctx, limit)
}

func (s *SQLiteTraceStore) Count(ctx context.Context) (int64, error) {
	return s.CountTraces(ctx)
}

func (s *SQLiteTraceStore) GetStats(ctx context.Context) (map[string]interface{}, error) {
	return s.GetTraceStats(ctx)
}

// SQLiteBudgetStore implements BudgetStore interface
type SQLiteBudgetStore struct {
	*SQLiteStore
}

func (s *SQLiteBudgetStore) Reserve(ctx context.Context, sessionID string, amount float64) (bool, error) {
	return s.ReserveBudget(ctx, sessionID, amount)
}

func (s *SQLiteBudgetStore) Commit(ctx context.Context, sessionID string, amount float64) error {
	return s.CommitBudget(ctx, sessionID, amount)
}

func (s *SQLiteBudgetStore) Rollback(ctx context.Context, sessionID string, amount float64) error {
	return s.RollbackBudget(ctx, sessionID, amount)
}

func (s *SQLiteBudgetStore) GetRemaining(ctx context.Context, sessionID string) (float64, error) {
	return s.GetRemainingBudget(ctx, sessionID)
}

func (s *SQLiteBudgetStore) SetBudget(ctx context.Context, sessionID string, amount float64) error {
	return s.SetSessionBudget(ctx, sessionID, amount)
}

func (s *SQLiteBudgetStore) Get(ctx context.Context) (*Budget, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT SUM(total_budget) as total, SUM(used_budget) as used FROM budget_store")
	if err != nil {
		return nil, err
	}
	_ = rows.Close()

	var total, used float64
	if rows.Next() {
		err := rows.Scan(&total, &used)
		if err != nil {
			return nil, err
		}
	}

	return &Budget{
		TotalBudget: total,
		UsedBudget:  used,
	}, nil
}

func (s *SQLiteBudgetStore) Set(ctx context.Context, amount float64) error {
	// This sets a global budget - we'll use a special session ID for this
	return s.SetSessionBudget(ctx, "__global__", amount)
}

// SQLiteProviderStore implements ProviderStore interface
type SQLiteProviderStore struct {
	*SQLiteStore
}

func (s *SQLiteProviderStore) Save(ctx context.Context, config *ProviderConfig) error {
	return s.SaveProvider(ctx, config.Name, config)
}

func (s *SQLiteProviderStore) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	return s.GetProvider(ctx, name)
}

func (s *SQLiteProviderStore) List(ctx context.Context) ([]*ProviderConfig, error) {
	return s.ListProviders(ctx)
}

func (s *SQLiteProviderStore) Delete(ctx context.Context, name string) error {
	return s.DeleteProvider(ctx, name)
}

func (s *SQLiteProviderStore) GetDefault(ctx context.Context) (*ProviderConfig, error) {
	return s.GetDefaultProvider(ctx)
}

func (s *SQLiteProviderStore) SetDefault(ctx context.Context, name string) error {
	return s.SetDefaultProvider(ctx, name)
}

// SQLiteRiskReportStore implements RiskReportStore interface
type SQLiteRiskReportStore struct {
	*SQLiteStore
}

func (s *SQLiteRiskReportStore) Save(ctx context.Context, report *domain.RiskReport) error {
	return s.SaveRiskReport(ctx, report)
}

func (s *SQLiteRiskReportStore) Get(ctx context.Context, traceID string) (*domain.RiskReport, error) {
	return s.GetRiskReport(ctx, traceID)
}

func (s *SQLiteRiskReportStore) List(ctx context.Context, limit int) ([]*domain.RiskReport, error) {
	return s.ListRiskReports(ctx, limit)
}

func (s *SQLiteRiskReportStore) Delete(ctx context.Context, traceID string) error {
	return s.DeleteRiskReport(ctx, traceID)
}

// SQLiteAPIKeyStore implements APIKeyStore interface
type SQLiteAPIKeyStore struct {
	*SQLiteStore
}

func (s *SQLiteAPIKeyStore) Create(ctx context.Context, keyHash, keyPrefix, name, description string, expiresAt *int64, permissions []string) (*APIKey, error) {
	return s.CreateAPIKey(ctx, keyHash, keyPrefix, name, description, expiresAt, permissions)
}

func (s *SQLiteAPIKeyStore) List(ctx context.Context) ([]*APIKey, error) {
	return s.ListAPIKeys(ctx)
}

func (s *SQLiteAPIKeyStore) Delete(ctx context.Context, keyID string) error {
	return s.DeleteAPIKey(ctx, keyID)
}

func (s *SQLiteAPIKeyStore) Validate(ctx context.Context, keyID string) (bool, error) {
	key, err := s.GetAPIKey(ctx, keyID)
	if err != nil {
		return false, err
	}
	return key.IsActive, nil
}

func (s *SQLiteAPIKeyStore) Verify(ctx context.Context, key string) (*APIKey, error) {
	keyHash := HashAPIKey(key)
	return s.GetAPIKeyByHash(ctx, keyHash)
}
