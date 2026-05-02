package upstream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// SSEReader 是一个SSE（Server-Sent Events）解析器
type SSEReader struct {
	reader *bufio.Reader
	buffer bytes.Buffer
}

// NewSSEReader 创建一个新的SSE解析器
func NewSSEReader(reader io.Reader) *SSEReader {
	return &SSEReader{
		reader: bufio.NewReader(reader),
	}
}

// Read 从底层reader读取数据到缓冲区
func (r *SSEReader) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

// ParseEvent 解析SSE事件
func (r *SSEReader) ParseEvent(data []byte) (StreamEvent, error) {
	r.buffer.Write(data)

	// 按行分割数据
	lines := strings.Split(r.buffer.String(), "\n")
	var eventType string
	var eventData []string

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			// 空行表示事件结束
			if len(eventData) > 0 {
				// 处理事件数据
				dataStr := strings.Join(eventData, "\n")
				r.buffer.Reset()
				// 保留未处理的行
				for j := i + 1; j < len(lines); j++ {
					r.buffer.WriteString(lines[j])
					if j < len(lines)-1 {
						r.buffer.WriteString("\n")
					}
				}
				return r.processEvent(eventType, dataStr)
			}
		} else if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(line[6:])
		} else if strings.HasPrefix(line, "data:") {
			eventData = append(eventData, strings.TrimSpace(line[5:]))
		}
	}

	// 没有完整的事件，返回空事件
	return StreamEvent{Done: false}, nil
}

// processEvent 处理SSE事件数据
func (r *SSEReader) processEvent(eventType, data string) (StreamEvent, error) {
	event := StreamEvent{Done: false}

	// 解析OpenAI风格的流式响应
	var openAIResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int `json:"index"`
			Delta        struct {
				Role    string `json:"role,omitempty"`
				Content string `json:"content,omitempty"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason,omitempty"`
		} `json:"choices"`
		Usage *Usage `json:"usage,omitempty"`
	}

	if err := json.Unmarshal([]byte(data), &openAIResp); err != nil {
		// 解析失败，直接返回原始数据
		event.Content = data
		return event, nil
	}

	// 处理OpenAI响应
	if len(openAIResp.Choices) > 0 {
		choice := openAIResp.Choices[0]
		event.Content = choice.Delta.Content

		// 检查是否完成
		if choice.FinishReason != "" {
			event.Done = true
		}

		// 处理usage信息
		if openAIResp.Usage != nil {
			event.Usage = openAIResp.Usage
		}
	}

	return event, nil
}
