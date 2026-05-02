package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"

	_ "github.com/lib/pq"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

type PostgresRiskReportStore struct {
	db *sql.DB
}

func NewPostgresRiskReportStore(dsn string) (*PostgresRiskReportStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

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

	if err := createRiskReportTable(db); err != nil {
		return nil, fmt.Errorf("failed to create risk_report table: %w", err)
	}

	return &PostgresRiskReportStore{db: db}, nil
}

func createRiskReportTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS risk_reports (
		id SERIAL PRIMARY KEY,
		report_id TEXT UNIQUE NOT NULL,
		baseline_trace_id VARCHAR(64) NOT NULL,
		candidate_trace_id VARCHAR(64) NOT NULL,
		model_changed BOOLEAN DEFAULT FALSE,
		token_similarity_ratio DECIMAL(5,4),
		edit_distance INTEGER,
		risk_score DECIMAL(10,4),
		risk_methodology VARCHAR(100),
		risk_recommendation VARCHAR(20),
		risk_recommendation_reasons TEXT,
		baseline_length INTEGER,
		candidate_length INTEGER,
		diff_methodology VARCHAR(100),
		generated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_risk_reports_baseline ON risk_reports(baseline_trace_id);
	CREATE INDEX IF NOT EXISTS idx_risk_reports_candidate ON risk_reports(candidate_trace_id);
	CREATE INDEX IF NOT EXISTS idx_risk_reports_generated ON risk_reports(generated_at);
	`

	_, err := db.Exec(query)
	return err
}

func (s *PostgresRiskReportStore) Close() error {
	return s.db.Close()
}

func (s *PostgresRiskReportStore) Save(ctx context.Context, report *domain.RiskReport) error {
	tracer := otel.Tracer("postgres-risk-report")
	ctx, span := tracer.Start(ctx, "SaveRiskReport")
	defer span.End()

	if report.BaselineTraceID == "" || report.CandidateTraceID == "" {
		return fmt.Errorf("baseline_trace_id and candidate_trace_id are required")
	}

	reportID := report.BaselineTraceID + "_" + report.CandidateTraceID

	var reasonsStr string
	if report.Risk != nil && len(report.Risk.RecommendationReasons) > 0 {
		reasonsStr = strings.Join(report.Risk.RecommendationReasons, "|")
	}

	var riskScore float64
	var riskMethodology, riskRecommendation string
	if report.Risk != nil {
		riskScore = report.Risk.Score
		riskMethodology = report.Risk.Methodology
		riskRecommendation = string(report.Risk.Recommendation)
	}

	var baselineLen, candidateLen int
	var diffMethodology string
	var similarityRatio float64
	var editDistance int
	if report.TokenDiff != nil {
		baselineLen = report.TokenDiff.BaselineLength
		candidateLen = report.TokenDiff.CandidateLength
		diffMethodology = report.TokenDiff.Methodology
		similarityRatio = report.TokenDiff.SimilarityRatio
		editDistance = report.TokenDiff.EditDistance
	}

	generatedAt := time.Now()
	if report.GeneratedAt != "" {
		if t, err := time.Parse(time.RFC3339, report.GeneratedAt); err == nil {
			generatedAt = t
		}
	}

	query := `
	INSERT INTO risk_reports (
		report_id, baseline_trace_id, candidate_trace_id, model_changed,
		token_similarity_ratio, edit_distance, risk_score, risk_methodology,
		risk_recommendation, risk_recommendation_reasons, baseline_length,
		candidate_length, diff_methodology, generated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	ON CONFLICT (report_id) DO UPDATE SET
		token_similarity_ratio = EXCLUDED.token_similarity_ratio,
		edit_distance = EXCLUDED.edit_distance,
		risk_score = EXCLUDED.risk_score,
		risk_methodology = EXCLUDED.risk_methodology,
		risk_recommendation = EXCLUDED.risk_recommendation,
		risk_recommendation_reasons = EXCLUDED.risk_recommendation_reasons
	`

	_, err := s.db.ExecContext(ctx, query,
		reportID,
		report.BaselineTraceID,
		report.CandidateTraceID,
		report.ModelChanged,
		similarityRatio,
		editDistance,
		riskScore,
		riskMethodology,
		riskRecommendation,
		reasonsStr,
		baselineLen,
		candidateLen,
		diffMethodology,
		generatedAt,
	)

	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to save risk report: %w", err)
	}

	return nil
}

func (s *PostgresRiskReportStore) List(ctx context.Context, limit int) ([]*domain.RiskReport, error) {
	return s.ListRecent(ctx, limit)
}

func (s *PostgresRiskReportStore) Get(ctx context.Context, reportID string) (*domain.RiskReport, error) {
	tracer := otel.Tracer("postgres-risk-report")
	ctx, span := tracer.Start(ctx, "GetRiskReportByID")
	defer span.End()

	query := `
	SELECT report_id, baseline_trace_id, candidate_trace_id, model_changed,
		token_similarity_ratio, edit_distance, risk_score, risk_methodology,
		risk_recommendation, risk_recommendation_reasons, baseline_length,
		candidate_length, diff_methodology, generated_at
	FROM risk_reports WHERE report_id = $1
	`

	var report domain.RiskReport
	var reasonsStr sql.NullString
	var similarityRatio, riskScore sql.NullFloat64
	var editDistance, baselineLen, candidateLen sql.NullInt64
	var riskMethodology, riskRecommendation, diffMethodology sql.NullString
	var generatedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, reportID).Scan(
		&report.BaselineTraceID,
		&report.BaselineTraceID,
		&report.CandidateTraceID,
		&report.ModelChanged,
		&similarityRatio,
		&editDistance,
		&riskScore,
		&riskMethodology,
		&riskRecommendation,
		&reasonsStr,
		&baselineLen,
		&candidateLen,
		&diffMethodology,
		&generatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("risk report not found: %s", reportID)
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get risk report: %w", err)
	}

	if similarityRatio.Valid {
		report.TokenDiff = &domain.TokenDiff{
			SimilarityRatio: similarityRatio.Float64,
			EditDistance:    int(editDistance.Int64),
			BaselineLength:  int(baselineLen.Int64),
			CandidateLength: int(candidateLen.Int64),
			Methodology:     diffMethodology.String,
		}
	}

	if riskScore.Valid {
		report.Risk = &domain.RiskAssessment{
			Score:          riskScore.Float64,
			Methodology:    riskMethodology.String,
			Recommendation: domain.Recommendation(riskRecommendation.String),
		}
		if reasonsStr.Valid && reasonsStr.String != "" {
			report.Risk.RecommendationReasons = strings.Split(reasonsStr.String, "|")
		}
	}

	if generatedAt.Valid {
		report.GeneratedAt = generatedAt.Time.Format(time.RFC3339)
	}

	return &report, nil
}

func (s *PostgresRiskReportStore) GetByBaselineID(ctx context.Context, baselineID string) ([]*domain.RiskReport, error) {
	tracer := otel.Tracer("postgres-risk-report")
	ctx, span := tracer.Start(ctx, "GetRiskReportsByBaseline")
	defer span.End()

	query := `
	SELECT report_id, baseline_trace_id, candidate_trace_id, model_changed,
		token_similarity_ratio, edit_distance, risk_score, risk_methodology,
		risk_recommendation, risk_recommendation_reasons, baseline_length,
		candidate_length, diff_methodology, generated_at
	FROM risk_reports WHERE baseline_trace_id = $1 ORDER BY generated_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, baselineID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query risk reports: %w", err)
	}
	defer rows.Close()

	return scanRiskReports(rows)
}

func (s *PostgresRiskReportStore) ListRecent(ctx context.Context, limit int) ([]*domain.RiskReport, error) {
	tracer := otel.Tracer("postgres-risk-report")
	ctx, span := tracer.Start(ctx, "ListRecentRiskReports")
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	query := `
	SELECT report_id, baseline_trace_id, candidate_trace_id, model_changed,
		token_similarity_ratio, edit_distance, risk_score, risk_methodology,
		risk_recommendation, risk_recommendation_reasons, baseline_length,
		candidate_length, diff_methodology, generated_at
	FROM risk_reports ORDER BY generated_at DESC LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to query risk reports: %w", err)
	}
	defer rows.Close()

	return scanRiskReports(rows)
}

func scanRiskReports(rows *sql.Rows) ([]*domain.RiskReport, error) {
	var reports []*domain.RiskReport

	for rows.Next() {
		var report domain.RiskReport
		var reasonsStr sql.NullString
		var similarityRatio, riskScore sql.NullFloat64
		var editDistance, baselineLen, candidateLen sql.NullInt64
		var riskMethodology, riskRecommendation, diffMethodology sql.NullString
		var generatedAt sql.NullTime

		err := rows.Scan(
			&report.BaselineTraceID,
			&report.BaselineTraceID,
			&report.CandidateTraceID,
			&report.ModelChanged,
			&similarityRatio,
			&editDistance,
			&riskScore,
			&riskMethodology,
			&riskRecommendation,
			&reasonsStr,
			&baselineLen,
			&candidateLen,
			&diffMethodology,
			&generatedAt,
		)
		if err != nil {
			continue
		}

		if similarityRatio.Valid {
			report.TokenDiff = &domain.TokenDiff{
				SimilarityRatio: similarityRatio.Float64,
				EditDistance:    int(editDistance.Int64),
				BaselineLength:  int(baselineLen.Int64),
				CandidateLength: int(candidateLen.Int64),
				Methodology:     diffMethodology.String,
			}
		}

		if riskScore.Valid {
			report.Risk = &domain.RiskAssessment{
				Score:          riskScore.Float64,
				Methodology:    riskMethodology.String,
				Recommendation: domain.Recommendation(riskRecommendation.String),
			}
			if reasonsStr.Valid && reasonsStr.String != "" {
				report.Risk.RecommendationReasons = strings.Split(reasonsStr.String, "|")
			}
		}

		if generatedAt.Valid {
			report.GeneratedAt = generatedAt.Time.Format(time.RFC3339)
		}

		reports = append(reports, &report)
	}

	return reports, nil
}

func (s *PostgresRiskReportStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM risk_reports").Scan(&count)
	return count, err
}

func (s *PostgresRiskReportStore) ListByRiskLevel(ctx context.Context, riskLevel string, limit int) ([]*domain.RiskReport, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
	SELECT report_id, baseline_trace_id, candidate_trace_id, model_changed,
		token_similarity_ratio, edit_distance, risk_score, risk_methodology,
		risk_recommendation, risk_recommendation_reasons, baseline_length,
		candidate_length, diff_methodology, generated_at
	FROM risk_reports WHERE risk_recommendation = $1 ORDER BY generated_at DESC LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, riskLevel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanRiskReports(rows)
}

func (s *PostgresRiskReportStore) GetStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)

	var total int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM risk_reports").Scan(&total)
	if err != nil {
		return nil, err
	}
	stats["total"] = total

	var lowCount, mediumCount, highCount int64

	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM risk_reports WHERE risk_recommendation = 'APPROVE'").Scan(&lowCount)
	if err == nil {
		stats["low"] = lowCount
	}

	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM risk_reports WHERE risk_recommendation = 'REVIEW'").Scan(&mediumCount)
	if err == nil {
		stats["medium"] = mediumCount
	}

	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM risk_reports WHERE risk_recommendation = 'REJECT'").Scan(&highCount)
	if err == nil {
		stats["high"] = highCount
	}

	return stats, nil
}

func (s *PostgresRiskReportStore) Delete(ctx context.Context, traceID string) error {
	tracer := otel.Tracer("postgres-risk-report")
	ctx, span := tracer.Start(ctx, "DeleteRiskReport")
	defer span.End()

	query := "DELETE FROM risk_reports WHERE baseline_trace_id = $1 OR candidate_trace_id = $1"
	_, err := s.db.ExecContext(ctx, query, traceID)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete risk report: %w", err)
	}

	return nil
}
