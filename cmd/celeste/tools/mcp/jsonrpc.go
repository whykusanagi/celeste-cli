// cmd/celeste/tools/mcp/jsonrpc.go
package mcp

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// idCounter provides unique IDs for JSON-RPC requests.
var idCounter atomic.Int64

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no ID, no response expected).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.Number     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// ErrorObject is the error payload in a JSON-RPC 2.0 response.
type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *ErrorObject) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// NewRequest creates a new JSON-RPC 2.0 request with an auto-incremented ID.
// params is marshaled to JSON. Pass nil for no params.
func NewRequest(method string, params any) (*Request, error) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      idCounter.Add(1),
		Method:  method,
	}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = data
	}
	return req, nil
}

// NewNotification creates a JSON-RPC 2.0 notification (no ID).
func NewNotification(method string) *Notification {
	return &Notification{
		JSONRPC: "2.0",
		Method:  method,
	}
}
