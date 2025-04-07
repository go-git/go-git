package packp

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

// Encode writes the ReferenceUpdateRequest encoding to the stream.
func (req *UpdateRequests) Encode(w io.Writer) error {
	if err := req.validate(); err != nil {
		return err
	}

	if err := req.encodeShallow(w, req.Shallow); err != nil {
		return err
	}

	if err := req.encodeCommands(w, req.Commands, req.Capabilities); err != nil {
		return err
	}

	return nil
}

func (req *UpdateRequests) encodeShallow(w io.Writer,
	h *plumbing.Hash,
) error {
	if h == nil {
		return nil
	}

	objId := []byte(h.String())
	_, err := pktline.Writef(w, "%s%s", shallow, objId)
	return err
}

func (req *UpdateRequests) encodeCommands(w io.Writer,
	cmds []*Command, cap *capability.List,
) error {
	if _, err := pktline.Writef(w, "%s\x00%s",
		formatCommand(cmds[0]), cap.String()); err != nil {
		return err
	}

	for _, cmd := range cmds[1:] {
		if _, err := pktline.Writef(w, formatCommand(cmd)); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

func formatCommand(cmd *Command) string {
	o := cmd.Old.String()
	n := cmd.New.String()
	return fmt.Sprintf("%s %s %s", o, n, cmd.Name)
}
