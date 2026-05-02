package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/tokenizer"
)

type BudgetService struct {
	defaultBufferPercent float64
	store                storage.BudgetStore
}

func NewBudgetService(store storage.BudgetStore) *BudgetService {
	return &BudgetService{
		defaultBufferPercent: 0.001,
		store:                store,
	}
}

func (s *BudgetService) GetRemaining(ctx context.Context, sessionID string) (float64, error) {
	if s.store != nil {
		return s.store.GetRemaining(ctx, sessionID)
	}
	return 0, nil
}

func (s *BudgetService) SetBudget(ctx context.Context, sessionID string, amount float64) error {
	if s.store != nil {
		return s.store.SetBudget(ctx, sessionID, amount)
	}
	return nil
}

func (s *BudgetService) Reserve(ctx context.Context, sessionID string, amount float64) (bool, error) {
	if s.store != nil {
		return s.store.Reserve(ctx, sessionID, amount)
	}
	return false, nil
}

func (s *BudgetService) Commit(ctx context.Context, sessionID string, amount float64) error {
	if s.store != nil {
		return s.store.Commit(ctx, sessionID, amount)
	}
	return nil
}

func (s *BudgetService) Rollback(ctx context.Context, sessionID string, amount float64) error {
	if s.store != nil {
		return s.store.Rollback(ctx, sessionID, amount)
	}
	return nil
}

type BudgetResult struct {
	Allowed       bool
	LimitUSD      float64
	ConsumedUSD   float64
	RemainingUSD  float64
	BufferApplied bool
}

func (s *BudgetService) CheckBudget(ctx context.Context, limitUSD, currentCostUSD float64) *BudgetResult {
	if limitUSD == 0 {
		return &BudgetResult{
			Allowed:       true,
			LimitUSD:      0,
			ConsumedUSD:   currentCostUSD,
			RemainingUSD:  0,
			BufferApplied: false,
		}
	}

	buffer := limitUSD * s.defaultBufferPercent
	effectiveLimit := limitUSD + buffer

	result := &BudgetResult{
		LimitUSD:      limitUSD,
		ConsumedUSD:   currentCostUSD,
		RemainingUSD:  effectiveLimit - currentCostUSD,
		BufferApplied: buffer > 0,
	}

	if currentCostUSD >= effectiveLimit {
		result.Allowed = false
		result.RemainingUSD = 0
	} else {
		result.Allowed = true
	}

	return result
}

func (r *BudgetResult) GetSafeErrorDetails() map[string]interface{} {
	return map[string]interface{}{
		"limit_usd":      r.LimitUSD,
		"buffer_applied": r.BufferApplied,
	}
}

func (s *BudgetService) ShouldHardStop(limitUSD, currentCostUSD float64) bool {
	buffer := limitUSD * s.defaultBufferPercent
	return currentCostUSD >= (limitUSD + buffer)
}

type PrivacyService struct {
	piiPatterns map[string]*regexp.Regexp
}

func NewPrivacyService() *PrivacyService {
	service := &PrivacyService{
		piiPatterns: make(map[string]*regexp.Regexp),
	}

	// 初始化 PII 检测模式
	service.piiPatterns["email"] = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	service.piiPatterns["phone"] = regexp.MustCompile(`(\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}`)
	service.piiPatterns["ssn"] = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	service.piiPatterns["credit_card"] = regexp.MustCompile(`\b(?:\d[ -]*?){13,16}\b`)
	service.piiPatterns["ip_address"] = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

	return service
}

type MaskingResult struct {
	OriginalContent  any
	MaskedContent    any
	MaskedFields     []string
	AlgorithmVersion string
}

type MaskingStrategy string

const (
	MaskingStrategyRegex               MaskingStrategy = "regex"
	MaskingStrategyPresidio            MaskingStrategy = "presidio"
	MaskingStrategySynthetic           MaskingStrategy = "synthetic"
	MaskingStrategyConstrainedDecoding MaskingStrategy = "constrained_decoding"
)

func (s *PrivacyService) ApplyMasking(ctx context.Context, level domain.PrivacyLevel, content any, fields []string) (*MaskingResult, error) {
	result := &MaskingResult{
		MaskedFields:     fields,
		AlgorithmVersion: "v1.0",
	}

	switch level {
	case domain.PrivacyMasked:
		result.OriginalContent = content
		result.MaskedContent = s.maskFieldsWithJSONPath(content, fields)
	case domain.PrivacyHashOnly:
		result.OriginalContent = content
		result.MaskedContent = s.hashContent(content)
	default:
		result.MaskedContent = content
	}

	return result, nil
}

func (s *PrivacyService) hashContent(content any) any {
	switch v := content.(type) {
	case string:
		hash := sha256.Sum256([]byte(v))
		return hex.EncodeToString(hash[:])
	case []string:
		hashed := make([]string, len(v))
		for i, str := range v {
			hash := sha256.Sum256([]byte(str))
			hashed[i] = hex.EncodeToString(hash[:])
		}
		return hashed
	default:
		// 对于其他类型，序列化为 JSON 后再哈希
		jsonBytes, err := json.Marshal(content)
		if err != nil {
			return content
		}
		hash := sha256.Sum256(jsonBytes)
		return hex.EncodeToString(hash[:])
	}
}

func (s *PrivacyService) ApplyMaskingWithStrategy(ctx context.Context, level domain.PrivacyLevel, strategy MaskingStrategy, content any, fields []string) (*MaskingResult, error) {
	result := &MaskingResult{
		MaskedFields:     fields,
		AlgorithmVersion: "v1.0",
	}

	switch level {
	case domain.PrivacyMasked:
		result.OriginalContent = content
		if strategy == MaskingStrategyRegex {
			result.MaskedContent = s.maskFieldsWithPII(content)
		} else {
			result.MaskedContent = s.maskFieldsWithJSONPath(content, fields)
		}
	case domain.PrivacyHashOnly:
		result.OriginalContent = content
		result.MaskedContent = s.hashContent(content)
	default:
		result.MaskedContent = content
	}

	return result, nil
}

// maskFieldsWithJSONPath 使用简化的 JSONPath 进行字段级脱敏
func (s *PrivacyService) maskFieldsWithJSONPath(content any, fields []string) any {
	// 如果没有指定字段，应用 PII 检测
	if len(fields) == 0 {
		return s.maskFieldsWithPII(content)
	}

	// 将内容序列化为 JSON 后再处理，这样可以处理任意结构
	jsonBytes, err := json.Marshal(content)
	if err != nil {
		return content
	}

	var data any
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return content
	}

	// 处理每个指定的字段
	for _, fieldPath := range fields {
		fieldPath = strings.TrimSpace(fieldPath)
		if fieldPath == "" {
			continue
		}
		s.maskFieldByPath(&data, fieldPath)
	}

	return data
}

// maskFieldByPath 根据路径脱敏字段
func (s *PrivacyService) maskFieldByPath(data *any, path string) {
	// 简化的路径处理，支持 $.field, field, array[*].field
	path = strings.TrimPrefix(path, "$.")
	parts := strings.Split(path, ".")

	s.traverseAndMask(data, parts, 0)
}

func (s *PrivacyService) traverseAndMask(data *any, parts []string, index int) {
	if index >= len(parts) {
		// 到达目标节点，脱敏它
		*data = s.maskValue(*data)
		return
	}

	part := parts[index]

	switch v := (*data).(type) {
	case map[string]any:
		// 处理通配符
		if part == "*" {
			for key := range v {
				subData := v[key]
				s.traverseAndMask(&subData, parts, index+1)
				v[key] = subData
			}
		} else if val, exists := v[part]; exists {
			s.traverseAndMask(&val, parts, index+1)
			v[part] = val
		}
	case []any:
		// 处理数组通配符
		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			inner := strings.Trim(part, "[]")
			if inner == "*" || inner == "" {
				// 对所有元素应用余下的路径
				for i, elem := range v {
					s.traverseAndMask(&elem, parts, index+1)
					v[i] = elem
				}
			} else {
				// 指定索引
				if idx, err := parseArrayIndex(inner); err == nil && idx >= 0 && idx < len(v) {
					s.traverseAndMask(&v[idx], parts, index+1)
				}
			}
		} else if part == "*" {
			// 对数组中所有元素应用余下的路径
			for i, elem := range v {
				s.traverseAndMask(&elem, parts, index+1)
				v[i] = elem
			}
		}
	}
}

func parseArrayIndex(s string) (int, error) {
	var idx int
	_, err := fmt.Sscanf(s, "%d", &idx)
	return idx, err
}

func (s *PrivacyService) maskValue(value any) any {
	switch v := value.(type) {
	case string:
		return "[REDACTED]"
	case []any:
		// 递归脱敏数组中所有值
		masked := make([]any, len(v))
		for i, elem := range v {
			masked[i] = s.maskValue(elem)
		}
		return masked
	case map[string]any:
		// 递归脱敏对象中所有值
		masked := make(map[string]any)
		for k, v := range v {
			masked[k] = s.maskValue(v)
		}
		return masked
	default:
		return "[REDACTED]"
	}
}

// maskFieldsWithPII 使用正则表达式检测并脱敏 PII 信息
func (s *PrivacyService) maskFieldsWithPII(content any) any {
	switch v := content.(type) {
	case string:
		return s.maskStringWithPII(v)
	case []string:
		masked := make([]string, len(v))
		for i, str := range v {
			masked[i] = s.maskStringWithPII(str)
		}
		return masked
	case []any:
		masked := make([]any, len(v))
		for i, elem := range v {
			masked[i] = s.maskFieldsWithPII(elem)
		}
		return masked
	case map[string]any:
		masked := make(map[string]any)
		for k, val := range v {
			masked[k] = s.maskFieldsWithPII(val)
		}
		return masked
	default:
		// 对于其他类型，尝试序列化为字符串再处理
		jsonStr, err := json.Marshal(content)
		if err != nil {
			return content
		}
		maskedStr := s.maskStringWithPII(string(jsonStr))
		// 尝试反序列化回原类型
		var result any
		if err := json.Unmarshal([]byte(maskedStr), &result); err == nil {
			return result
		}
		return maskedStr
	}
}

func (s *PrivacyService) maskStringWithPII(str string) string {
	result := str
	// 应用所有 PII 模式
	for patternName, pattern := range s.piiPatterns {
		var replacement string
		switch patternName {
		case "email":
			replacement = "[EMAIL_REDACTED]"
		case "phone":
			replacement = "[PHONE_REDACTED]"
		case "ssn":
			replacement = "[SSN_REDACTED]"
		case "credit_card":
			replacement = "[CREDIT_CARD_REDACTED]"
		case "ip_address":
			replacement = "[IP_REDACTED]"
		default:
			replacement = "[REDACTED]"
		}
		result = pattern.ReplaceAllString(result, replacement)
	}
	return result
}

func (s *PrivacyService) maskFields(content any, fields []string) any {
	if len(fields) == 0 {
		return s.maskFieldsWithPII(content)
	}
	return s.maskFieldsWithJSONPath(content, fields)
}

type DeprecationService struct{}

func NewDeprecationService() *DeprecationService {
	return &DeprecationService{}
}

func (s *DeprecationService) IsDeprecated(feature string) bool {
	deprecatedFeatures := map[string]bool{
		"legacy-auth": true,
	}
	return deprecatedFeatures[feature]
}

func (s *DeprecationService) GetDeprecationWarning(feature string) string {
	return fmt.Sprintf("Feature '%s' is deprecated and will be removed in a future version", feature)
}

type DiffService struct{}

func NewDiffService() *DiffService {
	return &DiffService{}
}

func (s *DiffService) Compare(a, b *domain.Trace) (bool, []string, error) {
	if a == nil || b == nil {
		return false, nil, nil
	}

	var diffFields []string

	if a.Model != b.Model {
		diffFields = append(diffFields, "model")
	}

	if a.ExecutionState != b.ExecutionState {
		diffFields = append(diffFields, "execution_state")
	}

	return len(diffFields) > 0, diffFields, nil
}

func (s *DiffService) GenerateDiffReport(a, b *domain.Trace) (string, error) {
	hasDiff, fields, err := s.Compare(a, b)
	if err != nil {
		return "", err
	}

	if !hasDiff {
		return "No differences found", nil
	}

	report := "Differences found in fields: "
	report += strings.Join(fields, ", ")
	return report, nil
}

type TokenizerService struct {
	tokenizer tokenizer.Tokenizer
}

func NewTokenizerService() *TokenizerService {
	tok, _ := tokenizer.DefaultTokenizer()
	return &TokenizerService{tokenizer: tok}
}

func (s *TokenizerService) CountTokens(text string) (int, error) {
	if s.tokenizer == nil {
		return 0, fmt.Errorf("tokenizer not initialized")
	}
	return s.tokenizer.CountTokens(text)
}

func (s *TokenizerService) Truncate(text string, maxTokens int) (string, error) {
	if s.tokenizer == nil {
		return text, nil
	}

	tokens, err := s.tokenizer.CountTokens(text)
	if err != nil {
		return text, err
	}

	if tokens <= maxTokens {
		return text, nil
	}

	runes := []rune(text)
	for i := len(runes); i > 0; i-- {
		testText := string(runes[:i])
		testTokens, _ := s.tokenizer.CountTokens(testText)
		if testTokens <= maxTokens {
			return testText, nil
		}
	}

	return "", nil
}
