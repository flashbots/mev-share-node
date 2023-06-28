// Package jsonrpcserver allows exposing functions like:
// func Foo(context, int) (int, error)
// as a JSON RPC methods
//
// This implementation is similar to the one in go-ethereum, but the idea is to eventually replace it as a default
// JSON RPC server implementation in Flasbhots projects and for this we need to reimplement some of the quirks of existing API.
package jsonrpcserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

var (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeCustomError    = -32000
)

const (
	maxOriginIDLength = 255
)

type (
	highPriorityKey struct{}
	signerKey       struct{}
	originKey       struct{}
)

type JSONRPCRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      any               `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      any              `json:"id"`
	Result  *json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError    `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *any   `json:"data,omitempty"`
}

type Handler struct {
	methods map[string]methodHandler
}

type Methods map[string]interface{}

// NewHandler creates JSONRPC http.Handler from the map that maps method names to method functions
// each method function must:
// - have context as a first argument
// - return error as a last argument
// - have argument types that can be unmarshalled from JSON
// - have return types that can be marshalled to JSON
func NewHandler(methods Methods) (*Handler, error) {
	m := make(map[string]methodHandler)
	for name, fn := range methods {
		method, err := getMethodTypes(fn)
		if err != nil {
			return nil, err
		}
		m[name] = method
	}
	return &Handler{
		methods: m,
	}, nil
}

func writeJSONRPCError(w http.ResponseWriter, id any, code int, msg string) {
	res := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  nil,
		Error: &JSONRPCError{
			Code:    code,
			Message: msg,
			Data:    nil,
		},
	}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// read request
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, CodeParseError, err.Error())
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSONRPCError(w, req.ID, CodeParseError, "invalid jsonrpc version")
		return
	}
	if req.ID != nil {
		// id must be string or number
		switch req.ID.(type) {
		case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		default:
			writeJSONRPCError(w, req.ID, CodeParseError, "invalid id type")
		}
	}

	highPriority := r.Header.Get("high_prio") == "true"
	ctx := context.WithValue(r.Context(), highPriorityKey{}, highPriority)

	signature := r.Header.Get("x-flashbots-signature")
	if split := strings.Split(signature, ":"); len(split) > 0 {
		signer := common.HexToAddress(split[0])
		ctx = context.WithValue(ctx, signerKey{}, signer)
	}

	origin := r.Header.Get("x-flashbots-origin")
	if origin != "" {
		if len(origin) > maxOriginIDLength {
			writeJSONRPCError(w, req.ID, CodeInvalidRequest, "x-flashbots-origin header is too long")
			return
		}
		ctx = context.WithValue(ctx, originKey{}, origin)
	}

	// get method
	method, ok := h.methods[req.Method]
	if !ok {
		writeJSONRPCError(w, req.ID, CodeMethodNotFound, "method not found")
		return
	}

	// call method
	result, err := method.call(ctx, req.Params)
	if err != nil {
		writeJSONRPCError(w, req.ID, CodeCustomError, err.Error())
		return
	}

	marshaledResult, err := json.Marshal(result)
	if err != nil {
		writeJSONRPCError(w, req.ID, CodeInternalError, err.Error())
		return
	}

	// write response
	rawMessageResult := json.RawMessage(marshaledResult)
	res := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  &rawMessageResult,
		Error:   nil,
	}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func GetPriority(ctx context.Context) bool {
	value, ok := ctx.Value(highPriorityKey{}).(bool)
	if !ok {
		return false
	}
	return value
}

func GetSigner(ctx context.Context) common.Address {
	value, ok := ctx.Value(signerKey{}).(common.Address)
	if !ok {
		return common.Address{}
	}
	return value
}

func GetOrigin(ctx context.Context) string {
	value, ok := ctx.Value(originKey{}).(string)
	if !ok {
		return ""
	}
	return value
}
