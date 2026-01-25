package plumbing

import "fmt"

// PermanentError represents an unrecoverable error.
type PermanentError struct {
	Err error
}

// NewPermanentError returns a new PermanentError wrapping the given error.
func NewPermanentError(err error) *PermanentError {
	if err == nil {
		return nil
	}

	return &PermanentError{Err: err}
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent client error: %s", e.Err.Error())
}
