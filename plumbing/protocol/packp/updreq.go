package packp

import (
	"errors"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// Errors returned by the updreq package.
var (
	ErrEmptyCommands    = errors.New("commands cannot be empty")
	ErrMalformedCommand = errors.New("malformed command")
)

// UpdateRequests values represent reference upload requests.
// The zero value is safe to use; Commands and Shallows can be populated
// via append.
type UpdateRequests struct {
	Capabilities capability.List
	Commands     []*Command
	Shallows     []plumbing.Hash
	// TODO: Support push-cert
}

func validateUpdateRequests(req *UpdateRequests) error {
	if len(req.Commands) == 0 {
		return ErrEmptyCommands
	}

	for _, c := range req.Commands {
		if err := c.validate(); err != nil {
			return err
		}
	}

	return nil
}

// Action represents the action type of a command.
type Action string

// Action types.
const (
	Create  Action = "create"
	Update  Action = "update"
	Delete  Action = "delete"
	Invalid Action = "invalid"
)

// Command represents a command to be executed on a reference.
type Command struct {
	Name plumbing.ReferenceName
	Old  plumbing.Hash
	New  plumbing.Hash
}

// Action returns the action type of the command.
func (c *Command) Action() Action {
	// Compare with IsZero rather than == plumbing.ZeroHash: the latter also
	// matches on the object-format field, and a zero object id decoded from
	// the wire carries the negotiated format (e.g. sha256 for a 64-hex id)
	// while plumbing.ZeroHash is format-unset. IsZero looks only at the
	// bytes, mirroring Git's is_null_oid.
	if c.Old.IsZero() && c.New.IsZero() {
		return Invalid
	}

	if c.Old.IsZero() {
		return Create
	}

	if c.New.IsZero() {
		return Delete
	}

	return Update
}

func (c *Command) validate() error {
	if c.Action() == Invalid {
		return ErrMalformedCommand
	}

	return nil
}
