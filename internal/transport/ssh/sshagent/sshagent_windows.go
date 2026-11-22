//go:build windows

package sshagent

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"

	"github.com/go-git/go-git/v6/utils/trace"
)

// openSSHAgentPipe is the named pipe served by the Windows OpenSSH agent.
const openSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

func dial() (net.Conn, error) {
	return dialPipe(openSSHAgentPipe)
}

func dialPipe(name string) (net.Conn, error) {
	trace.SSH.Printf("ssh: dialing SSH agent at %s", name)
	conn, err := winio.DialPipe(name, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return conn, nil
}
