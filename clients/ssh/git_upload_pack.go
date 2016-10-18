// Package ssh implements a ssh client for go-git.
package ssh

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"

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
	return i, i.Decode(pktline.NewScanner(bytes.NewReader(out)))
}

// Disconnect the SSH client.
func (s *GitUploadPackService) Disconnect() (err error) {
	if !s.connected {
		return ErrNotConnected
	}
	s.connected = false
	return s.client.Close()
}

// Fetch retrieves the GitUploadPack form the repository.
// You must be connected to the repository before using this method
// (using the ConnectWithAuth() method).
// TODO: fetch should really reuse the info session instead of openning a new
// one
func (s *GitUploadPackService) Fetch(r *common.GitUploadPackRequest) (rc io.ReadCloser, err error) {
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

	si, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}

	so, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := session.Start(s.getCommand()); err != nil {
		return nil, err
	}

	go func() {
		fmt.Fprintln(si, r.String())
		err = si.Close()
	}()

	// TODO: investigate this *ExitError type (command fails or
	// doesn't complete successfully), as it is happenning all
	// the time, but everything seems to work fine.
	if err := session.Wait(); err != nil {
		if _, ok := err.(*ssh.ExitError); !ok {
			return nil, err
		}
	}

	// read until the header of the second answer
	soBuf := bufio.NewReader(so)
	token := "0000"
	for {
		var line string
		line, err = soBuf.ReadString('\n')
		if err == io.EOF {
			return nil, ErrUploadPackAnswerFormat
		}
		if line[0:len(token)] == token {
			break
		}
	}

	data, err := ioutil.ReadAll(soBuf)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(data)
	return ioutil.NopCloser(buf), nil
}

func (s *GitUploadPackService) getCommand() string {
	directory := s.endpoint.Path
	directory = directory[1:len(directory)]

	return fmt.Sprintf("git-upload-pack %s", directory)
}
