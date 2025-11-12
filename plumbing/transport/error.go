
package transport

import (
	"fmt"
)

type AuthenticationRequiredError struct {
	Err error
}

func NewAuthenticationRequiredError(err error) error {
	return &AuthenticationRequiredError{
		Err: err,
	}
}

func (e *AuthenticationRequiredError) Error() string {
	return fmt.Sprintf("authentication required: %s", e.Err.Error())
}

func (e *AuthenticationRequiredError) Unwrap() error {
	return e.Err
}

type AuthorizationFailedError struct {
	Err error
}

func NewAuthorizationFailedError(err error) error {
	return &AuthorizationFailedError{
		Err: err,
	}
}

func (e *AuthorizationFailedError) Error() string {
	return fmt.Sprintf("authorization failed: %s", e.Err.Error())
}

func (e *AuthorizationFailedError) Unwrap() error {
	return e.Err
}

type RepositoryNotFoundError struct {
	Err error
}

func NewRepositoryNotFoundError(err error) error {
	return &RepositoryNotFoundError{
		Err: err,
	}
}

func (e *RepositoryNotFoundError) Error() string {
	return fmt.Sprintf("repository not found: %s", e.Err.Error())
}

func (e *RepositoryNotFoundError) Unwrap() error {
	return e.Err
}

