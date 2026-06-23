package packp

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// writeCommand encodes a protocol v2 command request:
//
//	command=<name> LF
//	*( capability LF )
//	delim-pkt
//	*( command-specific-arg )
//	flush-pkt
//
// The delimiter packet is always emitted, even when there are no
// command-specific arguments. The v2 grammar makes the delim-pkt
// mandatory (only command-args is optional), and upstream Git always
// sends it (connect.c, fetch-pack.c).
func writeCommand(w io.Writer, name string, capabilities []string, writeArgs func(io.Writer) error) error {
	if _, err := pktline.Writeln(w, "command="+name); err != nil {
		return err
	}
	for _, c := range capabilities {
		if _, err := pktline.Writeln(w, c); err != nil {
			return err
		}
	}

	if err := pktline.WriteDelim(w); err != nil {
		return err
	}
	if writeArgs != nil {
		if err := writeArgs(w); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}
