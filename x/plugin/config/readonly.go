package config

import (
	"errors"

	"github.com/go-git/go-git/v6/config"
)

// ErrReadOnly is returned by SetConfig on a read-only ConfigStorer.
var ErrReadOnly = errors.New("config storer is read-only")

// readOnlyStorer wraps a config value as a read-only config.ConfigStorer.
// Calls to SetConfig return ErrReadOnly.
type readOnlyStorer struct {
	cfg config.Config
}

// Config returns a deep copy of the stored configuration.
func (s *readOnlyStorer) Config() (*config.Config, error) {
	return cloneConfig(&s.cfg), nil
}

// SetConfig always returns ErrReadOnly.
func (s *readOnlyStorer) SetConfig(*config.Config) error {
	return ErrReadOnly
}
