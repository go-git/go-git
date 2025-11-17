package plumbing

import (
	"fmt"
	"errors"
)

var (
	ErrUnexpected = errors.New("unexpected client error")
)

type PermanentError struct {
	Err error
}

func NewPermanentError(err error) *PermanentError {
	if err == nil {
		return nil
	}

	return &PermanentError{Err: err}
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent client error: %s", e.Err.Error())
}

