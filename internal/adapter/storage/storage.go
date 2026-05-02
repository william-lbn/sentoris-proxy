package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

type TraceStore interface {
	Save(ctx context.Context, trace *domain.Trace) error
	Get(ctx context.Context, traceID string) (*domain.Trace, error)
	Update(ctx context.Context, trace *domain.Trace) error
	Delete(ctx context.Context, traceID string) error
	ListBySession(ctx context.Context, sessionID string) ([]*domain.Trace, error)
	CleanupExpired(ctx context.Context) (int64, error)
	ListRecent(ctx context.Context, limit int) ([]*domain.Trace, error)
	Count(ctx context.Context) (int64, error)
	GetStats(ctx context.Context) (map[string]interface{}, error)
}

type BudgetStore interface {
	Reserve(ctx context.Context, sessionID string, amount float64) (bool, error)
	Commit(ctx context.Context, sessionID string, amount float64) error
	Rollback(ctx context.Context, sessionID string, amount float64) error
	GetRemaining(ctx context.Context, sessionID string) (float64, error)
	Get(ctx context.Context) (*Budget, error)
	Set(ctx context.Context, amount float64) error
	SetBudget(ctx context.Context, sessionID string, amount float64) error
}

type Budget struct {
	TotalBudget float64 `json:"total_budget"`
	UsedBudget  float64 `json:"used_budget"`
}

type MemoryTraceStore struct {
	traces map[string]*domain.Trace
	mu     sync.RWMutex
}

func NewMemoryTraceStore() *MemoryTraceStore {
	return &MemoryTraceStore{
		traces: make(map[string]*domain.Trace),
	}
}

func (s *MemoryTraceStore) Save(ctx context.Context, trace *domain.Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traces[trace.TraceID] = trace
	return nil
}

func (s *MemoryTraceStore) Get(ctx context.Context, traceID string) (*domain.Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trace, ok := s.traces[traceID]
	if !ok {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}
	return trace, nil
}

func (s *MemoryTraceStore) Update(ctx context.Context, trace *domain.Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.traces[trace.TraceID]; !ok {
		return fmt.Errorf("trace not found: %s", trace.TraceID)
	}
	s.traces[trace.TraceID] = trace
	return nil
}

func (s *MemoryTraceStore) Delete(ctx context.Context, traceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.traces, traceID)
	return nil
}

func (s *MemoryTraceStore) ListBySession(ctx context.Context, sessionID string) ([]*domain.Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var traces []*domain.Trace
	for _, trace := range s.traces {
		if trace.SessionID != nil && *trace.SessionID == sessionID {
			traces = append(traces, trace)
		}
	}
	return traces, nil
}

func (s *MemoryTraceStore) CleanupExpired(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	count := int64(0)
	for id, trace := range s.traces {
		if trace.TTLExpireAt != nil && trace.TTLExpireAt.Before(now) {
			delete(s.traces, id)
			count++
		}
	}
	return count, nil
}

func (s *MemoryTraceStore) ListRecent(ctx context.Context, limit int) ([]*domain.Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var traces []*domain.Trace
	for _, trace := range s.traces {
		traces = append(traces, trace)
	}

	for i := 0; i < len(traces)-1; i++ {
		for j := i + 1; j < len(traces); j++ {
			if traces[i].CreatedAt.Before(traces[j].CreatedAt) {
				traces[i], traces[j] = traces[j], traces[i]
			}
		}
	}

	if len(traces) > limit {
		traces = traces[:limit]
	}

	return traces, nil
}

func (s *MemoryTraceStore) Count(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.traces)), nil
}

func (s *MemoryTraceStore) GetStats(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"total": len(s.traces),
		"by_state": map[string]int64{
			"INIT":            0,
			"CONSTRAINT_EVAL": 0,
			"EXECUTING":       0,
			"VALIDATION":      0,
			"FINALIZED":       0,
			"FAILED":          0,
		},
	}

	for _, trace := range s.traces {
		state := string(trace.ExecutionState)
		if count, ok := stats["by_state"].(map[string]int64)[state]; ok {
			stats["by_state"].(map[string]int64)[state] = count + 1
		}
	}

	return stats, nil
}

type MemoryBudgetStore struct {
	budget   float64
	reserved float64
	used     float64
	mu       sync.RWMutex
}

func NewMemoryBudgetStore() *MemoryBudgetStore {
	return &MemoryBudgetStore{
		budget:   100.0,
		reserved: 0,
		used:     0,
	}
}

func (s *MemoryBudgetStore) Reserve(ctx context.Context, sessionID string, amount float64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining := s.budget - s.reserved - s.used
	if remaining < amount {
		return false, nil
	}
	s.reserved += amount
	return true, nil
}

func (s *MemoryBudgetStore) Commit(ctx context.Context, sessionID string, amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used += amount
	s.reserved -= amount
	return nil
}

func (s *MemoryBudgetStore) Rollback(ctx context.Context, sessionID string, amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserved -= amount
	return nil
}

func (s *MemoryBudgetStore) GetRemaining(ctx context.Context, sessionID string) (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.budget - s.reserved - s.used, nil
}

func (s *MemoryBudgetStore) Get(ctx context.Context) (*Budget, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &Budget{
		TotalBudget: s.budget,
		UsedBudget:  s.used,
	}, nil
}

func (s *MemoryBudgetStore) Set(ctx context.Context, amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.budget = amount
	return nil
}

func (s *MemoryBudgetStore) SetBudget(ctx context.Context, sessionID string, amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.budget = amount
	return nil
}

type RiskReportStore interface {
	Save(ctx context.Context, report *domain.RiskReport) error
	Get(ctx context.Context, traceID string) (*domain.RiskReport, error)
	List(ctx context.Context, limit int) ([]*domain.RiskReport, error)
	Delete(ctx context.Context, traceID string) error
}

type MemoryRiskReportStore struct {
	reports map[string]*domain.RiskReport
	mu      sync.RWMutex
}

func NewMemoryRiskReportStore() *MemoryRiskReportStore {
	return &MemoryRiskReportStore{
		reports: make(map[string]*domain.RiskReport),
	}
}

func (s *MemoryRiskReportStore) Save(ctx context.Context, report *domain.RiskReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports[report.BaselineTraceID] = report
	return nil
}

func (s *MemoryRiskReportStore) Get(ctx context.Context, traceID string) (*domain.RiskReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	report, ok := s.reports[traceID]
	if !ok {
		return nil, fmt.Errorf("risk report not found: %s", traceID)
	}
	return report, nil
}

func (s *MemoryRiskReportStore) List(ctx context.Context, limit int) ([]*domain.RiskReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var reports []*domain.RiskReport
	for _, report := range s.reports {
		reports = append(reports, report)
	}

	if len(reports) > limit {
		reports = reports[:limit]
	}

	return reports, nil
}

func (s *MemoryRiskReportStore) Delete(ctx context.Context, traceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.reports, traceID)
	return nil
}

type APIKeyStore interface {
	Create(ctx context.Context, keyHash, keyPrefix, name, description string, expiresAt *int64, permissions []string) (*APIKey, error)
	List(ctx context.Context) ([]*APIKey, error)
	Delete(ctx context.Context, keyID string) error
	Validate(ctx context.Context, keyID string) (bool, error)
	Verify(ctx context.Context, key string) (*APIKey, error)
}

type APIKey struct {
	KeyID       string    `json:"key_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   *int64    `json:"expires_at,omitempty"`
	IsActive    bool      `json:"is_active"`
	KeyHash     string    `json:"-"`
	KeyPrefix   string    `json:"key_prefix,omitempty"`
	Permissions []string  `json:"permissions,omitempty"`
	LastUsedAt  *int64    `json:"last_used_at,omitempty"`
}

type MemoryAPIKeyStore struct {
	keys map[string]*APIKey
	mu   sync.RWMutex
}

func NewMemoryAPIKeyStore() *MemoryAPIKeyStore {
	return &MemoryAPIKeyStore{
		keys: make(map[string]*APIKey),
	}
}

func (s *MemoryAPIKeyStore) Create(ctx context.Context, keyHash, keyPrefix, name, description string, expiresAt *int64, permissions []string) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keyID := fmt.Sprintf("key_%d", time.Now().UnixNano())
	key := &APIKey{
		KeyID:       keyID,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
		IsActive:    true,
		Permissions: permissions,
	}
	s.keys[keyID] = key
	return key, nil
}

func (s *MemoryAPIKeyStore) List(ctx context.Context) ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []*APIKey
	for _, key := range s.keys {
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *MemoryAPIKeyStore) Delete(ctx context.Context, keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, keyID)
	return nil
}

func (s *MemoryAPIKeyStore) Validate(ctx context.Context, keyID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.keys[keyID]
	if !ok {
		return false, nil
	}
	return key.IsActive, nil
}

func (s *MemoryAPIKeyStore) Verify(ctx context.Context, key string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.keys {
		if k.IsActive && k.KeyHash == key {
			return k, nil
		}
	}
	return nil, fmt.Errorf("invalid api key")
}

// Helper functions for JSON serialization
func mustMarshal(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func mustUnmarshal(data string, v interface{}) {
	if data == "" {
		return
	}
	_ = json.Unmarshal([]byte(data), v)
}
