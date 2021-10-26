package file

import (
	"fmt"
	"os"

	urlparse "github.com/go-git/go-git/v5/internal/url"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/internal/common"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

func newEndpoint(path string) (*transport.Endpoint, error) {
	if !urlparse.MatchesScpLike(path) && !urlparse.MatchesScpLikeExtended(path) {
		return transport.NewEndpointScpCorrect(path)
	}
	return transport.NewEndpoint(path)
}

// ServeUploadPack serves a git-upload-pack request using standard output, input
// and error. This is meant to be used when implementing a git-upload-pack
// command.
func ServeUploadPack(path string) error {
	ep, err := newEndpoint(path)
	if err != nil {
		return err
	}

	// TODO: define and implement a server-side AuthMethod
	s, err := server.DefaultServer.NewUploadPackSession(ep, nil)
	if err != nil {
		return fmt.Errorf("error creating session: %s", err)
	}

	return common.ServeUploadPack(srvCmd, s)
}

// ServeReceivePack serves a git-receive-pack request using standard output,
// input and error. This is meant to be used when implementing a
// git-receive-pack command.
func ServeReceivePack(path string) error {
	ep, err := newEndpoint(path)
	if err != nil {
		return err
	}

	// TODO: define and implement a server-side AuthMethod
	s, err := server.DefaultServer.NewReceivePackSession(ep, nil)
	if err != nil {
		return fmt.Errorf("error creating session: %s", err)
	}

	return common.ServeReceivePack(srvCmd, s)
}

var srvCmd = common.ServerCommand{
	Stdin:  os.Stdin,
	Stdout: ioutil.WriteNopCloser(os.Stdout),
	Stderr: os.Stderr,
}
