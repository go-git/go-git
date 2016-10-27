// Package ssh implements a ssh client for go-git.
package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/formats/packp/advrefs"

	"golang.org/x/crypto/ssh"
)

// New errors introduced by this package.
var (
	ErrInvalidAuthMethod      = errors.New("invalid ssh auth method")
	ErrAuthRequired           = errors.New("cannot connect: auth required")
	ErrNotConnected           = errors.New("not connected")
	ErrAlreadyConnected       = errors.New("already connected")
	ErrUploadPackAnswerFormat = errors.New("git-upload-pack bad answer format")
	ErrUnsupportedVCS         = errors.New("only git is supported")
	ErrUnsupportedRepo        = errors.New("only github.com is supported")

	nak = []byte("0008NAK\n")
)

// GitUploadPackService holds the service information.
// The zero value is safe to use.
type GitUploadPackService struct {
	connected bool
	endpoint  common.Endpoint
	client    *ssh.Client
	auth      AuthMethod
}

// NewGitUploadPackService initialises a GitUploadPackService,
func NewGitUploadPackService(endpoint common.Endpoint) common.GitUploadPackService {
	return &GitUploadPackService{endpoint: endpoint}
}

// Connect connects to the SSH server, unless a AuthMethod was set with SetAuth
// method, by default uses an auth method based on PublicKeysCallback, it
// connects to a SSH agent, using the address stored in the SSH_AUTH_SOCK
// environment var
func (s *GitUploadPackService) Connect() error {
	if s.connected {
		return ErrAlreadyConnected
	}

	if err := s.setAuthFromEndpoint(); err != nil {
		return err
	}

	var err error
	s.client, err = ssh.Dial("tcp", s.getHostWithPort(), s.auth.clientConfig())
	if err != nil {
		return err
	}

	s.connected = true
	return nil
}

func (s *GitUploadPackService) getHostWithPort() string {
	host := s.endpoint.Host
	if strings.Index(s.endpoint.Host, ":") == -1 {
		host += ":22"
	}

	return host
}

func (s *GitUploadPackService) setAuthFromEndpoint() error {
	var u string
	if info := s.endpoint.User; info != nil {
		u = info.Username()
	}

	var err error
	s.auth, err = NewSSHAgentAuth(u)
	if err != nil {
		return err
	}

	return nil
}

// SetAuth sets the AuthMethod
func (s *GitUploadPackService) SetAuth(auth common.AuthMethod) error {
	var ok bool
	s.auth, ok = auth.(AuthMethod)
	if !ok {
		return ErrInvalidAuthMethod
	}

	return nil
}

// Info returns the GitUploadPackInfo of the repository. The client must be
// connected with the repository (using the ConnectWithAuth() method) before
// using this method.
func (s *GitUploadPackService) Info() (i *common.GitUploadPackInfo, err error) {
	if !s.connected {
		return nil, ErrNotConnected
	}

	session, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer func() {
		// the session can be closed by the other endpoint,
		// therefore we must ignore a close error.
		_ = session.Close()
	}()

	out, err := session.Output(s.getCommand())
	if err != nil {
		return nil, err
	}

	i = common.NewGitUploadPackInfo()
	return i, i.Decode(bytes.NewReader(out))
}

// Disconnect the SSH client.
func (s *GitUploadPackService) Disconnect() (err error) {
	if !s.connected {
		return ErrNotConnected
	}
	s.connected = false
	return s.client.Close()
}

// Fetch returns a packfile for a given upload request.  It opens a new
// SSH session on a connected GitUploadPackService, sends the given
// upload request to the server and returns a reader for the received
// packfile.  Closing the returned reader will close the SSH session.
func (s *GitUploadPackService) Fetch(r *common.GitUploadPackRequest) (rc io.ReadCloser, err error) {
	if !s.connected {
		return nil, ErrNotConnected
	}

	session, err := s.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("cannot open SSH session: %s", err)
	}

	si, err := session.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot pipe remote stdin: %s", err)
	}

	so, err := session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot pipe remote stdout: %s", err)
	}

	done := make(chan error)
	go func() {
		done <- session.Run(s.getCommand())
	}()

	if err := skipAdvRef(so); err != nil {
		return nil, fmt.Errorf("skipping advertised-refs: %s", err)
	}

	// send the upload request
	_, err = io.Copy(si, r.Reader())
	if err != nil {
		return nil, fmt.Errorf("sending upload-req message: %s", err)
	}

	if err := si.Close(); err != nil {
		return nil, fmt.Errorf("closing input: %s", err)
	}

	// TODO support multi_ack mode
	// TODO support multi_ack_detailed mode
	// TODO support acks for common objects
	// TODO build a proper state machine for all these processing options
	buf := make([]byte, len(nak))
	if _, err := io.ReadFull(so, buf); err != nil {
		return nil, fmt.Errorf("looking for NAK: %s", err)
	}
	if !bytes.Equal(buf, nak) {
		return nil, fmt.Errorf("NAK answer not found")
	}

	return &fetchSession{
		Reader:  so,
		session: session,
		done:    done,
	}, nil
}

func skipAdvRef(so io.Reader) error {
	d := advrefs.NewDecoder(so)
	ar := advrefs.New()

	return d.Decode(ar)
}

type fetchSession struct {
	io.Reader
	session *ssh.Session
	done    chan error
}

// Close closes the session and collects the output state of the remote
// SSH command.
//
// If both the remote command and the closing of the session completes
// susccessfully it returns nil.
//
// If the remote command completes unsuccessfully or is interrupted by a
// signal, it returns the corresponding *ExitError.
//
// Otherwise, if clossing the SSH session fails it returns the close
// error.  Closing the session when the other has already close it is
// not cosidered an error.
func (f *fetchSession) Close() (err error) {
	if err := <-f.done; err != nil {
		return err
	}

	if err := f.session.Close(); err != nil && err != io.EOF {
		return err
	}

	return nil
}

func (s *GitUploadPackService) getCommand() string {
	directory := s.endpoint.Path
	directory = directory[1:len(directory)]

	return fmt.Sprintf("git-upload-pack '%s'", directory)
}
