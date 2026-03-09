// Input: echo, encoding/json
// Output: tRPC 协议处理引擎 (HandleTRPC/HandleTRPCMutation/HandleTRPCBatch)
// Role: tRPC 兼容层核心，解析 query/mutation/batch 请求，路由到注册的 procedure handler
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- tRPC protocol types ---

type TRPCRequest struct {
	JSON json.RawMessage `json:"json"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

type TRPCResponse struct {
	Result TRPCResult `json:"result"`
}

type TRPCResult struct {
	Data TRPCData `json:"data"`
}

type TRPCData struct {
	JSON interface{} `json:"json"`
}

// TRPCErrorResponse wraps error in superjson format for tRPC v11 兼容
type TRPCErrorResponse struct {
	Error TRPCSuperJSONError `json:"error"`
}

// TRPCSuperJSONError 使用 superjson 格式包装错误（tRPC v11 要求 error 也经过 transformer 序列化）
type TRPCSuperJSONError struct {
	JSON TRPCErrorData `json:"json"`
}

type TRPCErrorData struct {
	Message string         `json:"message"`
	Code    int            `json:"code"`
	Data    *TRPCErrorInfo `json:"data,omitempty"`
}

type TRPCErrorInfo struct {
	Code       string `json:"code"`
	HTTPStatus int    `json:"httpStatus"`
}

// ProcedureFunc handles a tRPC procedure call.
type ProcedureFunc func(c echo.Context, input json.RawMessage) (interface{}, error)

// SubscriptionFunc handles a tRPC subscription (SSE streaming).
// It receives a channel to send data events; close the channel when done.
type SubscriptionFunc func(c echo.Context, input json.RawMessage, emit chan<- interface{})

// procedureRegistry maps "router.procedure" to handler functions.
type procedureRegistry map[string]ProcedureFunc

// subscriptionRegistry maps "router.procedure" to subscription handler functions.
type subscriptionRegistry map[string]SubscriptionFunc

// trpcErr is a typed error for tRPC responses.
type trpcErr struct {
	message string
	code    string
	status  int
}

func (e *trpcErr) Error() string { return e.message }

// --- tRPC protocol handler ---

func (h *Handler) HandleTRPC(c echo.Context) error {
	procedures := c.Param("procedures")
	isBatch := c.QueryParam("batch") == "1"

	if procedures == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "No procedure specified")
	}

	procNames := strings.Split(procedures, ",")

	if isBatch {
		return h.handleBatchTRPC(c, procNames)
	}

	if len(procNames) != 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "Multiple procedures require batch=1")
	}

	input, err := h.extractInput(c, "")
	if err != nil {
		return h.trpcError(c, err.Error(), "BAD_REQUEST", http.StatusBadRequest)
	}

	// Check if this is a subscription request
	subs := h.buildSubscriptionRegistry()
	if subFn, ok := subs[procNames[0]]; ok {
		return h.handleSSESubscription(c, procNames[0], input, subFn)
	}

	result, err := h.callProcedure(c, procNames[0], input)
	if err != nil {
		return h.handleProcedureError(c, err)
	}

	return c.JSON(http.StatusOK, TRPCResponse{
		Result: TRPCResult{Data: TRPCData{JSON: result}},
	})
}

func (h *Handler) handleBatchTRPC(c echo.Context, procNames []string) error {
	results := make([]interface{}, len(procNames))

	for i, proc := range procNames {
		idx := fmt.Sprintf("%d", i)
		input, err := h.extractInput(c, idx)
		if err != nil {
			results[i] = TRPCErrorResponse{
				Error: TRPCSuperJSONError{
					JSON: TRPCErrorData{
						Message: err.Error(),
						Code:    -32600,
						Data:    &TRPCErrorInfo{Code: "BAD_REQUEST", HTTPStatus: 400},
					},
				},
			}
			continue
		}

		result, err := h.callProcedure(c, proc, input)
		if err != nil {
			results[i] = h.buildErrorResult(err)
		} else {
			results[i] = TRPCResponse{
				Result: TRPCResult{Data: TRPCData{JSON: result}},
			}
		}
	}

	return c.JSON(http.StatusOK, results)
}

func (h *Handler) extractInput(c echo.Context, batchIdx string) (json.RawMessage, error) {
	if c.Request().Method == http.MethodGet {
		inputStr := c.QueryParam("input")
		if inputStr == "" {
			return nil, nil
		}

		decoded, err := url.QueryUnescape(inputStr)
		if err != nil {
			decoded = inputStr
		}

		if batchIdx != "" {
			var batchInput map[string]TRPCRequest
			if err := json.Unmarshal([]byte(decoded), &batchInput); err != nil {
				return nil, fmt.Errorf("invalid batch input: %w", err)
			}
			if req, ok := batchInput[batchIdx]; ok {
				return req.JSON, nil
			}
			return nil, nil
		}

		var req TRPCRequest
		if err := json.Unmarshal([]byte(decoded), &req); err != nil {
			return []byte(decoded), nil
		}
		if req.JSON != nil {
			return req.JSON, nil
		}
		return []byte(decoded), nil
	}

	// POST
	body := make(map[string]json.RawMessage)
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return nil, nil
	}

	if batchIdx != "" {
		raw, ok := body[batchIdx]
		if !ok {
			return nil, nil
		}
		var req TRPCRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return raw, nil
		}
		if req.JSON != nil {
			return req.JSON, nil
		}
		return raw, nil
	}

	if jsonData, ok := body["json"]; ok {
		return jsonData, nil
	}
	all, _ := json.Marshal(body)
	return all, nil
}

func (h *Handler) callProcedure(c echo.Context, name string, input json.RawMessage) (interface{}, error) {
	registry := h.buildRegistry()

	fn, ok := registry[name]
	if !ok {
		return nil, &trpcErr{message: fmt.Sprintf("No procedure '%s' found", name), code: "NOT_FOUND", status: 404}
	}

	return fn(c, input)
}

func (h *Handler) trpcError(c echo.Context, message, code string, status int) error {
	return c.JSON(status, TRPCErrorResponse{
		Error: TRPCSuperJSONError{
			JSON: TRPCErrorData{
				Message: message,
				Code:    -32600,
				Data:    &TRPCErrorInfo{Code: code, HTTPStatus: status},
			},
		},
	})
}

func (h *Handler) handleProcedureError(c echo.Context, err error) error {
	if te, ok := err.(*trpcErr); ok {
		return h.trpcError(c, te.message, te.code, te.status)
	}
	return h.trpcError(c, err.Error(), "INTERNAL_SERVER_ERROR", 500)
}

func (h *Handler) buildErrorResult(err error) interface{} {
	code := "INTERNAL_SERVER_ERROR"
	status := 500
	if te, ok := err.(*trpcErr); ok {
		code = te.code
		status = te.status
	}
	return TRPCErrorResponse{
		Error: TRPCSuperJSONError{
			JSON: TRPCErrorData{
				Message: err.Error(),
				Code:    -32600,
				Data:    &TRPCErrorInfo{Code: code, HTTPStatus: status},
			},
		},
	}
}

// handleSSESubscription handles tRPC subscription procedures via Server-Sent Events.
func (h *Handler) handleSSESubscription(c echo.Context, name string, input json.RawMessage, subFn SubscriptionFunc) error {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	emit := make(chan interface{}, 64)
	go subFn(c, input, emit)

	flusher, _ := w.Writer.(http.Flusher)
	id := 1

	// Send connected event
	connEvent := map[string]interface{}{
		"id":      id,
		"jsonrpc": "2.0",
		"result": map[string]interface{}{
			"type": "started",
		},
	}
	connData, _ := json.Marshal(connEvent)
	fmt.Fprintf(w, "id: %d\ndata: %s\n\n", id, connData)
	if flusher != nil {
		flusher.Flush()
	}
	id++

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case data, ok := <-emit:
			if !ok {
				// Channel closed, send stopped event
				stopEvent := map[string]interface{}{
					"id":      id,
					"jsonrpc": "2.0",
					"result": map[string]interface{}{
						"type": "stopped",
					},
				}
				stopData, _ := json.Marshal(stopEvent)
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", id, stopData)
				if flusher != nil {
					flusher.Flush()
				}
				return nil
			}
			event := map[string]interface{}{
				"id":      id,
				"jsonrpc": "2.0",
				"result": map[string]interface{}{
					"type": "data",
					"data": data,
				},
			}
			eventData, _ := json.Marshal(event)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", id, eventData)
			if flusher != nil {
				flusher.Flush()
			}
			id++
		}
	}
}

// buildSubscriptionRegistry creates the subscription procedure registry.
func (h *Handler) buildSubscriptionRegistry() subscriptionRegistry {
	s := make(subscriptionRegistry)
	h.registerSubscriptionsTRPC(s)
	return s
}
