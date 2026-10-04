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
// Each capability is written as "key\n" or "key=value\n", and several values
// are joined into one, "key=v1 v2\n", as a command lists its features
// (fetch=shallow filter). DecodeListV2 reads that line back as the single
// value "v1 v2", so the two are inverses only for keys with at most one value.
// The caller is responsible for writing the terminating packet (flush-pkt or
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
// capability to the list. "key" adds key with no value, and "key=value" adds
// value as one string, unsplit, so "key=" adds the empty string. It returns
// the terminating packet length (pktline.Flush, pktline.Delim, or
// pktline.ResponseEnd) so the caller knows what terminated the capability
// list.
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
		if !hasValue {
			l.Add(key)
			continue
		}
		// The value is kept as sent, spaces and all: git hands it whole to
		// the capability's handler, which splits it only where that
		// capability takes a list (fetch=shallow filter), and rejects
		// "object-format=" or "object-format=sha1 sha256" as an unknown
		// format.
		l.Add(key, value)
	}
}
