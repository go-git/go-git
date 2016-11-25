package ssh

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/src-d/go-git.v4/plumbing/transport"

	"golang.org/x/crypto/ssh"
)

var (
	errAlreadyConnected = errors.New("ssh session already created")
)

type client struct{}

// DefaultClient is the default SSH client.
var DefaultClient = &client{}

func (c *client) NewFetchPackSession(ep transport.Endpoint) (
	transport.FetchPackSession, error) {

	return newFetchPackSession(ep)
}

func (c *client) NewSendPackSession(ep transport.Endpoint) (
	transport.SendPackSession, error) {

	return newSendPackSession(ep)
}

type session struct {
	connected   bool
	endpoint    transport.Endpoint
	client      *ssh.Client
	session     *ssh.Session
	stdin       io.WriteCloser
	stdout      io.Reader
	stderr      io.Reader
	sessionDone chan error
	auth        AuthMethod
}

func (s *session) SetAuth(auth transport.AuthMethod) error {
	a, ok := auth.(AuthMethod)
	if !ok {
		return transport.ErrInvalidAuthMethod
	}

	s.auth = a
	return nil
}

// Close closes the SSH session.
func (s *session) Close() error {
	if !s.connected {
		return nil
	}

	s.connected = false

	//XXX: If did read the full packfile, then the session might be already
	//     closed.
	_ = s.session.Close()

	return s.client.Close()
}

// ensureConnected connects to the SSH server, unless a AuthMethod was set with
// SetAuth method, by default uses an auth method based on PublicKeysCallback,
// it connects to a SSH agent, using the address stored in the SSH_AUTH_SOCK
// environment var.
func (s *session) connect() error {
	if s.connected {
		return errAlreadyConnected
	}

	if err := s.setAuthFromEndpoint(); err != nil {
		return err
	}

	var err error
	s.client, err = ssh.Dial("tcp", s.getHostWithPort(), s.auth.clientConfig())
	if err != nil {
		return err
	}

	if err := s.openSSHSession(); err != nil {
		_ = s.client.Close()
		return err
	}

	s.connected = true
	return nil
}

func (s *session) getHostWithPort() string {
	host := s.endpoint.Host
	if strings.Index(s.endpoint.Host, ":") == -1 {
		host += ":22"
	}

	return host
}

func (s *session) setAuthFromEndpoint() error {
	var u string
	if info := s.endpoint.User; info != nil {
		u = info.Username()
	}

	var err error
	s.auth, err = NewSSHAgentAuth(u)
	return err
}

func (s *session) openSSHSession() error {
	var err error
	s.session, err = s.client.NewSession()
	if err != nil {
		return fmt.Errorf("cannot open SSH session: %s", err)
	}

	s.stdin, err = s.session.StdinPipe()
	if err != nil {
		return fmt.Errorf("cannot pipe remote stdin: %s", err)
	}

	s.stdout, err = s.session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("cannot pipe remote stdout: %s", err)
	}

	s.stderr, err = s.session.StderrPipe()
	if err != nil {
		return fmt.Errorf("cannot pipe remote stderr: %s", err)
	}

	return nil
}

func (s *session) runCommand(cmd string) chan error {
	done := make(chan error)
	go func() {
		done <- s.session.Run(cmd)
	}()

	return done
}

const (
	githubRepoNotFoundErr    = "ERROR: Repository not found."
	bitbucketRepoNotFoundErr = "conq: repository does not exist."
)

func isRepoNotFoundError(s string) bool {
	if strings.HasPrefix(s, githubRepoNotFoundErr) {
		return true
	}

	if strings.HasPrefix(s, bitbucketRepoNotFoundErr) {
		return true
	}

	return false
}
