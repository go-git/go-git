package git

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/suite"
)

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
