package tokenizer

import (
	"fmt"

	"github.com/pkoukk/tiktoken-go"
)

// Tokenizer 是一个tokenizer接口
type Tokenizer interface {
	CountTokens(text string) (int, error)
	GetModel() string
}

// OpenAITokenizer 是OpenAI模型的tokenizer
type OpenAITokenizer struct {
	model     string
	tokenizer *tiktoken.Tiktoken
}

// NewOpenAITokenizer 创建一个新的OpenAI tokenizer
func NewOpenAITokenizer(model string) (*OpenAITokenizer, error) {
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		// 如果模型不存在，使用默认的cl100k_base编码
		enc, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return nil, fmt.Errorf("failed to get encoding: %w", err)
		}
	}

	return &OpenAITokenizer{
		model:     model,
		tokenizer: enc,
	}, nil
}

// CountTokens 计算文本的token数量
func (t *OpenAITokenizer) CountTokens(text string) (int, error) {
	tokens := t.tokenizer.Encode(text, nil, nil)
	return len(tokens), nil
}

// GetModel 返回tokenizer对应的模型
func (t *OpenAITokenizer) GetModel() string {
	return t.model
}

// DefaultTokenizer 创建一个默认的tokenizer
func DefaultTokenizer() (*OpenAITokenizer, error) {
	return NewOpenAITokenizer("gpt-3.5-turbo")
}
