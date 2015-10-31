package core

import "fmt"

type PermanentError struct {
	err error
}

func NewPermanentError(err error) *PermanentError {
	if err == nil {
		return nil
	}

	return &PermanentError{err: err}
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent client error: %s", e.err.Error())
}

type UnexpectedError struct {
	err error
}

func NewUnexpectedError(err error) *UnexpectedError {
	if err == nil {
		return nil
	}

	return &UnexpectedError{err: err}
}

func (e *UnexpectedError) Error() string {
	return fmt.Sprintf("unexpected client error: %s", e.err.Error())
}
