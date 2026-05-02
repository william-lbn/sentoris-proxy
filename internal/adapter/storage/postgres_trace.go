package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"

	_ "github.com/lib/pq"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

// PostgresTraceStore 实现了基于PostgreSQL的Trace存储
type PostgresTraceStore struct {
	db *sql.DB
}

// NewPostgresTraceStore 创建一个新的PostgreSQL Trace存储
func NewPostgresTraceStore(dsn string) (*PostgresTraceStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// 配置连接池
	db.SetMaxOpenConns(25)                 // 最大打开连接数
	db.SetMaxIdleConns(5)                  // 最大空闲连接数
	db.SetConnMaxLifetime(5 * time.Minute) // 连接最大生命周期

	// 测试连接，带重试
	var lastErr error
	for i := 0; i < 5; i++ {
		if err := db.Ping(); err != nil {
			lastErr = err
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}
		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to ping postgres after 5 attempts: %w", lastErr)
	}

	// 创建表
	if err := createTraceTable(db); err != nil {
		return nil, fmt.Errorf("failed to create trace table: %w", err)
	}

	return &PostgresTraceStore{
		db: db,
	}, nil
}

// Close 关闭PostgreSQL连接
func (s *PostgresTraceStore) Close() error {
	return s.db.Close()
}

// createTraceTable 创建Trace表
func createTraceTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS traces (
		trace_id TEXT PRIMARY KEY,
		parent_id TEXT NULL,
		session_id TEXT NOT NULL,
		execution_state TEXT NOT NULL,
		model TEXT NOT NULL,
		input JSONB NOT NULL,
		output JSONB NOT NULL,
		observations JSONB NOT NULL,
		proofs JSONB NOT NULL,
		constraints_applied JSONB NOT NULL,
		created_at TIMESTAMP NOT NULL,
		ttl_expire_at TIMESTAMP NULL,
		extensions JSONB NOT NULL DEFAULT '{}'
	);

	CREATE INDEX IF NOT EXISTS idx_traces_session_id ON traces(session_id);
	CREATE INDEX IF NOT EXISTS idx_traces_parent_id ON traces(parent_id);
	CREATE INDEX IF NOT EXISTS idx_traces_created_at ON traces(created_at);
	CREATE INDEX IF NOT EXISTS idx_traces_ttl_expire_at ON traces(ttl_expire_at);
	`

	_, err := db.Exec(query)
	return err
}

// Save 保存Trace
func (s *PostgresTraceStore) Save(ctx context.Context, trace *domain.Trace) error {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "SaveTrace")
	defer span.End()

	// 序列化Trace
	inputJSON, err := json.Marshal(trace.Input)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal input: %w", err)
	}

	outputJSON, err := json.Marshal(trace.Output)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	observationsJSON, err := json.Marshal(trace.Observations)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal observations: %w", err)
	}

	proofsJSON, err := json.Marshal(trace.Proofs)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal proofs: %w", err)
	}

	constraintsAppliedJSON, err := json.Marshal(trace.ConstraintsApplied)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal constraints_applied: %w", err)
	}

	extensionsJSON, err := json.Marshal(trace.Extensions)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal extensions: %w", err)
	}

	// 准备父ID
	var parentID sql.NullString
	if trace.ParentID != nil {
		parentID.String = *trace.ParentID
		parentID.Valid = true
	}

	// 准备session ID
	var sessionID string
	if trace.SessionID != nil {
		sessionID = *trace.SessionID
	}

	// 准备TTL过期时间
	var ttlExpireAt sql.NullTime
	if trace.TTLExpireAt != nil {
		ttlExpireAt.Time = *trace.TTLExpireAt
		ttlExpireAt.Valid = true
	}

	// 执行插入
	query := `
	INSERT INTO traces (
		trace_id, parent_id, session_id, execution_state, model, 
		input, output, observations, proofs, constraints_applied, 
		created_at, ttl_expire_at, extensions
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	ON CONFLICT (trace_id) DO UPDATE SET
		parent_id = $2,
		session_id = $3,
		execution_state = $4,
		model = $5,
		input = $6,
		output = $7,
		observations = $8,
		proofs = $9,
		constraints_applied = $10,
		created_at = $11,
		ttl_expire_at = $12,
		extensions = $13
	`

	_, err = s.db.ExecContext(
		ctx,
		query,
		trace.TraceID,
		parentID,
		sessionID,
		trace.ExecutionState,
		trace.Model,
		inputJSON,
		outputJSON,
		observationsJSON,
		proofsJSON,
		constraintsAppliedJSON,
		trace.CreatedAt,
		ttlExpireAt,
		extensionsJSON,
	)

	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to save trace: %w", err)
	}

	return nil
}

// Update 更新Trace
func (s *PostgresTraceStore) Update(ctx context.Context, trace *domain.Trace) error {
	tracer := otel.Tracer("postgres-trace")
	ctx, span := tracer.Start(ctx, "UpdateTrace")
	defer span.End()

	inputJSON, err := json.Marshal(trace.Input)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal input: %w", err)
	}

	outputJSON, err := json.Marshal(trace.Output)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	observationsJSON, err := json.Marshal(trace.Observations)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal observations: %w", err)
	}

	proofsJSON, err := json.Marshal(trace.Proofs)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal proofs: %w", err)
	}

	constraintsAppliedJSON, err := json.Marshal(trace.ConstraintsApplied)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal constraints_applied: %w", err)
	}

	extensionsJSON, err := json.Marshal(trace.Extensions)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal extensions: %w", err)
	}

	var parentID sql.NullString
	if trace.ParentID != nil {
		parentID.String = *trace.ParentID
		parentID.Valid = true
	}

	var sessionID string
	if trace.SessionID != nil {
		sessionID = *trace.SessionID
	}

	var ttlExpireAt sql.NullTime
	if trace.TTLExpireAt != nil {
		ttlExpireAt.Time = *trace.TTLExpireAt
		ttlExpireAt.Valid = true
	}

	query := `
	UPDATE traces SET
		parent_id = $2,
		session_id = $3,
		execution_state = $4,
		model = $5,
		input = $6,
		output = $7,
		observations = $8,
		proofs = $9,
		constraints_applied = $10,
		created_at = $11,
		ttl_expire_at = $12,
		extensions = $13
	WHERE trace_id = $1
	`

	result, err := s.db.ExecContext(
		ctx,
		query,
		trace.TraceID,
		parentID,
		sessionID,
		trace.ExecutionState,
		trace.Model,
		inputJSON,
		outputJSON,
		observationsJSON,
		proofsJSON,
		constraintsAppliedJSON,
		trace.CreatedAt,
		ttlExpireAt,
		extensionsJSON,
	)

	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to update trace: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("trace not found: %s", trace.TraceID)
	}

	return nil
}

// Get 根据ID获取Trace
func (s *PostgresTraceStore) Get(ctx context.Context, traceID string) (*domain.Trace, error) {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "GetTraceByID")
	defer span.End()

	query := `
	SELECT 
		trace_id, parent_id, session_id, execution_state, model, 
		input, output, observations, proofs, constraints_applied, 
		created_at, ttl_expire_at, extensions
	FROM traces
	WHERE trace_id = $1
	`

	row := s.db.QueryRowContext(ctx, query, traceID)

	var trace domain.Trace
	var parentID sql.NullString
	var sessionID string
	var ttlExpireAt sql.NullTime
	var inputJSON, outputJSON, observationsJSON, proofsJSON, constraintsAppliedJSON, extensionsJSON []byte

	err := row.Scan(
		&trace.TraceID,
		&parentID,
		&sessionID,
		&trace.ExecutionState,
		&trace.Model,
		&inputJSON,
		&outputJSON,
		&observationsJSON,
		&proofsJSON,
		&constraintsAppliedJSON,
		&trace.CreatedAt,
		&ttlExpireAt,
		&extensionsJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get trace: %w", err)
	}

	// 解析JSON字段
	if err := json.Unmarshal(inputJSON, &trace.Input); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal input: %w", err)
	}

	if err := json.Unmarshal(outputJSON, &trace.Output); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal output: %w", err)
	}

	if err := json.Unmarshal(observationsJSON, &trace.Observations); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal observations: %w", err)
	}

	if err := json.Unmarshal(proofsJSON, &trace.Proofs); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal proofs: %w", err)
	}

	if err := json.Unmarshal(constraintsAppliedJSON, &trace.ConstraintsApplied); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal constraints_applied: %w", err)
	}

	if err := json.Unmarshal(extensionsJSON, &trace.Extensions); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal extensions: %w", err)
	}

	// 处理空值
	if parentID.Valid {
		trace.ParentID = &parentID.String
	}

	if ttlExpireAt.Valid {
		trace.TTLExpireAt = &ttlExpireAt.Time
	}

	return &trace, nil
}

// Delete 删除Trace
func (s *PostgresTraceStore) Delete(ctx context.Context, traceID string) error {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "DeleteTrace")
	defer span.End()

	query := "DELETE FROM traces WHERE trace_id = $1"
	_, err := s.db.ExecContext(ctx, query, traceID)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete trace: %w", err)
	}

	return nil
}

// ListBySession 根据会话ID列出Trace
func (s *PostgresTraceStore) ListBySession(ctx context.Context, sessionID string) ([]*domain.Trace, error) {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "ListTracesBySession")
	defer span.End()

	query := `
	SELECT 
		trace_id, parent_id, session_id, execution_state, model, 
		input, output, observations, proofs, constraints_applied, 
		created_at, ttl_expire_at, extensions
	FROM traces
	WHERE session_id = $1
	ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query traces: %w", err)
	}
	defer rows.Close()

	var traces []*domain.Trace
	for rows.Next() {
		var trace domain.Trace
		var parentID sql.NullString
		var sessionID string
		var ttlExpireAt sql.NullTime
		var inputJSON, outputJSON, observationsJSON, proofsJSON, constraintsAppliedJSON, extensionsJSON []byte

		err := rows.Scan(
			&trace.TraceID,
			&parentID,
			&sessionID,
			&trace.ExecutionState,
			&trace.Model,
			&inputJSON,
			&outputJSON,
			&observationsJSON,
			&proofsJSON,
			&constraintsAppliedJSON,
			&trace.CreatedAt,
			&ttlExpireAt,
			&extensionsJSON,
		)

		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to scan trace: %w", err)
		}

		// 解析JSON字段
		if err := json.Unmarshal(inputJSON, &trace.Input); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal input: %w", err)
		}

		if err := json.Unmarshal(outputJSON, &trace.Output); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal output: %w", err)
		}

		if err := json.Unmarshal(observationsJSON, &trace.Observations); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal observations: %w", err)
		}

		if err := json.Unmarshal(proofsJSON, &trace.Proofs); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal proofs: %w", err)
		}

		if err := json.Unmarshal(constraintsAppliedJSON, &trace.ConstraintsApplied); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal constraints_applied: %w", err)
		}

		if err := json.Unmarshal(extensionsJSON, &trace.Extensions); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal extensions: %w", err)
		}

		// 处理空值
		if parentID.Valid {
			trace.ParentID = &parentID.String
		}

		if ttlExpireAt.Valid {
			trace.TTLExpireAt = &ttlExpireAt.Time
		}

		traces = append(traces, &trace)
	}

	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return traces, nil
}

// CleanupExpired 清理过期的Trace
func (s *PostgresTraceStore) CleanupExpired(ctx context.Context) (int64, error) {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "CleanupExpiredTraces")
	defer span.End()

	query := "DELETE FROM traces WHERE ttl_expire_at < $1"
	result, err := s.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to cleanup expired traces: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}

// ListRecent 获取最近的N条Trace
func (s *PostgresTraceStore) ListRecent(ctx context.Context, limit int) ([]*domain.Trace, error) {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "ListRecentTraces")
	defer span.End()

	query := `
	SELECT
		trace_id, parent_id, session_id, execution_state, model,
		input, output, observations, proofs, constraints_applied,
		created_at, ttl_expire_at, extensions
	FROM traces
	ORDER BY created_at DESC
	LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query traces: %w", err)
	}
	defer rows.Close()

	var traces []*domain.Trace
	for rows.Next() {
		var trace domain.Trace
		var parentID sql.NullString
		var sessionID string
		var ttlExpireAt sql.NullTime
		var inputJSON, outputJSON, observationsJSON, proofsJSON, constraintsAppliedJSON, extensionsJSON []byte

		err := rows.Scan(
			&trace.TraceID,
			&parentID,
			&sessionID,
			&trace.ExecutionState,
			&trace.Model,
			&inputJSON,
			&outputJSON,
			&observationsJSON,
			&proofsJSON,
			&constraintsAppliedJSON,
			&trace.CreatedAt,
			&ttlExpireAt,
			&extensionsJSON,
		)

		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to scan trace: %w", err)
		}

		// 解析JSON字段
		if err := json.Unmarshal(inputJSON, &trace.Input); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal input: %w", err)
		}

		if err := json.Unmarshal(outputJSON, &trace.Output); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal output: %w", err)
		}

		if err := json.Unmarshal(observationsJSON, &trace.Observations); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal observations: %w", err)
		}

		if err := json.Unmarshal(proofsJSON, &trace.Proofs); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal proofs: %w", err)
		}

		if err := json.Unmarshal(constraintsAppliedJSON, &trace.ConstraintsApplied); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal constraints_applied: %w", err)
		}

		if err := json.Unmarshal(extensionsJSON, &trace.Extensions); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal extensions: %w", err)
		}

		// 处理空值
		if parentID.Valid {
			trace.ParentID = &parentID.String
		}

		if ttlExpireAt.Valid {
			trace.TTLExpireAt = &ttlExpireAt.Time
		}

		traces = append(traces, &trace)
	}

	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return traces, nil
}

// Count 获取Trace总数
func (s *PostgresTraceStore) Count(ctx context.Context) (int64, error) {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "CountTraces")
	defer span.End()

	query := "SELECT COUNT(*) FROM traces"
	var count int64
	err := s.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to count traces: %w", err)
	}

	return count, nil
}

// GetStats 获取统计信息
func (s *PostgresTraceStore) GetStats(ctx context.Context) (map[string]interface{}, error) {
	// 创建tracer
	tracer := otel.Tracer("postgres-trace")

	// 创建span
	ctx, span := tracer.Start(ctx, "GetTraceStats")
	defer span.End()

	stats := make(map[string]interface{})

	// 总数
	var totalCount int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM traces").Scan(&totalCount)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to count traces: %w", err)
	}
	stats["total"] = totalCount

	// 按状态统计
	query := `
	SELECT execution_state, COUNT(*)
	FROM traces
	GROUP BY execution_state
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query state stats: %w", err)
	}
	defer rows.Close()

	stateStats := make(map[string]int64)
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			span.RecordError(err)
			continue
		}
		stateStats[state] = count
	}
	stats["by_state"] = stateStats

	// 按模型统计
	query = `
	SELECT model, COUNT(*)
	FROM traces
	GROUP BY model
	`
	rows, err = s.db.QueryContext(ctx, query)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query model stats: %w", err)
	}
	defer rows.Close()

	modelStats := make(map[string]int64)
	for rows.Next() {
		var model string
		var count int64
		if err := rows.Scan(&model, &count); err != nil {
			span.RecordError(err)
			continue
		}
		modelStats[model] = count
	}
	stats["by_model"] = modelStats

	// 总成本
	query = `
	SELECT COALESCE(SUM((observations->>'cost_estimated_usd')::float), 0)
	FROM traces
	`
	var totalCost float64
	err = s.db.QueryRowContext(ctx, query).Scan(&totalCost)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get total cost: %w", err)
	}
	stats["total_cost"] = totalCost

	return stats, nil
}
