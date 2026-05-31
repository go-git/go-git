package client_test

import (
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

// WithSSHConfig wires a typed Identity and HostConfig into a client.
func ExampleWithSSHConfig() {
	host := &ssh.HostConfig{StrictHostKeyChecking: ssh.HostKeyCheckAcceptNew}
	id := &ssh.Identity{User: "git"} // add auth with ssh.KeyAuth or ssh.AgentAuth

	_ = client.New(client.WithSSHConfig(id, host))
	// Output:
}
