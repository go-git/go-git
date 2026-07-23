package git

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"
)

type errObjectStorer struct {
	*memory.Storage
	err error
}

func (s *errObjectStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	return nil, s.err
}

type objectWalkerSuite struct {
	BaseSuite
}

func TestObjectWalkerSuite(t *testing.T) {
	suite.Run(t, new(objectWalkerSuite))
}

func (s *objectWalkerSuite) TestNormalClonedRepo() {
	t := s.T()
	local := t.TempDir()

	cmd := exec.Command(
		"git",
		"clone",
		"--no-checkout",
		"file://"+s.GetBasicLocalRepositoryURL(),
		local,
	)
	cmd.Dir = local
	cmd.Env = os.Environ()
	buf := &bytes.Buffer{}
	cmd.Stderr = buf
	cmd.Stdout = buf
	err := cmd.Run()
	s.NoError(err, buf.String())

	r, err := PlainOpen(local)
	s.Require().NoError(err)

	shallow, err := r.Storer.Shallow()
	s.Require().NoError(err)
	s.Empty(shallow)

	walker := newObjectWalker(r.Storer)
	err = walker.walkAllRefs()
	s.Require().NoError(err)
}

func (s *objectWalkerSuite) TestShallowClonedRepo() {
	t := s.T()
	local := t.TempDir()

	cmd := exec.Command(
		"git",
		"clone",
		"--no-checkout",
		"--bare",
		"--depth", "2",
		"file://"+s.GetBasicLocalRepositoryURL(),
		local,
	)
	cmd.Dir = local
	cmd.Env = os.Environ()
	buf := &bytes.Buffer{}
	cmd.Stderr = buf
	cmd.Stdout = buf
	err := cmd.Run()
	s.NoError(err, buf.String())

	r, err := PlainOpen(local)
	s.Require().NoError(err)

	shallow, err := r.Storer.Shallow()
	s.Require().NoError(err)
	s.NotEmpty(shallow)

	walker := newObjectWalker(r.Storer)
	err = walker.walkAllRefs()
	s.Require().NoError(err)
}

func (s *objectWalkerSuite) TestUnexpectedErrors() {
	memStorage := memory.NewStorage()
	hash := plumbing.NewHash("c0ffee00000000000000000000000000000000000")
	ref := plumbing.NewHashReference(plumbing.HEAD, hash)
	err := memStorage.SetReference(ref)
	s.Require().NoError(err)

	errStorer := &errObjectStorer{
		Storage: memStorage,
		err:     io.ErrUnexpectedEOF,
	}

	walker := newObjectWalker(errStorer)
	err = walker.walkAllRefs()
	s.Error(err)
	s.ErrorIs(err, io.ErrUnexpectedEOF)
}
