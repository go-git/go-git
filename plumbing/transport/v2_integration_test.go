package transport

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

type V2IntegrationSuite struct {
	suite.Suite
}

func TestV2IntegrationSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(V2IntegrationSuite))
}

// uploadPackConn runs real `git upload-pack` against a repository with
// GIT_PROTOCOL=version=2, exposing its stdio as a transport.Conn so the
// client v2 code path can be exercised against canonical Git.
type uploadPackConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func newUploadPackConn(repoPath string) (*uploadPackConn, error) {
	cmd := exec.Command("git", "upload-pack", "--strict", repoPath)
	cmd.Env = append(os.Environ(), "GIT_PROTOCOL=version=2")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &uploadPackConn{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

func (c *uploadPackConn) Reader() io.Reader      { return c.stdout }
func (c *uploadPackConn) Writer() io.WriteCloser { return c.stdin }
func (c *uploadPackConn) Close() error {
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	_ = c.cmd.Wait()
	return nil
}

func (s *V2IntegrationSuite) repoPath() string {
	if _, err := exec.LookPath("git"); err != nil {
		s.T().Skip("git not installed")
	}
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	return dot.Root()
}

func (s *V2IntegrationSuite) TestStreamLsRefs() {
	conn, err := newUploadPackConn(s.repoPath())
	s.Require().NoError(err)

	sess, err := NewStreamSession(conn, UploadPackService)
	s.Require().NoError(err)
	defer sess.Close()

	_, isV2 := sess.(*v2Session)
	s.Require().True(isV2, "session should negotiate protocol v2")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refs, err := sess.GetRemoteRefs(ctx)
	s.Require().NoError(err)

	byName := map[string]*plumbing.Reference{}
	for _, r := range refs {
		byName[r.Name().String()] = r
	}
	s.Contains(byName, "refs/heads/master")
	s.Contains(byName, "HEAD")
}

func (s *V2IntegrationSuite) TestStreamFetch() {
	conn, err := newUploadPackConn(s.repoPath())
	s.Require().NoError(err)

	sess, err := NewStreamSession(conn, UploadPackService)
	s.Require().NoError(err)
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refs, err := sess.GetRemoteRefs(ctx)
	s.Require().NoError(err)

	var want plumbing.Hash
	for _, r := range refs {
		if r.Name().String() == "refs/heads/master" {
			want = r.Hash()
		}
	}
	s.Require().False(want.IsZero())

	st := memory.NewStorage()
	err = sess.Fetch(ctx, st, &FetchRequest{Wants: []plumbing.Hash{want}})
	s.Require().NoError(err)

	obj, err := st.EncodedObject(plumbing.CommitObject, want)
	s.Require().NoError(err)
	s.Equal(want, obj.Hash())
}
