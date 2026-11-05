package transport

import (
	"errors"
	"io"
)

// ErrPackJunk is returned when pack data follows the pack trailer. The wording
// is that of Git's index-pack.
var ErrPackJunk = errors.New("pack has junk at the end")

// DrainPackSection reads the rest of a multiplexed pack response from demuxer,
// once the object storage has stored the pack, up to its closing flush-pkt.
// Progress still goes to the demuxer's Progress, and an error the server sends
// on the error band is returned.
//
// Pack data after the trailer fails the fetch with [ErrPackJunk], as Git fails
// a fetch when index-pack has stopped reading and the data cannot be written
// to it. Empty pack-data packets, which servers send as keepalives, are not
// data. The pack is already stored by then, so the fetch reports an error
// although its objects are in storage.
//
// The check is best effort: it sees only what the storage left unread.
// Filesystem storage reads the pack to its end and drops what follows the
// trailer, and memory storage's parser may have buffered past the trailer,
// so junk that either consumed goes unnoticed.
func DrainPackSection(demuxer io.Reader) error {
	n, err := io.Copy(io.Discard, demuxer)
	if err == nil && n > 0 {
		err = ErrPackJunk
	}
	return err
}
