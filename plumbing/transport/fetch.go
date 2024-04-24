package transport

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

type NoMatchingRefSpecError struct {
	RefSpec config.RefSpec
}

func (e NoMatchingRefSpecError) Error() string {
	return fmt.Sprintf("couldn't find remote ref %q", e.RefSpec.Src())
}

func (e NoMatchingRefSpecError) Is(target error) bool {
	_, ok := target.(NoMatchingRefSpecError)
	return ok
}

// FetchPack fetches a packfile from the remote connection into the given
// storage repository and updates the shallow information.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	conn Connection,
	req *FetchRequest,
) (err error) {
	res, err := conn.Fetch(ctx, req)
	if err != nil {
		return err
	}

	// Do we have sideband enabled?
	var demuxer *sideband.Demuxer
	var reader io.Reader = res.Packfile
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

	log.Printf("updating repository with packfile")
	if err := packfile.UpdateObjectStorage(st, reader); err != nil {
		return err
	}

	if err := res.Packfile.Close(); err != nil {
		return err
	}

	log.Printf("updating shallows")

	// Update shallow
	if len(res.Shallows) > 0 {
		if err := updateShallow(st, res.Shallows); err != nil {
			return err
		}
	}

	log.Printf("fetch-pack done")

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
