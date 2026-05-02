package governance

import (
	"context"
	"regexp"
	"strings"
)

type InputSanitizer struct {
	injectionPatterns []*regexp.Regexp
}

func NewInputSanitizer() *InputSanitizer {
	return &InputSanitizer{
		injectionPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(ignore\s+previous|ignore\s+all|disregard\s+previous|disregard\s+all|reset\s+prompt|forget\s+instructions)`),
			regexp.MustCompile(`(?i)(system\s+prompt|system\s+role|as\s+a\s+system)`),
			regexp.MustCompile(`(?i)(execute\s+command|run\s+code|python\s+code|javascript\s+code)`),
			regexp.MustCompile(`(?i)(override\s+settings|bypass\s+filter|disable\s+security)`),
			regexp.MustCompile(`(?i)(prompt\s+injection|jailbreak|escape\s+prompt)`),
			regexp.MustCompile(`(?i)(BEGIN\s+SYSTEM|END\s+SYSTEM|<system>.*</system>)`),
		},
	}
}

type SanitizationResult struct {
	IsClean       bool
	OriginalInput string
	SanitizedInput string
	Warnings      []string
	Blocked       bool
	BlockReason   string
}

func (s *InputSanitizer) Sanitize(ctx context.Context, input string) *SanitizationResult {
	result := &SanitizationResult{
		IsClean:       true,
		OriginalInput: input,
		SanitizedInput: input,
		Warnings:      []string{},
		Blocked:       false,
	}

	for _, pattern := range s.injectionPatterns {
		if pattern.MatchString(input) {
			result.IsClean = false
			result.Warnings = append(result.Warnings, "Potential injection pattern detected: "+pattern.String())
			
			if s.shouldBlock(pattern) {
				result.Blocked = true
				result.BlockReason = "Detected malicious injection attempt"
				return result
			}
		}
	}

	sanitized := s.applyBasicSanitization(input)
	result.SanitizedInput = sanitized

	return result
}

func (s *InputSanitizer) SanitizeMessages(ctx context.Context, messages []map[string]string) []map[string]string {
	sanitizedMessages := make([]map[string]string, len(messages))
	
	for i, msg := range messages {
		sanitizedMsg := make(map[string]string)
		for k, v := range msg {
			if strings.ToLower(k) == "content" || strings.ToLower(k) == "prompt" {
				result := s.Sanitize(ctx, v)
				if result.Blocked {
					sanitizedMsg[k] = "[BLOCKED]"
				} else {
					sanitizedMsg[k] = result.SanitizedInput
				}
			} else {
				sanitizedMsg[k] = v
			}
		}
		sanitizedMessages[i] = sanitizedMsg
	}
	
	return sanitizedMessages
}

func (s *InputSanitizer) shouldBlock(pattern *regexp.Regexp) bool {
	blockingPatterns := []string{
		"ignore previous",
		"ignore all",
		"disregard previous",
		"disregard all",
		"override settings",
		"bypass filter",
		"disable security",
	}
	
	patternStr := pattern.String()
	for _, bp := range blockingPatterns {
		if strings.Contains(strings.ToLower(patternStr), bp) {
			return true
		}
	}
	return false
}

func (s *InputSanitizer) applyBasicSanitization(input string) string {
	sanitized := input
	
	sanitized = strings.ReplaceAll(sanitized, "<script>", "[REDACTED]")
	sanitized = strings.ReplaceAll(sanitized, "</script>", "[REDACTED]")
	sanitized = strings.ReplaceAll(sanitized, "<iframe>", "[REDACTED]")
	sanitized = strings.ReplaceAll(sanitized, "</iframe>", "[REDACTED]")
	
	sanitized = strings.ReplaceAll(sanitized, "javascript:", "[REDACTED]")
	sanitized = strings.ReplaceAll(sanitized, "data:", "[REDACTED]")
	
	return sanitized
}

func (s *InputSanitizer) RedactForLogging(data string) string {
	if data == "" {
		return "[REDACTED]"
	}
	
	if len(data) > 50 {
		return "[REDACTED]"
	}
	
	return "[REDACTED]"
}

func (s *InputSanitizer) RedactFieldsForLogging(fields map[string]interface{}) map[string]interface{} {
	redacted := make(map[string]interface{})
	
	for k, v := range fields {
		sensitiveFields := []string{"prompt", "response", "content", "message", "input", "output", "api_key", "password", "secret"}
		
		isSensitive := false
		for _, sf := range sensitiveFields {
			if strings.Contains(strings.ToLower(k), sf) {
				isSensitive = true
				break
			}
		}
		
		if isSensitive {
			redacted[k] = "[REDACTED]"
		} else {
			redacted[k] = v
		}
	}
	
	return redacted
}

func (s *InputSanitizer) IsPotentiallyMalicious(input string) bool {
	result := s.Sanitize(context.Background(), input)
	return !result.IsClean || result.Blocked
}