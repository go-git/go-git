// Package ssh implements a ssh client for go-git.
package ssh

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing/format/pktline"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
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

func (s *fetchPackSession) AdvertisedReferences() (*packp.AdvRefs, error) {
	if s.advRefsRun {
		return nil, transport.ErrAdvertistedReferencesAlreadyCalled
	}

	defer func() { s.advRefsRun = true }()

	if err := s.ensureRunCommand(); err != nil {
		return nil, err
	}

	ar := packp.NewAdvRefs()
	if err := ar.Decode(s.stdout); err != nil {
		if err != packp.ErrEmptyAdvRefs {
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

	return ar, nil
}

// FetchPack returns a packfile for a given upload request.
// Closing the returned reader will close the SSH session.
func (s *fetchPackSession) FetchPack(req *packp.UploadPackRequest) (
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
