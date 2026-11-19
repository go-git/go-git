//
// Copyright (c) 2014 David Mzareulyan
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software
// and associated documentation files (the "Software"), to deal in the Software without restriction,
// including without limitation the rights to use, copy, modify, merge, publish, distribute,
// sublicense, and/or sell copies of the Software, and to permit persons to whom the Software
// is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all copies or substantial
// portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING
// BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM,
// DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//

// Originally from: https://github.com/xanzy/ssh-agent/blob/v0.3.3/sshagent_windows.go
// Upstream commit: 0fa644ba07f41bbe341c7f3d59bd30443ff13002
// MIT LICENSE: https://github.com/davidmz/go-pageant/blob/master/LICENSE.txt
// Pageant support was removed. The full API, including Pageant, is in
// github.com/go-git/x/ssh/agent.

//go:build windows

package sshagent

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh/agent"

	"github.com/go-git/go-git/v6/utils/trace"
)

const (
	sshAgentPipe = `\\.\pipe\openssh-ssh-agent`
)

// New connects to the Windows OpenSSH agent named pipe. It returns the agent
// client and its connection, which the caller must close. The returned error
// wraps ErrNotFound when the pipe cannot be opened. Pageant is not supported.
func New() (agent.Agent, net.Conn, error) {
	return dialPipe(sshAgentPipe)
}

func dialPipe(name string) (agent.Agent, net.Conn, error) {
	trace.SSH.Printf("ssh: winio.DialPipe %s", name)
	conn, err := winio.DialPipe(name, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("error connecting to the Windows OpenSSH agent at %s: %w: %w", name, err, ErrNotFound)
	}
	return agent.NewClient(conn), conn, nil
}
