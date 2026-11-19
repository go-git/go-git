//
// Copyright 2015, Sander van Harmelen
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Originally from: https://github.com/xanzy/ssh-agent/blob/v0.3.3/sshagent.go
// Upstream commit: 0fa644ba07f41bbe341c7f3d59bd30443ff13002
// The full API, including Pageant, is in github.com/go-git/x/ssh/agent.

//go:build !windows

package sshagent

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh/agent"

	"github.com/go-git/go-git/v6/utils/trace"
)

// New connects to the SSH agent listening on the socket named by
// SSH_AUTH_SOCK. It returns the agent client and its connection, which the
// caller must close. The returned error wraps ErrNotFound when SSH_AUTH_SOCK
// is not set or cannot be dialed.
func New() (agent.Agent, net.Conn, error) {
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock == "" {
		return nil, nil, fmt.Errorf("SSH_AUTH_SOCK is not set: %w", ErrNotFound)
	}

	trace.SSH.Printf("ssh: net.Dial unix sock %s", sshAuthSock)
	conn, err := net.Dial("unix", sshAuthSock)
	if err != nil {
		return nil, nil, fmt.Errorf("error connecting to SSH_AUTH_SOCK: %w: %w", err, ErrNotFound)
	}

	return agent.NewClient(conn), conn, nil
}
