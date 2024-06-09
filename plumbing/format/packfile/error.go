package packfile

import (
	"errors"
	"fmt"
)

// Error specifies errors returned during packfile parsing.
type Error struct {
	error
}

// NewError returns a new error.
func NewError(reason string) *Error {
	return &Error{errors.New(reason)}
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.error
}

// AddDetails adds details to an error, with additional text.
func (e *Error) AddDetails(format string, args ...interface{}) *Error {
	err := fmt.Errorf(format, args...)
	if e.error == nil {
		return &Error{err}
	}
	return &Error{fmt.Errorf("%w: %w", e.error, err)}
}
