package packp

import (
	"errors"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"
)

var (
	ErrEmptyCommands    = errors.New("commands cannot be empty")
	ErrMalformedCommand = errors.New("malformed command")
)

// ReferenceUpdateRequest values represent reference upload requests.
// Values from this type are not zero-value safe, use the New function instead.
//
// TODO: Add support for push-cert
type ReferenceUpdateRequest struct {
	Capabilities *capability.List
	Commands     []*Command
	Shallow      *plumbing.Hash
}

// New returns a pointer to a new ReferenceUpdateRequest value.
func NewReferenceUpdateRequest() *ReferenceUpdateRequest {
	return &ReferenceUpdateRequest{
		Capabilities: capability.NewList(),
		Commands:     nil,
	}
}

func (r *ReferenceUpdateRequest) validate() error {
	if len(r.Commands) == 0 {
		return ErrEmptyCommands
	}

	for _, c := range r.Commands {
		if err := c.validate(); err != nil {
			return err
		}
	}

	return nil
}

type Action string

const (
	Create  Action = "create"
	Update         = "update"
	Delete         = "delete"
	Invalid        = "invalid"
)

type Command struct {
	Name string
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
