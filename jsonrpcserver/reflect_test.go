package jsonrpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type ctxKey string

func rawParams(raw string) []json.RawMessage {
	var params []json.RawMessage
	err := json.Unmarshal([]byte(raw), &params)
	if err != nil {
		panic(err)
	}
	return params
}

func TestGetMethodTypes(t *testing.T) {
	funcWithTypes := func(ctx context.Context, arg1 int, arg2 float32) error {
		return nil
	}
	methodTypes, err := getMethodTypes(funcWithTypes)
	require.NoError(t, err)
	require.Equal(t, 3, len(methodTypes.in))
	require.Equal(t, 1, len(methodTypes.out))

	funcWithoutArgs := func(ctx context.Context) error {
		return nil
	}
	methodTypes, err = getMethodTypes(funcWithoutArgs)
	require.NoError(t, err)

	funcWithouCtx := func(arg1 int, arg2 float32) error {
		return nil
	}
	methodTypes, err = getMethodTypes(funcWithouCtx)
	require.ErrorIs(t, err, ErrMustHaveContext)

	funcWithouError := func(ctx context.Context, arg1 int, arg2 float32) (int, float32) {
		return 0, 0
	}
	methodTypes, err = getMethodTypes(funcWithouError)
	require.ErrorIs(t, err, ErrMustReturnError)

	funcWithTooManyReturnValues := func(ctx context.Context, arg1 int, arg2 float32) (int, float32, error) {
		return 0, 0, nil
	}
	methodTypes, err = getMethodTypes(funcWithTooManyReturnValues)
	require.ErrorIs(t, err, ErrTooManyReturnValues)
}

type dummyStruct struct {
	Field int `json:"field"`
}

func TestExtractArgumentsFromJSON(t *testing.T) {
	funcWithTypes := func(context.Context, int, float32, []int, dummyStruct) error {
		return nil
	}
	methodTypes, err := getMethodTypes(funcWithTypes)
	require.NoError(t, err)

	jsonArgs := rawParams(`[1, 2.0, [2, 3, 5], {"field": 11}]`)
	args, err := extractArgumentsFromJSONparamsArray(methodTypes.in[1:], jsonArgs)
	require.NoError(t, err)
	require.Equal(t, 4, len(args))
	require.Equal(t, int(1), args[0].Interface())
	require.Equal(t, float32(2.0), args[1].Interface())
	require.Equal(t, []int{2, 3, 5}, args[2].Interface())
	require.Equal(t, dummyStruct{Field: 11}, args[3].Interface())

	funcWithoutArgs := func(context.Context) error {
		return nil
	}
	methodTypes, err = getMethodTypes(funcWithoutArgs)
	require.NoError(t, err)
	jsonArgs = rawParams(`[]`)
	args, err = extractArgumentsFromJSONparamsArray(methodTypes.in[1:], jsonArgs)
	require.NoError(t, err)
	require.Equal(t, 0, len(args))
}

func TestCall_old(t *testing.T) {
	var (
		errorArg = 0
		errorOut = errors.New("function error") //nolint:goerr113
	)
	funcWithTypes := func(ctx context.Context, arg int) (dummyStruct, error) {
		value := ctx.Value(ctxKey("key")).(string) //nolint:forcetypeassert
		require.Equal(t, "value", value)

		if arg == errorArg {
			return dummyStruct{}, errorOut
		}
		return dummyStruct{arg}, nil
	}
	methodTypes, err := getMethodTypes(funcWithTypes)
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), ctxKey("key"), "value")

	jsonArgs := rawParams(`[1]`)
	result, err := methodTypes.call(ctx, jsonArgs)
	require.NoError(t, err)
	require.Equal(t, dummyStruct{1}, result)

	jsonArgs = rawParams(fmt.Sprintf(`[%d]`, errorArg))
	result, err = methodTypes.call(ctx, jsonArgs)
	require.ErrorIs(t, err, errorOut)
	require.Equal(t, dummyStruct{}, result)
}

func TestCall(t *testing.T) {
	// for testing error return
	var (
		errorArg = 0
		errorOut = errors.New("function error") //nolint:goerr113
	)
	functionWithTypes := func(ctx context.Context, arg int) (dummyStruct, error) {
		// test context
		value := ctx.Value(ctxKey("key")).(string) //nolint:forcetypeassert
		require.Equal(t, "value", value)

		if arg == errorArg {
			return dummyStruct{}, errorOut
		}
		return dummyStruct{arg}, nil
	}
	functionNoArgs := func(ctx context.Context) (dummyStruct, error) {
		// test context
		value := ctx.Value(ctxKey("key")).(string) //nolint:forcetypeassert
		require.Equal(t, "value", value)

		return dummyStruct{1}, nil
	}
	functionNoArgsError := func(ctx context.Context) (dummyStruct, error) {
		// test context
		value := ctx.Value(ctxKey("key")).(string) //nolint:forcetypeassert
		require.Equal(t, "value", value)

		return dummyStruct{}, errorOut
	}
	functionNoReturn := func(ctx context.Context, arg int) error {
		// test context
		value := ctx.Value(ctxKey("key")).(string) //nolint:forcetypeassert
		require.Equal(t, "value", value)
		return nil
	}
	functonNoReturnError := func(ctx context.Context, arg int) error {
		// test context
		value := ctx.Value(ctxKey("key")).(string) //nolint:forcetypeassert
		require.Equal(t, "value", value)

		return errorOut
	}

	testCases := map[string]struct {
		function      interface{}
		args          string
		expectedValue interface{}
		expectedError error
	}{
		"functionWithTypes": {
			function:      functionWithTypes,
			args:          `[1]`,
			expectedValue: dummyStruct{1},
			expectedError: nil,
		},
		"functionWithTypesError": {
			function:      functionWithTypes,
			args:          fmt.Sprintf(`[%d]`, errorArg),
			expectedValue: dummyStruct{},
			expectedError: errorOut,
		},
		"functionNoArgs": {
			function:      functionNoArgs,
			args:          `[]`,
			expectedValue: dummyStruct{1},
			expectedError: nil,
		},
		"functionNoArgsError": {
			function:      functionNoArgsError,
			args:          `[]`,
			expectedValue: dummyStruct{},
			expectedError: errorOut,
		},
		"functionNoReturn": {
			function:      functionNoReturn,
			args:          `[1]`,
			expectedValue: nil,
			expectedError: nil,
		},
		"functionNoReturnError": {
			function:      functonNoReturnError,
			args:          `[1]`,
			expectedValue: nil,
			expectedError: errorOut,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			methodTypes, err := getMethodTypes(testCase.function)
			require.NoError(t, err)

			ctx := context.WithValue(context.Background(), ctxKey("key"), "value")

			result, err := methodTypes.call(ctx, rawParams(testCase.args))
			if testCase.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, testCase.expectedError)
			}
			require.Equal(t, testCase.expectedValue, result)
		})
	}
}
