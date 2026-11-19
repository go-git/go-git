// Package sshagent connects go-git's SSH transport to the running SSH agent.
// On Unix-like systems the agent is reached through the socket named by the
// SSH_AUTH_SOCK environment variable. On Windows it is reached through the
// Windows OpenSSH agent named pipe.
//
// It is a trimmed copy of github.com/xanzy/ssh-agent v0.3.3 (commit
// 0fa644ba07f41bbe341c7f3d59bd30443ff13002) without Pageant support. The
// github.com/go-git/x/ssh/agent module supports Pageant and exposes the full
// agent API.
package sshagent

import "errors"

// ErrNotFound is returned by New when no SSH agent can be reached.
var ErrNotFound = errors.New("no SSH agent found, use github.com/go-git/x/ssh/agent for Pageant or other agents")
