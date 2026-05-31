package test

import (
	"io"
	"net"
	"os/exec"
	"sync"
	"testing"

	glssh "github.com/gliderlabs/ssh"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh/testdata"
)

// StartGitSSHServer starts an SSH server that serves git by running the
// requested git command over the session, accepting any authentication. It
// returns the listening address; the server is closed on t.Cleanup.
func StartGitSSHServer(t testing.TB) *net.TCPAddr {
	t.Helper()

	l := ListenTCP(t)
	server := &glssh.Server{Handler: func(s glssh.Session) {
		args := s.Command()
		if len(args) < 2 {
			_ = s.Exit(1)
			return
		}
		cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // test server
		stdout, _ := cmd.StdoutPipe()
		stdin, _ := cmd.StdinPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			_ = s.Exit(1)
			return
		}
		go func() { defer func() { _ = stdin.Close() }(); _, _ = io.Copy(stdin, s) }()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = io.Copy(s.Stderr(), stderr) }()
		go func() { defer wg.Done(); _, _ = io.Copy(s, stdout) }()
		wg.Wait()
		if cmd.Wait() != nil {
			_ = s.Exit(1)
			return
		}
		_ = s.Exit(0)
	}}
	require.NoError(t, server.SetOption(glssh.HostKeyPEM(testdata.PEMBytes["ed25519"])))

	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(l) }()
	t.Cleanup(func() { _ = l.Close(); <-done })

	return l.Addr().(*net.TCPAddr)
}
