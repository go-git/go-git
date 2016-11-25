// Package ssh implements a ssh client for go-git.
package ssh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/advrefs"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/pktline"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/ulreq"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/utils/ioutil"

	"golang.org/x/crypto/ssh"
)

type fetchPackSession struct {
	*session
	cmdRun     bool
	advRefsRun bool
	done       chan error
}

func newFetchPackSession(ep transport.Endpoint) (*fetchPackSession, error) {
	s := &fetchPackSession{
		session: &session{
			endpoint: ep,
		},
	}
	if err := s.connect(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *fetchPackSession) AdvertisedReferences() (*transport.UploadPackInfo, error) {
	if s.advRefsRun {
		return nil, transport.ErrAdvertistedReferencesAlreadyCalled
	}

	defer func() { s.advRefsRun = true }()

	if err := s.ensureRunCommand(); err != nil {
		return nil, err
	}

	i := transport.NewUploadPackInfo()
	if err := i.Decode(s.stdout); err != nil {
		if err != advrefs.ErrEmpty {
			return nil, err
		}

		_ = s.stdin.Close()
		scan := bufio.NewScanner(s.stderr)
		if !scan.Scan() {
			return nil, transport.ErrEmptyRemoteRepository
		}

		if isRepoNotFoundError(string(scan.Bytes())) {
			return nil, transport.ErrRepositoryNotFound
		}

		return nil, err
	}

	return i, nil
}

// FetchPack returns a packfile for a given upload request.
// Closing the returned reader will close the SSH session.
func (s *fetchPackSession) FetchPack(req *transport.UploadPackRequest) (
	io.ReadCloser, error) {

	if req.IsEmpty() {
		return nil, transport.ErrEmptyUploadPackRequest
	}

	if !s.advRefsRun {
		if _, err := s.AdvertisedReferences(); err != nil {
			return nil, err
		}
	}

	if err := fetchPack(s.stdin, s.stdout, req); err != nil {
		return nil, err
	}

	fs := &fetchSession{
		Reader:  s.stdout,
		session: s.session.session,
		done:    s.done,
	}

	r, err := ioutil.NonEmptyReader(fs)
	if err == ioutil.ErrEmptyReader {
		_ = fs.Close()
		return nil, transport.ErrEmptyUploadPackRequest
	}

	return ioutil.NewReadCloser(r, fs), nil
}

func (s *fetchPackSession) ensureRunCommand() error {
	if s.cmdRun {
		return nil
	}

	s.cmdRun = true
	s.done = s.runCommand(s.getCommand())
	return nil
}

type fetchSession struct {
	io.Reader
	session *ssh.Session
	done    <-chan error
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

func (s *fetchPackSession) getCommand() string {
	directory := s.endpoint.Path
	directory = directory[1:]

	return fmt.Sprintf("git-upload-pack '%s'", directory)
}

var (
	nak = []byte("NAK")
	eol = []byte("\n")
)

// FetchPack implements the git-fetch-pack protocol.
//
// TODO support multi_ack mode
// TODO support multi_ack_detailed mode
// TODO support acks for common objects
// TODO build a proper state machine for all these processing options
func fetchPack(w io.WriteCloser, r io.Reader,
	req *transport.UploadPackRequest) error {

	if err := sendUlReq(w, req); err != nil {
		return fmt.Errorf("sending upload-req message: %s", err)
	}

	if err := sendHaves(w, req); err != nil {
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

func sendUlReq(w io.Writer, req *transport.UploadPackRequest) error {
	ur := ulreq.New()
	ur.Wants = req.Wants
	ur.Depth = ulreq.DepthCommits(req.Depth)
	e := ulreq.NewEncoder(w)

	return e.Encode(ur)
}

func sendHaves(w io.Writer, req *transport.UploadPackRequest) error {
	e := pktline.NewEncoder(w)
	for _, have := range req.Haves {
		if err := e.Encodef("have %s\n", have); err != nil {
			return fmt.Errorf("sending haves for %q: %s", have, err)
		}
	}

	if len(req.Haves) != 0 {
		if err := e.Flush(); err != nil {
			return fmt.Errorf("sending flush-pkt after haves: %s", err)
		}
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
