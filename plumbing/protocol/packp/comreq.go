package packp

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

const (
	symRefAttr = "symref-target:"
	acksLine   = "acknowledgements\n"
	packLine   = "packfile\n"
)

// CommandRequest values represent the information transmitted on a
// command request message used in wire protocol v2. Values from this type
// are not zero-value safe, use the New function instead.
type CommandRequest struct {
	Command      string
	Capabilities *capability.List
	Args         *capability.List
}

// NewCommandRequest returns a pointer to a new CommandRequest value, ready to be
// used. It has no capabilities or args. Please
// note that to encode a command-request it must have at least a command.
func NewCommandRequest() *CommandRequest {
	return &CommandRequest{
		Capabilities: capability.NewList(),
		Args:         capability.NewList(),
	}
}

// Validate validates the content of UploadRequest, following the next rules:
//   - Wants MUST have at least one reference
//   - capability.Shallow MUST be present if Shallows is not empty
//   - is a non-zero DepthCommits is given capability.Shallow MUST be present
//   - is a DepthSince is given capability.Shallow MUST be present
//   - is a DepthReference is given capability.DeepenNot MUST be present
//   - MUST contain only maximum of one of capability.Sideband and capability.Sideband64k
//   - MUST contain only maximum of one of capability.MultiACK and capability.MultiACKDetailed
func (req *CommandRequest) Validate() error {
	if req.Command == "" {
		return fmt.Errorf("empty command")
	}

	if len(req.Capabilities.All()) == 0 {
		return fmt.Errorf("empty capability list")
	}

	return nil
}

// Decode reads the next upload-request form its input and
// stores it in the UploadRequest.
func (c *CommandRequest) Decode(r io.Reader) error {
	s := pktline.NewScanner(r)

	// decode command=<key> LF
	s.Scan()
	key := string(s.Bytes())

	// this request can be only flush to indicate end of transaction
	if s.PktType() == pktline.FlushType {
		return nil
	}

	if i := strings.Index(key, "command="); i < 0 {
		return fmt.Errorf("missing command name")
	} else {
		c.Command = strings.TrimSpace(key[i+8:])
	}

	// now read capabilities
	for s.Scan() && !(s.PktType() == pktline.DelimType) {
		sp := strings.Split(strings.TrimSpace(string(s.Bytes())), "=")
		if len(sp) == 1 {
			c.Capabilities.Add(capability.Capability(sp[0]))
		} else {
			c.Capabilities.Add(
				capability.Capability(sp[0]),
				strings.Split(sp[1], " ")...,
			)
		}
	}

	// this part is command specific
	for s.Scan() && !(s.PktType() == pktline.FlushType) {
		sp := strings.Split(strings.TrimSpace(string(s.Bytes())), " ")
		if len(sp) == 1 {
			c.Args.Add(capability.Capability(sp[0]))
		} else {
			c.Args.Add(
				capability.Capability(sp[0]),
				strings.Split(sp[1], " ")...,
			)
		}
	}

	return nil
}

// CommandResponse values represent the information transmitted on a
// command response message used in wire protocol v2. Values from this type
// are not zero-value safe, use the New function instead.
type CommandResponse struct {
	Refs         []*plumbing.Reference
	Capabilities *capability.List
	Args         *capability.List
	Wants        []plumbing.Hash
	Haves        []plumbing.Hash
	Command      string
	Storer       storer.Storer
}

func (res *CommandResponse) Encode(w io.Writer) error {
	ple := pktline.NewEncoder(w)

	switch res.Command {
	case capability.LsRefs.String():
		for _, r := range res.Refs {
			switch r.Type() {
			case plumbing.SymbolicReference:
				ple.EncodeString(r.Hash().String() + " " + r.Name().String() + " " + symRefAttr + " " + r.Target().String() + "\n")
			case plumbing.HashReference:
				ple.EncodeString(r.Hash().String() + " " + r.Name().String() + "\n")
			default:
				return fmt.Errorf("unreckognized reference type")
			}
		}
	case capability.Fetch.String():
		// ofsdelta
		var delta uint
		if res.Args.Supports(capability.OFSDelta) {
			delta = 50
		}

		// if done was sent skip acks section
		if res.Args.Supports("done") {
			ple.EncodeString(packLine)
			if len(res.Haves) > 0 {
				pack := &bytes.Buffer{}
				pke := packfile.NewEncoder(pack, res.Storer, false)
				_, err := pke.Encode(res.Haves, delta)
				if err != nil {
					return err
				}
				ple.Encode(append([]byte{1}, pack.Bytes()...))
			}
		} else {
			ple.EncodeString(acksLine)
		}
	}

	return ple.Flush()
}
