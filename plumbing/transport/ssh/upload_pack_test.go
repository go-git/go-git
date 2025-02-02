package ssh

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	testutils "github.com/go-git/go-git/v5/internal/transport/ssh/test"
	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/gliderlabs/ssh"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	stdssh "golang.org/x/crypto/ssh"
)

type UploadPackSuite struct {
	test.UploadPackSuite
	opts []ssh.Option

	port int
	base string
}

func (s *UploadPackSuite) TearDownSuite() {
	fixtures.Clean()
}

func (s *UploadPackSuite) SetupSuite() {
	if runtime.GOOS == "js" {
		s.T().Skip("tcp connections are not available in wasm")
	}

	l, err := net.Listen("tcp", "localhost:0")
	s.NoError(err)

	s.port = l.Addr().(*net.TCPAddr).Port
	s.base = s.T().TempDir()

	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}

	s.UploadPackSuite.Client = NewTransport(&stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	})

	s.UploadPackSuite.Endpoint = s.prepareRepository(fixtures.Basic().One(), "basic.git")
	s.UploadPackSuite.EmptyEndpoint = s.prepareRepository(fixtures.ByTag("empty").One(), "empty.git")
	s.UploadPackSuite.NonExistentEndpoint = s.newEndpoint("non-existent.git")

	server := &ssh.Server{Handler: testutils.HandlerSSH}
	for _, opt := range s.opts {
		opt(server)
	}
	go func() {
		log.Fatal(server.Serve(l))
	}()
}

func (s *UploadPackSuite) prepareRepository(f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	s.NoError(err)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	s.NoError(err)

	return s.newEndpoint(name)
}

func (s *UploadPackSuite) newEndpoint(name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf(
		"ssh://git@localhost:%d/%s/%s", s.port, filepath.ToSlash(s.base), name,
	))

	s.NoError(err)
	return ep
}

func handlerSSH(s ssh.Session) {
	cmd, stdin, stderr, stdout, err := buildCommand(s.Command())
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		defer stdin.Close()
		io.Copy(stdin, s)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(s.Stderr(), stderr)
	}()

	go func() {
		defer wg.Done()
		io.Copy(s, stdout)
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		return
	}
}

func buildCommand(c []string) (cmd *exec.Cmd, stdin io.WriteCloser, stderr, stdout io.ReadCloser, err error) {
	if len(c) != 2 {
		err = fmt.Errorf("invalid command")
		return
	}

	// fix for Windows environments
	path := strings.Replace(c[1], "/C:/", "C:/", 1)

	cmd = exec.Command(c[0], path)
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return
	}

	stdin, err = cmd.StdinPipe()
	if err != nil {
		return
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return
	}

	return
}
