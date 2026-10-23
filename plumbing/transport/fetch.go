package transport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// FetchPack fetches a packfile from the remote into the given storage.
//
// caps must be the capabilities the fetch was negotiated with: as
// [NegotiatePack] requests side-band only when req.Progress is set, packf is
// read as multiplexed only when caps support side-band and req.Progress is
// non-nil. A multiplexed response is read up to its closing flush-pkt, and
// an error the remote sends on the error band, even after the pack, fails
// the fetch. Otherwise packf may be left unread past the pack trailer.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	caps capability.List,
	packf io.ReadCloser,
	shallowInfo *packp.ShallowUpdate,
	req *FetchRequest,
) error {
	packf = ioutil.NewContextReadCloser(ctx, packf)

	var demuxer *sideband.Demuxer
	var reader io.Reader = packf
	if caps.Supports(capability.Sideband64k) {
		demuxer = sideband.NewDemuxer(sideband.Sideband64k, reader)
	} else if caps.Supports(capability.Sideband) {
		demuxer = sideband.NewDemuxer(sideband.Sideband, reader)
	}

	// Sideband is only requested when there is progress to report (see
	// NegotiatePack), so only then is the response muxed.
	muxed := demuxer != nil && req.Progress != nil
	if muxed {
		demuxer.Progress = req.Progress
		reader = demuxer
	}

	// A filtered fetch deliberately leaves out objects, so the pack has to be
	// recorded as coming from a promisor remote. Git otherwise reads those
	// absences as corruption: fsck reports broken links to them and gc fails
	// with "unable to read".
	//
	// The marker is left empty. Git fills it with the refs it sought on this
	// path (fetch-pack.c create_promisor_file) and leaves it empty when
	// repacking (repack-promisor.c), and accepts either, because only the
	// file's presence is ever consulted — packfile.c tests it with access(2)
	// and never opens it.
	if req.Filter != "" {
		if err := packfile.UpdatePromisorObjectStorage(st, reader, ""); err != nil {
			return err
		}
	} else if err := packfile.UpdateObjectStorage(st, reader); err != nil {
		return err
	}

	// Storage that parses the pack stops reading at its trailer, so read the
	// rest of the response here: an error the server sends after the pack
	// must fail the fetch, and trailing progress must reach the caller. The
	// demuxer stops at the closing flush-pkt; an unmuxed response has no end
	// marker, so reading on would wait for the server to close.
	if muxed {
		if _, err := io.Copy(io.Discard, demuxer); err != nil {
			return err
		}
	}

	if err := packf.Close(); err != nil {
		return err
	}

	if shallowInfo != nil {
		if err := updateShallow(st, shallowInfo); err != nil {
			return err
		}
	}

	return nil
}

func updateShallow(st storage.Storer, shallowInfo *packp.ShallowUpdate) error {
	shallows, err := st.Shallow()
	if err != nil {
		return err
	}

outer:
	for _, s := range shallowInfo.Shallows {
		for _, oldS := range shallows {
			if s == oldS {
				continue outer
			}
		}
		shallows = append(shallows, s)
	}

	for _, s := range shallowInfo.Unshallows {
		for i, oldS := range shallows {
			if s == oldS {
				shallows = append(shallows[:i], shallows[i+1:]...)
				break
			}
		}
	}

	return st.SetShallow(shallows)
}
