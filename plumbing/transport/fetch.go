package transport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// FetchPack fetches a packfile from the remote connection into the given
// storage repository and updates the shallow information.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	conn Connection,
	packf io.ReadCloser,
	shallowInfo *packp.ShallowUpdate,
	req *FetchRequest,
) (err error) {
	packf = ioutil.NewContextReadCloser(ctx, packf)

	// Do we have sideband enabled?
	var demuxer *sideband.Demuxer
	var reader io.Reader = packf
	caps := conn.Capabilities()
	if caps.Supports(capability.Sideband64k) {
		demuxer = sideband.NewDemuxer(sideband.Sideband64k, reader)
	} else if caps.Supports(capability.Sideband) {
		demuxer = sideband.NewDemuxer(sideband.Sideband, reader)
	}

	if demuxer != nil && req.Progress != nil {
		demuxer.Progress = req.Progress
		reader = demuxer
	}

	of := config.SHA1
	if req.Wants[0].Size() == config.SHA256.Size() {
		of = config.SHA256
	}

	if err := packfile.UpdateObjectStorage(st, reader, of); err != nil {
		return err
	}

	if err := packf.Close(); err != nil {
		return err
	}

	// Update shallow
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

	// unshallow commits
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
