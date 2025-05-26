package appstate

import "context"

type StateError interface {
	error
	StateReconcile() func(ctx context.Context) error
	CleanUp(ctx context.Context) error
}

type baseStateError struct {
	StateError
	stateReconcile func() func(ctx context.Context) error
	cleanUp        func(ctx context.Context) error
}

func (b *baseStateError) StateReconcile() func(ctx context.Context) error {
	if b.stateReconcile == nil {
		return nil
	}

	return b.stateReconcile()
}

func (b *baseStateError) CleanUp(ctx context.Context) error {
	if b.cleanUp == nil {
		return nil
	}

	return b.cleanUp(ctx)
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
	cleanUp func(ctx context.Context) error,
) StateError {
	return &ErrorUnknownState{
		baseStateError: baseStateError{
			stateReconcile: stateReconcile,
			cleanUp:        cleanUp,
		},
	}
}

var _ StateError = (*ErrorUnknownInProgressApp)(nil)

type ErrorUnknownInProgressApp struct {
	baseStateError
}

func (e *ErrorUnknownInProgressApp) Error() string {
	return "unknown in-progress app"
}

func IsUnknownInProgressApp(err StateError) bool {
	_, ok := err.(*ErrorUnknownInProgressApp)
	return ok
}

/**
 * @param cleanUp: a function to clean up the in-progress app,
 * e.g., cancel the in-progress operating from the running map
 */
func NewErrorUnknownInProgressApp(
	cleanUp func(ctx context.Context) error,
) StateError {
	return &ErrorUnknownInProgressApp{
		baseStateError: baseStateError{
			stateReconcile: nil,
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
