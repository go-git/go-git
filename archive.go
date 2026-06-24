package git

import (
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

// ArchiveRemote creates an archive from a remote repository.
// It returns an io.ReadCloser that yields the archive data.
// The caller must close the returned ReadCloser.
func ArchiveRemote(url string, o *ArchiveOptions) (io.ReadCloser, error) {
	return ArchiveRemoteContext(context.Background(), url, o)
}

// ArchiveRemoteContext creates an archive from a remote repository.
// The provided Context can be used to cancel the operation.
func ArchiveRemoteContext(ctx context.Context, url string, o *ArchiveOptions) (io.ReadCloser, error) {
	if o == nil {
		o = &ArchiveOptions{}
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if url == "" {
		return nil, errors.New("remote URL is required")
	}

	cl, req, err := newClient(url, o.ClientOptions)
	if err != nil {
		return nil, err
	}

	req.Command = transport.UploadArchiveService
	sess, err := cl.Handshake(ctx, req)
	if err != nil {
		return nil, err
	}

	// Check if session implements Archiver
	arch, ok := sess.(transport.Archiver)
	if !ok {
		_ = sess.Close()
		return nil, transport.ErrArchiveUnsupported
	}

	// Build arguments
	var args []string
	if o.Format != "" {
		args = append(args, "--format="+o.Format)
	}
	if o.Prefix != "" {
		args = append(args, "--prefix="+o.Prefix)
	}
	args = append(args, o.Treeish)
	if len(o.Paths) > 0 {
		args = append(args, "--")
		args = append(args, o.Paths...)
	}

	return arch.Archive(ctx, &transport.ArchiveRequest{
		Args:     args,
		Progress: o.Progress,
	})
}
