package transport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

// FetchPack fetches a packfile from the remote connection into the given
// storage repository and updates the shallow information.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	conn Connection,
	packf io.ReadCloser,
	shallows []plumbing.Hash,
	req *FetchRequest,
) (err error) {
	// Do we have sideband enabled?
	var demuxer *sideband.Demuxer
	var reader io.Reader = packf
	caps := conn.Capabilities()
	if caps.Supports(capability.Sideband) {
		demuxer = sideband.NewDemuxer(sideband.Sideband, reader)
	}
	if caps.Supports(capability.Sideband64k) {
		demuxer = sideband.NewDemuxer(sideband.Sideband64k, reader)
	}

	if demuxer != nil && req.Progress != nil {
		demuxer.Progress = req.Progress
		reader = demuxer
	}

	if err := packfile.UpdateObjectStorage(st, reader); err != nil {
		return err
	}

	if err := packf.Close(); err != nil {
		return err
	}

	// Update shallow
	if len(shallows) > 0 {
		if err := updateShallow(st, shallows); err != nil {
			return err
		}
	}

	return nil
}

func updateShallow(st storage.Storer, remoteShallows []plumbing.Hash) error {
	if len(remoteShallows) == 0 {
		return nil
	}

	shallows, err := st.Shallow()
	if err != nil {
		return err
	}

outer:
	for _, s := range remoteShallows {
		for _, oldS := range shallows {
			if s == oldS {
				continue outer
			}
		}
		shallows = append(shallows, s)
	}

	return st.SetShallow(shallows)
}
