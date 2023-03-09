package plumbing

import "fmt"

type PermanentError struct {
	Err error
}

func NewPermanentError(err error) *PermanentError {
	if err == nil {
		return nil
	}

	return &PermanentError{Err: err}
}

// Error implements Error interface and returns string representation of the error
func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent client error: %s", e.Err.Error())
}

// Unwrap implements the Unwrap interface and returns WrappedError
func (e *PermanentError) Unwrap() error {
	return e.Err
}

type UnexpectedError struct {
	Err error
}

func NewUnexpectedError(err error) *UnexpectedError {
	if err == nil {
		return nil
	}

	return &UnexpectedError{Err: err}
}

// Error implements Error interface and returns string representation of the error
func (e *UnexpectedError) Error() string {
	return fmt.Sprintf("unexpected client error: %s", e.Err.Error())
}

// Unwrap implements the Unwrap interface and returns WrappedError
func (e *UnexpectedError) Unwrap() error {
	return e.Err
}
