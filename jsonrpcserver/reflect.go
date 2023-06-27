package jsonrpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
)

var (
	ErrNotFunction         = errors.New("not a function")
	ErrMustReturnError     = errors.New("function must return error as a last return value")
	ErrMustHaveContext     = errors.New("function must have context.Context as a first argument")
	ErrTooManyReturnValues = errors.New("too many return values")

	ErrTooMuchArguments = errors.New("too much arguments")
)

type methodHandler struct {
	in  []reflect.Type
	out []reflect.Type
	fn  any
}

func getMethodTypes(fn interface{}) (methodHandler, error) {
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return methodHandler{}, ErrNotFunction
	}
	numIn := fnType.NumIn()
	in := make([]reflect.Type, numIn)
	for i := 0; i < numIn; i++ {
		in[i] = fnType.In(i)
	}
	// first input argument must be context.Context
	if numIn == 0 || in[0] != reflect.TypeOf((*context.Context)(nil)).Elem() {
		return methodHandler{}, ErrMustHaveContext
	}

	numOut := fnType.NumOut()
	out := make([]reflect.Type, numOut)
	for i := 0; i < numOut; i++ {
		out[i] = fnType.Out(i)
	}

	// function must contain error as a last return value
	if numOut == 0 || !out[numOut-1].Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return methodHandler{}, ErrMustReturnError
	}

	// function can return only one value
	if numOut > 2 {
		return methodHandler{}, ErrTooManyReturnValues
	}

	return methodHandler{in, out, fn}, nil
}

func (h methodHandler) call(ctx context.Context, params []json.RawMessage) (any, error) {
	args, err := extractArgumentsFromJSONparamsArray(h.in[1:], params)
	if err != nil {
		return nil, err
	}

	// prepend context.Context
	args = append([]reflect.Value{reflect.ValueOf(ctx)}, args...)

	// call function
	results := reflect.ValueOf(h.fn).Call(args)

	// check error
	var outError error
	if !results[len(results)-1].IsNil() {
		errVal, ok := results[len(results)-1].Interface().(error)
		if !ok {
			return nil, ErrMustReturnError
		}
		outError = errVal
	}

	if len(results) == 1 {
		return nil, outError
	} else {
		return results[0].Interface(), outError
	}
}

func extractArgumentsFromJSONparamsArray(in []reflect.Type, params []json.RawMessage) ([]reflect.Value, error) {
	if len(params) > len(in) {
		return nil, ErrTooMuchArguments
	}

	args := make([]reflect.Value, len(in))
	for i, argType := range in {
		arg := reflect.New(argType)
		if i < len(params) {
			if err := json.Unmarshal(params[i], arg.Interface()); err != nil {
				return nil, err
			}
		}
		args[i] = arg.Elem()
	}
	return args, nil
}
