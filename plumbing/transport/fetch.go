package transport

import (
	"context"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// FetchPack fetches a packfile from the remote into the given storage.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	caps capability.List,
	packf io.ReadCloser,
	shallowInfo *packp.ShallowUpdate,
	req *FetchRequest,
) error {
	packf = ioutil.NewContextReadCloser(ctx, packf)

	var reader io.Reader = packf
	if req.Progress != nil {
		maxSize := 0
		switch {
		case caps.Supports(capability.Sideband64k):
			maxSize = pktline.MaxSize
		case caps.Supports(capability.Sideband):
			maxSize = pktline.DefaultSize
		}
		if maxSize > 0 {
			reader = pktline.NewSidebandReader(reader, req.Progress, maxSize)
		}
	}

	if err := packfile.UpdateObjectStorage(st, reader); err != nil {
		return err
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
