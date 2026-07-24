package git

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

type errObjectStorer struct {
	*memory.Storage
	err error
	// failOn limits the failure to a single hash.
	// The zero value fails every lookup.
	failOn plumbing.Hash
}

func (s *errObjectStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if s.failOn.IsZero() || h == s.failOn {
		return nil, s.err
	}
	return s.Storage.EncodedObject(t, h)
}

type objectWalkerSuite struct {
	BaseSuite
}

func TestObjectWalkerSuite(t *testing.T) {
	t.Parallel()

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

	// The shallow root is reachable from the cloned refs and
	// must have been covered by the walk.
	s.Contains(walker.seen, shallow[0])
}

func (s *objectWalkerSuite) TestUnexpectedErrors() {
	memStorage := memory.NewStorage()
	hash := plumbing.NewHash("c0ffee0000000000000000000000000000000000")
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

func (s *objectWalkerSuite) TestParentUnexpectedErrors() {
	memStorage := memory.NewStorage()

	treeObj := memStorage.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	w, err := treeObj.Writer()
	s.Require().NoError(err)
	s.Require().NoError(w.Close())
	treeHash, err := memStorage.SetEncodedObject(treeObj)
	s.Require().NoError(err)

	parentHash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	commit := &object.Commit{
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{parentHash},
		Author:       object.Signature{Name: "go-git", Email: "go-git@example.com"},
		Committer:    object.Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:      "tip",
	}
	commitObj := memStorage.NewEncodedObject()
	s.Require().NoError(commit.Encode(commitObj))
	commitHash, err := memStorage.SetEncodedObject(commitObj)
	s.Require().NoError(err)

	ref := plumbing.NewHashReference(plumbing.HEAD, commitHash)
	s.Require().NoError(memStorage.SetReference(ref))

	errStorer := &errObjectStorer{
		Storage: memStorage,
		err:     io.ErrUnexpectedEOF,
		failOn:  parentHash,
	}

	walker := newObjectWalker(errStorer)
	err = walker.walkAllRefs()
	s.Error(err)
	s.ErrorIs(err, io.ErrUnexpectedEOF)
}
