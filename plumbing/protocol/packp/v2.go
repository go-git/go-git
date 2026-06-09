package packp

import (
	"bytes"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// writeCommand encodes a protocol v2 command request:
//
//	command=<name> LF
//	*( capability LF )
//	[ delim-pkt *( command-specific-arg ) ]
//	flush-pkt
//
// The delimiter packet and arguments are only emitted when writeArgs
// produces output, matching the grammar where command-args is optional.
func writeCommand(w io.Writer, name string, capabilities []string, writeArgs func(io.Writer) error) error {
	if _, err := pktline.Writeln(w, "command="+name); err != nil {
		return err
	}
	for _, c := range capabilities {
		if _, err := pktline.Writeln(w, c); err != nil {
			return err
		}
	}

	var args bytes.Buffer
	if writeArgs != nil {
		if err := writeArgs(&args); err != nil {
			return err
		}
	}

	if args.Len() > 0 {
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
		if _, err := w.Write(args.Bytes()); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}
