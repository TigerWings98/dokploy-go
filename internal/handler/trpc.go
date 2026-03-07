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

type TRPCErrorResponse struct {
	Error TRPCErrorData `json:"error"`
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

// procedureRegistry maps "router.procedure" to handler functions.
type procedureRegistry map[string]ProcedureFunc

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
				Error: TRPCErrorData{
					Message: err.Error(),
					Code:    -32600,
					Data:    &TRPCErrorInfo{Code: "BAD_REQUEST", HTTPStatus: 400},
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
		Error: TRPCErrorData{
			Message: message,
			Code:    -32600,
			Data:    &TRPCErrorInfo{Code: code, HTTPStatus: status},
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
		Error: TRPCErrorData{
			Message: err.Error(),
			Code:    -32600,
			Data:    &TRPCErrorInfo{Code: code, HTTPStatus: status},
		},
	}
}
