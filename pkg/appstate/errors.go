package appstate

import "context"

type StateError interface {
	error
	StateReconcile() func(ctx context.Context) error
	CleanUp() error
}

type baseStateError struct {
	StateError
	stateReconcile func() func(ctx context.Context) error
	cleanUp        func() error
}

func (b *baseStateError) StateReconcile() func(ctx context.Context) error {
	if b.stateReconcile == nil {
		return nil
	}

	return b.stateReconcile()
}

func (b *baseStateError) CleanUp() error {
	if b.cleanUp == nil {
		return nil
	}

	return b.cleanUp()
}

var _ StateError = (*ErrorUnknownState)(nil)

type ErrorUnknownState struct {
	baseStateError
}

func (e *ErrorUnknownState) Error() string {
	return "unknown state"
}

func IsUnknownState(err StateError) bool {
	_, ok := err.(*ErrorUnknownState)
	return ok
}

func NewErrorUnknownState(
	stateReconcile func() func(ctx context.Context) error,
	cleanUp func() error,
) StateError {
	return &ErrorUnknownState{
		baseStateError: baseStateError{
			stateReconcile: stateReconcile,
			cleanUp:        cleanUp,
		},
	}
}

var _ StateError = (*errorCommon)(nil)

type errorCommon struct {
	baseStateError
	error func() string
}

func (e errorCommon) Error() string {
	return e.error()
}

func NewStateError(msg string) StateError {
	return &errorCommon{
		baseStateError: baseStateError{},
		error:          func() string { return msg },
	}
}
