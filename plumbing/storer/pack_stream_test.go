package storer_test

import (
	"context"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

type fakeStreamer struct{}

func (fakeStreamer) StreamPack(context.Context, io.Writer, []plumbing.Hash, []plumbing.Hash, storer.PackStreamOptions) error {
	return nil
}

type fakeWalker struct{}

func (fakeWalker) PackObjects(context.Context, []plumbing.Hash, []plumbing.Hash) ([]plumbing.Hash, error) {
	return nil, nil
}

func TestPackStreamerInterfaceShape(t *testing.T) {
	t.Parallel()
	var _ storer.PackStreamer = fakeStreamer{}
	var _ storer.PackObjectWalker = fakeWalker{}

	opts := storer.PackStreamOptions{
		ThinPack:             true,
		SkipDeltaCompression: false,
		PackWindow:           10,
		ObjectFormat:         config.SHA1,
		Shallow:              []plumbing.Hash{plumbing.ZeroHash},
	}
	if opts.PackWindow != 10 {
		t.Fatalf("PackWindow round-trip failed: %d", opts.PackWindow)
	}
}
