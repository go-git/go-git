package rad

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/file"
)

// Options configures the rad transport.
type Options struct {
	// Home is the Radicle home directory containing the `storage/` tree.
	// If empty, RAD_HOME is used, falling back to $HOME/.radicle
	// ($USERPROFILE/.radicle on Windows).
	Home string
	// AllRefs advertises every reference in storage, including
	// refs/rad/* and refs/namespaces/*, instead of the default
	// canonical view (HEAD, refs/heads/*, refs/tags/*). It has no
	// effect on namespaced URLs (rad://<rid>/<nid>), which always
	// expose only that namespace's refs. The view stays read-only
	// either way.
	AllRefs bool
}

// Transport implements the rad:// protocol: read-only access to local
// Radicle storage, by composing go-git's file transport with a Loader that
// resolves rad://<rid>[/<nid>] to $RAD_HOME/storage/<rid> and applies a
// reference view over it. See the package doc for URL forms and scope.
type Transport struct {
	file *file.Transport
}

// NewTransport creates a rad transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{
		file: file.NewTransport(file.Options{Loader: newLoader(opts)}),
	}
}

// unsupported reports whether cmd is rejected. git-receive-pack is refused
// because writing raw refs into Radicle storage would leave it inconsistent
// until refs/rad/sigrefs is re-signed with the device key, which this
// transport does not do; git-upload-archive is refused because it needs a
// worktree-ish view that Radicle storage does not advertise.
func unsupported(cmd string) bool {
	return cmd == transport.ReceivePackService || cmd == transport.UploadArchiveService
}

// Connect opens a raw connection for req, delegating to the embedded file
// transport. It returns transport.ErrCommandUnsupported for
// git-receive-pack and git-upload-archive.
func (t *Transport) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	if unsupported(req.Command) {
		return nil, fmt.Errorf("%w: %s", transport.ErrCommandUnsupported, req.Command)
	}
	return t.file.Connect(ctx, req)
}
