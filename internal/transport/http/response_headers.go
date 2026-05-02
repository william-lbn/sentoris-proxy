package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sentoris-ai/sentoris-proxy/pkg/errors"
)

func (h *Handler) setResponseHeaders(w http.ResponseWriter, traceID string, costUSD float64, budgetRemaining *float64, acceptedCapabilities []string) {
	w.Header().Set("Sentoris-Version", "1.0")
	w.Header().Set("Sentoris-Trace-Id", traceID)
	w.Header().Set("Sentoris-Cost-Consumed", fmt.Sprintf("%.6f", costUSD))

	if budgetRemaining != nil {
		w.Header().Set("Sentoris-Budget-Remaining", fmt.Sprintf("%.6f", *budgetRemaining))
	}

	if len(acceptedCapabilities) > 0 {
		w.Header().Set("Sentoris-Accepted", strings.Join(acceptedCapabilities, ","))
	}
}

func (h *Handler) setErrorResponseHeaders(w http.ResponseWriter, traceID string, errCode errors.ErrorCode) {
	w.Header().Set("Sentoris-Version", "1.0")
	if traceID != "" {
		w.Header().Set("Sentoris-Trace-Id", traceID)
	}
	w.Header().Set("Sentoris-Accepted", "")
}

func (h *Handler) setTruncatedResponseHeaders(w http.ResponseWriter, traceID string, costUSD float64, truncated bool) {
	h.setResponseHeaders(w, traceID, costUSD, nil, nil)
	if truncated {
		w.Header().Set("Sentoris-Truncated", "true")
	}
}

func (h *Handler) setReplayResponseHeaders(w http.ResponseWriter, traceID string, baselineTraceID string, modelChanged bool) {
	w.Header().Set("Sentoris-Version", "1.0")
	w.Header().Set("Sentoris-Trace-Id", traceID)
	if baselineTraceID != "" {
		w.Header().Set("Sentoris-Baseline-Ref", baselineTraceID)
	}
	if modelChanged {
		w.Header().Set("Sentoris-Model-Changed", "true")
	}
}

func WriteSSEError(w http.ResponseWriter, code string, message string, traceID string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	errorData := map[string]interface{}{
		"error": map[string]interface{}{
			"code":     code,
			"message":  message,
			"trace_id": traceID,
		},
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	jsonBytes, _ := json.Marshal(errorData)
	fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
	flusher.Flush()

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	return nil
}
