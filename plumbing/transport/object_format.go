package transport

import (
	"slices"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

// sizeZeroIDs returns cmds with their zero old or new ids sized for f. Create
// and delete commands carry plumbing.ZeroHash, which encodes as 40 zeros;
// git's receive-pack parses ids at the repository's size and rejects a short
// zero id on a sha256 repository as a protocol error.
//
// cmds itself is returned when nothing needs resizing, which includes every
// sha1 request. Otherwise the result holds copies, and the request's own
// commands are not modified.
func sizeZeroIDs(cmds []*packp.Command, f config.ObjectFormat) []*packp.Command {
	hasZero := func(c *packp.Command) bool { return c.Old.IsZero() || c.New.IsZero() }
	if f == config.SHA1 || !slices.ContainsFunc(cmds, hasZero) {
		return cmds
	}

	copies := make([]packp.Command, len(cmds))
	out := make([]*packp.Command, len(cmds))
	for i, c := range cmds {
		copies[i] = *c
		if c.Old.IsZero() {
			copies[i].Old.ResetBySize(f.Size())
		}
		if c.New.IsZero() {
			copies[i].New.ResetBySize(f.Size())
		}
		out[i] = &copies[i]
	}
	return out
}
