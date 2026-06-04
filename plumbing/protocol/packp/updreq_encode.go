package packp

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// Encode writes the ReferenceUpdateRequest encoding to the stream.
func (req *UpdateRequests) Encode(w io.Writer) error {
	if err := validateUpdateRequests(req); err != nil {
		return err
	}

	if err := req.encodeShallow(w); err != nil {
		return err
	}

	if err := req.encodeCommands(w, req.Commands, &req.Capabilities); err != nil {
		return err
	}

	return nil
}

func (req *UpdateRequests) encodeShallow(w io.Writer) error {
	for _, h := range req.Shallows {
		objID := []byte(h.String())
		_, err := pktline.Writef(w, "%s%s", shallow, objID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (req *UpdateRequests) encodeCommands(w io.Writer,
	cmds []*Command, caps *capability.List,
) error {
	capStr := caps.String()
	if len(capStr) > 0 {
		// Canonical Git adds a space before the capabilities.
		// See https://github.com/git/git/blob/57da342c786f59eaeb436c18635cc1c7597733d9/send-pack.c#L594
		capStr = " " + capStr
	}
	if _, err := pktline.Writef(w, "%s\x00%s",
		formatCommand(cmds[0]), capStr); err != nil {
		return err
	}

	for _, cmd := range cmds[1:] {
		if _, err := pktline.Write(w, []byte(formatCommand(cmd))); err != nil {
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
