// Package common implements the git pack protocol with a pluggable transport.
// This is a low-level package to implement new transports. Use a concrete
// implementation instead (e.g. http, file, ssh).
//
// A simple example of usage can be found in the file package.
package common

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/src-d/go-git.v4/plumbing/format/pktline"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/utils/ioutil"
)

// Commander creates Command instances. This is the main entry point for
// transport implementations.
type Commander interface {
	// Command creates a new Command for the given git command and
	// endpoint. cmd can be git-upload-pack or git-receive-pack. An
	// error should be returned if the endpoint is not supported or the
	// command cannot be created (e.g. binary does not exist, connection
	// cannot be established).
	Command(cmd string, ep transport.Endpoint) (Command, error)
}

// Command is used for a single command execution.
// This interface is modeled after exec.Cmd and ssh.Session in the standard
// library.
type Command interface {
	// SetAuth sets the authentication method.
	SetAuth(transport.AuthMethod) error
	// StderrPipe returns a pipe that will be connected to the command's
	// standard error when the command starts. It should not be called after
	// Start.
	StderrPipe() (io.Reader, error)
	// StdinPipe returns a pipe that will be connected to the command's
	// standard input when the command starts. It should not be called after
	// Start. The pipe should be closed when no more input is expected.
	StdinPipe() (io.WriteCloser, error)
	// StdoutPipe returns a pipe that will be connected to the command's
	// standard output when the command starts. It should not be called after
	// Start.
	StdoutPipe() (io.Reader, error)
	// Start starts the specified command. It does not wait for it to
	// complete.
	Start() error
	// Wait waits for the command to exit. It must have been started by
	// Start. The returned error is nil if the command runs, has no
	// problems copying stdin, stdout, and stderr, and exits with a zero
	// exit status.
	Wait() error
	// Close closes the command without waiting for it to exit and releases
	// any resources. It can be called to forcibly finish the command
	// without calling to Wait or to release resources after calling
	// Wait.
	Close() error
}

type client struct {
	cmdr Commander
}

// NewClient creates a new client using the given Commander.
func NewClient(runner Commander) transport.Client {
	return &client{runner}
}

// NewFetchPackSession creates a new FetchPackSession.
func (c *client) NewFetchPackSession(ep transport.Endpoint) (
	transport.FetchPackSession, error) {

	return c.newSession(transport.UploadPackServiceName, ep)
}

// NewSendPackSession creates a new SendPackSession.
func (c *client) NewSendPackSession(ep transport.Endpoint) (
	transport.SendPackSession, error) {

	return nil, errors.New("git send-pack not supported")
}

type session struct {
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
	Command Command

	advRefsRun bool
}

func (c *client) newSession(s string, ep transport.Endpoint) (*session, error) {
	cmd, err := c.cmdr.Command(s, ep)
	if err != nil {
		return nil, err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &session{
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Command: cmd,
	}, nil
}

// SetAuth delegates to the command's SetAuth.
func (s *session) SetAuth(auth transport.AuthMethod) error {
	return s.Command.SetAuth(auth)
}

// AdvertisedReferences retrieves the advertised references from the server.
func (s *session) AdvertisedReferences() (*packp.AdvRefs, error) {
	if s.advRefsRun {
		return nil, transport.ErrAdvertistedReferencesAlreadyCalled
	}

	defer func() { s.advRefsRun = true }()

	ar := packp.NewAdvRefs()
	if err := ar.Decode(s.Stdout); err != nil {
		if err != packp.ErrEmptyAdvRefs {
			return nil, err
		}

		_ = s.Stdin.Close()
		err = transport.ErrEmptyRemoteRepository

		scan := bufio.NewScanner(s.Stderr)
		if !scan.Scan() {
			return nil, transport.ErrEmptyRemoteRepository
		}

		if isRepoNotFoundError(string(scan.Bytes())) {
			return nil, transport.ErrRepositoryNotFound
		}

		return nil, err
	}

	transport.FilterUnsupportedCapabilities(ar.Capabilities)
	return ar, nil
}

// FetchPack performs a request to the server to fetch a packfile. A reader is
// returned with the packfile content. The reader must be closed after reading.
func (s *session) FetchPack(req *packp.UploadPackRequest) (io.ReadCloser, error) {
	if req.IsEmpty() {
		return nil, transport.ErrEmptyUploadPackRequest
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	if !s.advRefsRun {
		if _, err := s.AdvertisedReferences(); err != nil {
			return nil, err
		}
	}

	if err := fetchPack(s.Stdin, s.Stdout, req); err != nil {
		return nil, err
	}

	r, err := ioutil.NonEmptyReader(s.Stdout)
	if err == ioutil.ErrEmptyReader {
		if c, ok := s.Stdout.(io.Closer); ok {
			_ = c.Close()
		}

		return nil, transport.ErrEmptyUploadPackRequest
	}

	if err != nil {
		return nil, err
	}

	wc := &waitCloser{s.Command}
	rc := ioutil.NewReadCloser(r, wc)

	return rc, nil
}

func (s *session) Close() error {
	return s.Command.Close()
}

const (
	githubRepoNotFoundErr    = "ERROR: Repository not found."
	bitbucketRepoNotFoundErr = "conq: repository does not exist."
	localRepoNotFoundErr     = "does not appear to be a git repository"
)

func isRepoNotFoundError(s string) bool {
	if strings.HasPrefix(s, githubRepoNotFoundErr) {
		return true
	}

	if strings.HasPrefix(s, bitbucketRepoNotFoundErr) {
		return true
	}

	if strings.HasSuffix(s, localRepoNotFoundErr) {
		return true
	}

	return false
}

var (
	nak = []byte("NAK")
	eol = []byte("\n")
)

// fetchPack implements the git-fetch-pack protocol.
//
// TODO support multi_ack mode
// TODO support multi_ack_detailed mode
// TODO support acks for common objects
// TODO build a proper state machine for all these processing options
func fetchPack(w io.WriteCloser, r io.Reader,
	req *packp.UploadPackRequest) error {

	if err := req.UploadRequest.Encode(w); err != nil {
		return fmt.Errorf("sending upload-req message: %s", err)
	}

	if err := req.UploadHaves.Encode(w); err != nil {
		return fmt.Errorf("sending haves message: %s", err)
	}

	if err := sendDone(w); err != nil {
		return fmt.Errorf("sending done message: %s", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("closing input: %s", err)
	}

	if err := readNAK(r); err != nil {
		return fmt.Errorf("reading NAK: %s", err)
	}

	return nil
}

func sendDone(w io.Writer) error {
	e := pktline.NewEncoder(w)

	return e.Encodef("done\n")
}

func readNAK(r io.Reader) error {
	s := pktline.NewScanner(r)
	if !s.Scan() {
		return s.Err()
	}

	b := s.Bytes()
	b = bytes.TrimSuffix(b, eol)
	if !bytes.Equal(b, nak) {
		return fmt.Errorf("expecting NAK, found %q instead", string(b))
	}

	return nil
}

type waitCloser struct {
	Command Command
}

// Close waits until the command exits and returns error, if any.
func (c *waitCloser) Close() error {
	return c.Command.Wait()
}
