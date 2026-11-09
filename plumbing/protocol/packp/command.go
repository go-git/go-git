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
//
// Like upstream's serve.c (process_request), Decode accepts the command line
// anywhere among the capability lines, and a request without command arguments
// may omit the delim-pkt. Unlike serve.c, it does not check the capabilities
// themselves, such as object-format.
type CommandRequest struct {
	Command      string
	Capabilities capability.List
	Args         CommandArgs

	// NewArgs, if set, is called by Decode once the command is known and
	// returns the decoder for its arguments, replacing Args. It lets a server
	// pick the argument type for the requested command, whose line is not
	// necessarily the first one of the request.
	NewArgs func(command string) (CommandArgs, error)
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

	// Read the command and capability lines, in any order, until the
	// delim-pkt or the flush-pkt that ends a request without arguments.
	const prefix = "command="
	var keys, length int
	var seen bool
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if keys == 0 {
					return nil
				}
				return io.ErrUnexpectedEOF
			}
			return err
		}
		length = l
		if length == pktline.Flush || length == pktline.Delim {
			break
		}
		if length == pktline.ResponseEnd {
			return errors.New("unexpected response-end-pkt in command request")
		}

		keys++
		line = bytes.TrimSuffix(line, []byte("\n"))
		if cmd, ok := bytes.CutPrefix(line, []byte(prefix)); ok {
			if seen {
				return fmt.Errorf("command %q requested after already requesting command %q", cmd, c.Command)
			}
			seen = true
			c.Command = string(cmd)
			continue
		}
		addListV2Line(&c.Capabilities, line)
	}

	if keys == 0 && length == pktline.Flush {
		return nil
	}
	if c.Command == "" {
		return errors.New("no command requested")
	}

	if c.NewArgs != nil {
		args, err := c.NewArgs(c.Command)
		if err != nil {
			return err
		}
		c.Args = args
	}

	// Without a delim-pkt the flush-pkt also ends the (empty) arguments.
	if length == pktline.Flush {
		return nil
	}

	// Read command args until flush-pkt.
	if c.Args != nil {
		return c.Args.Decode(r)
	}

	// No args decoder — consume the flush-pkt.
	length, _, err := pktline.ReadLine(r)
	if err != nil {
		return err
	}
	if length != pktline.Flush {
		return fmt.Errorf("expected flush-pkt after empty args, got %04x", length)
	}
	return nil
}
