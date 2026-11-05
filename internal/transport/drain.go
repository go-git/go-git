package transport

import (
	"errors"
	"io"
)

// ErrPackJunk is returned when pack data follows the pack trailer. The wording
// is that of Git's index-pack, which reports it only for a pack read from a
// regular file. transport.ErrPackJunk aliases this value.
var ErrPackJunk = errors.New("pack has junk at the end")

// DrainPackSection reads the rest of a multiplexed pack response from demuxer,
// once the object storage has stored the pack, up to its closing flush-pkt.
// Progress still goes to the demuxer's Progress, and an error the server sends
// on the error band is returned.
//
// Pack data after the trailer fails the fetch with [ErrPackJunk]. This is
// stricter than Git, where the outcome depends on how the pack is unpacked
// and on timing. Git's demuxer writes such data into the pipe to index-pack
// or unpack-objects, and that write fails the fetch with "error in sideband
// demultiplexer" if the unpacker has already exited, which junk larger than
// the pipe guarantees. index-pack checks for junk only when its input is a
// regular file. On a fetch it writes what it read past the trailer to
// stdout, where fetch-pack rejects it as malformed output, but junk it never
// read, still in the pipe, lets the fetch succeed. unpack-objects, used for
// small fetches, writes what it read past the trailer to stdout as well, and
// the fetch succeeds (Git v2.56.0: builtin/index-pack.c:1302-1306 and
// 1656-1657, builtin/unpack-objects.c:693-694, fetch-pack.c:937-940 and
// 1103-1104, run-command.c:1150-1154).
// upload-pack never sends pack data after the pack, so go-git fails such a
// fetch every time rather than depend on timing.
//
// Empty pack-data packets, which servers send as keepalives, are not data.
// The pack is already stored by then, so the fetch reports an error although
// its objects are in storage.
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
