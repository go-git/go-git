//go:build !windows

package sshagent

import (
	"fmt"
	"net"
	"os"

	"github.com/go-git/go-git/v6/utils/trace"
)

func dial() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, fmt.Errorf("%w: SSH_AUTH_SOCK is not set", ErrNotFound)
	}

	trace.SSH.Printf("ssh: dialing SSH agent at %s", sock)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return conn, nil
}
