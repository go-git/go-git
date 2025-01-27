package packp

import (
	"errors"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

var (
	ErrEmptyCommands    = errors.New("commands cannot be empty")
	ErrMalformedCommand = errors.New("malformed command")
)

// UpdateRequests values represent reference upload requests.
// Values from this type are not zero-value safe, use the New function instead.
type UpdateRequests struct {
	Capabilities *capability.List
	Commands     []*Command
	Options      []*Option
	Shallow      *plumbing.Hash
}

// NewUpdateRequests returns a new UpdateRequests.
func NewUpdateRequests() *UpdateRequests {
	// TODO: Add support for push-cert
	return &UpdateRequests{
		Capabilities: capability.NewList(),
	}
}

func (req *UpdateRequests) validate() error {
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

type Action string

const (
	Create  Action = "create"
	Update  Action = "update"
	Delete  Action = "delete"
	Invalid Action = "invalid"
)

type Command struct {
	Name plumbing.ReferenceName
	Old  plumbing.Hash
	New  plumbing.Hash
}

func (c *Command) Action() Action {
	if c.Old == plumbing.ZeroHash && c.New == plumbing.ZeroHash {
		return Invalid
	}

	if c.Old == plumbing.ZeroHash {
		return Create
	}

	if c.New == plumbing.ZeroHash {
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

type Option struct {
	Key   string
	Value string
}
