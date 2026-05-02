package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Logger 封装了slog.Logger，添加trace_id支持
type Logger struct {
	logger *slog.Logger
}

// NewLogger 创建一个新的日志记录器
func NewLogger(level string) *Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)

	return &Logger{
		logger: logger,
	}
}

// WithTraceID 添加trace_id到日志上下文
func (l *Logger) WithTraceID(traceID string) *Logger {
	return &Logger{
		logger: l.logger.With("trace_id", traceID),
	}
}

// WithContext 从context中提取trace_id并添加到日志上下文
func (l *Logger) WithContext(ctx context.Context) *Logger {
	traceID := ctx.Value("trace_id")
	if traceID != nil {
		return l.WithTraceID(traceID.(string))
	}
	return l
}

// Debug 记录调试日志
func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// Info 记录信息日志
func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// Warn 记录警告日志
func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// Error 记录错误日志
func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// SanitizeSensitiveData 脱敏敏感数据
func (l *Logger) SanitizeSensitiveData(data string) string {
	// 简单的脱敏逻辑，实际应用中需要更复杂的规则
	if strings.Contains(data, "password") {
		return strings.ReplaceAll(data, "password", "******")
	}
	if strings.Contains(data, "token") {
		return strings.ReplaceAll(data, "token", "******")
	}
	if strings.Contains(data, "secret") {
		return strings.ReplaceAll(data, "secret", "******")
	}
	if strings.Contains(data, "key") {
		return strings.ReplaceAll(data, "key", "******")
	}
	return data
}

// SanitizeMap 脱敏map中的敏感数据
func (l *Logger) SanitizeMap(data map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range data {
		key := strings.ToLower(k)
		if strings.Contains(key, "password") ||
			strings.Contains(key, "token") ||
			strings.Contains(key, "secret") ||
			strings.Contains(key, "key") ||
			strings.Contains(key, "auth") {
			result[k] = "******"
		} else {
			result[k] = v
		}
	}
	return result
}

// GetLogger 获取底层的slog.Logger
func (l *Logger) GetLogger() *slog.Logger {
	return l.logger
}

// 全局日志实例
var globalLogger *Logger

// InitLogger 初始化全局日志记录器
func InitLogger(level string) {
	globalLogger = NewLogger(level)
}

// GetGlobalLogger 获取全局日志记录器
func GetGlobalLogger() *Logger {
	if globalLogger == nil {
		globalLogger = NewLogger("info")
	}
	return globalLogger
}

// Debug 全局调试日志
func Debug(msg string, args ...any) {
	GetGlobalLogger().Debug(msg, args...)
}

// Info 全局信息日志
func Info(msg string, args ...any) {
	GetGlobalLogger().Info(msg, args...)
}

// Warn 全局警告日志
func Warn(msg string, args ...any) {
	GetGlobalLogger().Warn(msg, args...)
}

// Error 全局错误日志
func Error(msg string, args ...any) {
	GetGlobalLogger().Error(msg, args...)
}
