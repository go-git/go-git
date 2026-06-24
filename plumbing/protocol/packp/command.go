package packp

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// CommandArgs is the interface for v2 command-specific arguments.
type CommandArgs interface {
	Encoder
	Decoder
}

// CommandRequest represents a v2 command request.
//
// Wire format:
//
//	request = empty-request | command-request
//	empty-request = flush-pkt
//	command-request = command
//	    capability-list
//	    delim-pkt
//	    command-args
//	    flush-pkt
//	command = PKT-LINE("command=" key LF)
//	command-args = *command-specific-arg
//
// An empty Command encodes as an empty request (a single flush-pkt).
// On decode, a flush-pkt as the first packet leaves Command empty.
type CommandRequest struct {
	Command      string
	Capabilities capability.List
	Args         CommandArgs
}

// Encode writes the command request to w.
// If Command is empty, it writes a single flush-pkt (empty request).
func (c *CommandRequest) Encode(w io.Writer) error {
	if c.Command == "" {
		return pktline.WriteFlush(w)
	}

	if _, err := pktline.Writef(w, "command=%s\n", c.Command); err != nil {
		return err
	}

	if err := EncodeListV2(w, &c.Capabilities); err != nil {
		return err
	}

	if err := pktline.WriteDelim(w); err != nil {
		return err
	}

	if c.Args != nil {
		if err := c.Args.Encode(w); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

// Decode reads a command request from r.
// If the first packet is a flush-pkt, Command is left empty (empty request).
func (c *CommandRequest) Decode(r io.Reader) error {
	c.Command = ""
	c.Capabilities = capability.List{}

	length, line, err := pktline.ReadLine(r)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	if length == pktline.Flush {
		return nil
	}

	line = bytes.TrimSuffix(line, []byte("\n"))
	const prefix = "command="
	if !bytes.HasPrefix(line, []byte(prefix)) {
		return fmt.Errorf("expected command line, got %q", string(line))
	}
	c.Command = string(line[len(prefix):])

	// Read capabilities until delim-pkt.
	length, err = DecodeListV2(r, &c.Capabilities)
	if err != nil {
		return err
	}
	if length != pktline.Delim {
		return fmt.Errorf("expected delim-pkt after capabilities, got %04x", length)
	}

	// Read command args until flush-pkt.
	if c.Args != nil {
		return c.Args.Decode(r)
	}

	// No args decoder — consume the flush-pkt.
	length, _, err = pktline.ReadLine(r)
	if err != nil {
		return err
	}
	if length != pktline.Flush {
		return fmt.Errorf("expected flush-pkt after empty args, got %04x", length)
	}
	return nil
}
