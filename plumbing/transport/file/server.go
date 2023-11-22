package file

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5/plumbing/server"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

// ServeUploadPack serves a git-upload-pack request using standard output, input
// and error. This is meant to be used when implementing a git-upload-pack
// command.
func ServeUploadPack(path string) error {
	ep, err := transport.NewEndpoint(path)
	if err != nil {
		return err
	}

	// TODO: define and implement a server-side AuthMethod
	s, err := server.DefaultServer.NewUploadPackSession(ep, nil)
	if err != nil {
		return fmt.Errorf("error creating session: %s", err)
	}

	return server.ServeUploadPack(srvCmd, s)
}

// ServeReceivePack serves a git-receive-pack request using standard output,
// input and error. This is meant to be used when implementing a
// git-receive-pack command.
func ServeReceivePack(path string) error {
	ep, err := transport.NewEndpoint(path)
	if err != nil {
		return err
	}

	// TODO: define and implement a server-side AuthMethod
	s, err := server.DefaultServer.NewReceivePackSession(ep, nil)
	if err != nil {
		return fmt.Errorf("error creating session: %s", err)
	}

	return server.ServeReceivePack(srvCmd, s)
}

var srvCmd = server.ServerCommand{
	Stdin:  os.Stdin,
	Stdout: ioutil.WriteNopCloser(os.Stdout),
	Stderr: os.Stderr,
}
