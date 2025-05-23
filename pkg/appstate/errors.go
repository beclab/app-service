package appstate

import "errors"

var (
	ErrUnknownState = errors.New("application manager: unknown state")
)
