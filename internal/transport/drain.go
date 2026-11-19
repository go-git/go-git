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
// Pack data after the trailer fails the fetch with [ErrPackJunk]. Git's
// outcome depends on how the pack is unpacked and on timing. Its demuxer
// writes such data into the pipe to index-pack or unpack-objects, and that
// write fails the fetch with "error in sideband demultiplexer" if the
// unpacker has already exited, which junk larger than the pipe plus what
// the unpacker reads ahead (up to 128 KiB) guarantees. index-pack checks for
// junk only when its input is a regular file. On a fetch it writes what it
// read past the trailer to stdout. fetch-pack reads that only when it keeps
// the pack, and then rejects it as malformed output unless it is made of
// hex lines, which it takes as gitmodules OIDs. Junk the unpacker never
// read, still in the pipe, lets the fetch succeed. unpack-objects, used for
// small fetches, writes what it read past the trailer to stdout as well, and
// the fetch succeeds (Git v2.56.0: builtin/index-pack.c:1302-1306 and
// 1656-1657, builtin/unpack-objects.c:693-694, fetch-pack.c:924-942,
// 1076-1089 and 1103-1104, run-command.c:1150-1154).
// upload-pack never sends pack data after the pack, so the check fails
// such a fetch where the storage leaves the junk unread, rather than depend
// on timing.
//
// Empty pack-data packets, which servers send as keepalives, are not data.
// The pack is already stored by then, so the fetch reports an error although
// its objects are in storage.
//
// The check is best effort: it sees only what the storage left unread.
// Filesystem storage, the default, reads the pack source in 64 KiB chunks
// and stops at the trailer, and memory storage's parser buffers too, so
// each drops junk that arrives within the last chunk it read and reports
// only what follows.
func DrainPackSection(demuxer io.Reader) error {
	n, err := io.Copy(io.Discard, demuxer)
	if err == nil && n > 0 {
		err = ErrPackJunk
	}
	return err
}
