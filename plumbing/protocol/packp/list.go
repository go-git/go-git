package packp

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// EncodeListV2 writes capabilities in v2 format: one capability per pkt-line.
// Each capability is written as "key\n" or "key=value\n" or "key=v1 v2\n". The
// caller is responsible for writing the terminating packet (flush-pkt or
// delim-pkt) after the last capability.
func EncodeListV2(w io.Writer, l *capability.List) error {
	for _, key := range l.All() {
		values := l.Get(key)
		if len(values) == 0 {
			if _, err := pktline.Writef(w, "%s\n", key); err != nil {
				return err
			}
		} else {
			if _, err := pktline.Writef(w, "%s=%s\n", key, strings.Join(values, " ")); err != nil {
				return err
			}
		}
	}
	return nil
}

// DecodeListV2 reads capabilities in v2 format from a pkt-line stream. It
// reads pkt-lines until flush-pkt, delim-pkt, or EOF, appending each parsed
// capability to the list. It returns the terminating packet length
// (pktline.Flush, pktline.Delim, or pktline.ResponseEnd) so the caller knows
// what terminated the capability list.
func DecodeListV2(r io.Reader, l *capability.List) (int, error) {
	for {
		length, line, err := pktline.ReadLine(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return pktline.Flush, nil
			}
			return 0, err
		}

		if length == pktline.Flush || length == pktline.Delim || length == pktline.ResponseEnd {
			return length, nil
		}

		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) == 0 {
			continue
		}

		key, value, hasValue := strings.Cut(string(line), "=")
		if hasValue {
			for v := range strings.SplitSeq(value, " ") {
				if v != "" {
					l.Add(key, v)
				}
			}
		} else {
			l.Add(key)
		}
	}
}
