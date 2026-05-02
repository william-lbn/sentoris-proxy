package upstream

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent 表示一个Server-Sent Event
type SSEEvent struct {
	Type string
	Data string
}

// SSEDecoder 用于解码Server-Sent Events
type SSEDecoder struct {
	scanner *bufio.Scanner
}

// NewSSEDecoder 创建一个新的SSE解码器
func NewSSEDecoder(r io.Reader) *SSEDecoder {
	return &SSEDecoder{
		scanner: bufio.NewScanner(r),
	}
}

// Decode 解码一个SSE事件
func (d *SSEDecoder) Decode() (SSEEvent, error) {
	var event SSEEvent
	event.Type = "message"

	for d.scanner.Scan() {
		line := d.scanner.Text()

		// 空行表示事件结束
		if line == "" {
			return event, nil
		}

		// 注释行
		if strings.HasPrefix(line, ":") {
			continue
		}

		// 解析字段
		parts := strings.SplitN(line, ":", 2)
		field := parts[0]
		var value string
		if len(parts) > 1 {
			// 移除可能的空格
			value = strings.TrimPrefix(parts[1], " ")
		}

		switch field {
		case "event":
			event.Type = value
		case "data":
			event.Data += value + "\n"
		}
	}

	if err := d.scanner.Err(); err != nil {
		return SSEEvent{}, err
	}

	// 检查是否有数据
	if event.Data != "" {
		// 移除最后的换行符
		event.Data = strings.TrimSuffix(event.Data, "\n")
		return event, nil
	}

	return SSEEvent{}, io.EOF
}
