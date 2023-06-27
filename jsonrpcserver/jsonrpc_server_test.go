package jsonrpcserver

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandler_ServeHTTP(t *testing.T) {
	var (
		errorArg = -1
		errorOut = errors.New("custom error") //nolint:goerr113
	)
	handlerMethod := func(ctx context.Context, arg1 int) (dummyStruct, error) {
		if arg1 == errorArg {
			return dummyStruct{}, errorOut
		}
		return dummyStruct{arg1}, nil
	}

	handler, err := NewHandler(map[string]interface{}{
		"function": handlerMethod,
	})
	require.NoError(t, err)

	testCases := map[string]struct {
		requestBody      string
		expectedResponse string
	}{
		"success": {
			requestBody:      `{"jsonrpc":"2.0","id":1,"method":"function","params":[1]}`,
			expectedResponse: `{"jsonrpc":"2.0","id":1,"result":{"field":1}}`,
		},
		"error": {
			requestBody:      `{"jsonrpc":"2.0","id":1,"method":"function","params":[-1]}`,
			expectedResponse: `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"custom error"}}`,
		},
		"invalid json": {
			requestBody:      `{"jsonrpc":"2.0","id":1,"method":"function","params":[1]`,
			expectedResponse: `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"unexpected EOF"}}`,
		},
		"method not found": {
			requestBody:      `{"jsonrpc":"2.0","id":1,"method":"not_found","params":[1]}`,
			expectedResponse: `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`,
		},
		"invalid params": {
			requestBody:      `{"jsonrpc":"2.0","id":1,"method":"function","params":[1,2]}`,
			expectedResponse: `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"too much arguments"}}`, // TODO: return correct code here
		},
		"invalid params type": {
			requestBody:      `{"jsonrpc":"2.0","id":1,"method":"function","params":["1"]}`,
			expectedResponse: `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"json: cannot unmarshal string into Go value of type int"}}`,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			body := bytes.NewReader([]byte(testCase.requestBody))
			request, err := http.NewRequest(http.MethodPost, "/", body)
			require.NoError(t, err)

			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, request)
			require.Equal(t, http.StatusOK, rr.Code)

			require.JSONEq(t, testCase.expectedResponse, rr.Body.String())
		})
	}
}
