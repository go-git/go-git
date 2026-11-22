// Package sshagent connects go-git's default SSH auth to the SSH agent of the
// current user. On Unix-like systems the agent is reached through the socket
// named by the SSH_AUTH_SOCK environment variable. On Windows it is reached
// through the Windows OpenSSH agent named pipe.
//
// The github.com/go-git/x/ssh/agent module supports other agents, including
// Pageant on Windows.
package sshagent

import (
	"errors"
	"net"

	"golang.org/x/crypto/ssh/agent"
)

// ErrNotFound is returned by New when no SSH agent can be reached.
var ErrNotFound = errors.New("no SSH agent found, use github.com/go-git/x/ssh/agent for other agents or Pageant on Windows")

// New connects to the SSH agent and returns a client for it together with the
// underlying connection. The caller owns the connection and must close it once
// the client is no longer needed.
func New() (agent.ExtendedAgent, net.Conn, error) {
	conn, err := dial()
	if err != nil {
		return nil, nil, err
	}
	return agent.NewClient(conn), conn, nil
}
