package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var ErrEmptySSHAgentAddr = errors.New("SSH_AUTH_SOCK env variable is required")

// AuthMethod is the interface all auth methods for the ssh client
// must implement. The clientConfig method returns the ssh client
// configuration needed to establish an ssh connection.
type AuthMethod interface {
	clientConfig() *ssh.ClientConfig
}

// The names of the AuthMethod implementations. To be returned by the
// Name() method. Most git servers only allow PublicKeysName and
// PublicKeysCallbackName.
const (
	KeyboardInteractiveName = "ssh-keyboard-interactive"
	PasswordName            = "ssh-password"
	PasswordCallbackName    = "ssh-password-callback"
	PublicKeysName          = "ssh-public-keys"
	PublicKeysCallbackName  = "ssh-public-key-callback"
)

// KeyboardInteractive implements AuthMethod by using a
// prompt/response sequence controlled by the server.
type KeyboardInteractive struct {
	User      string
	Challenge ssh.KeyboardInteractiveChallenge
}

func (a *KeyboardInteractive) Name() string {
	return KeyboardInteractiveName
}

func (a *KeyboardInteractive) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *KeyboardInteractive) clientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.KeyboardInteractiveChallenge(a.Challenge)},
	}
}

// Password implements AuthMethod by using the given password.
type Password struct {
	User string
	Pass string
}

func (a *Password) Name() string {
	return PasswordName
}

func (a *Password) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *Password) clientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.Password(a.Pass)},
	}
}

// PasswordCallback implements AuthMethod by using a callback
// to fetch the password.
type PasswordCallback struct {
	User     string
	Callback func() (pass string, err error)
}

func (a *PasswordCallback) Name() string {
	return PasswordCallbackName
}

func (a *PasswordCallback) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PasswordCallback) clientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.PasswordCallback(a.Callback)},
	}
}

// PublicKeys implements AuthMethod by using the given
// key pairs.
type PublicKeys struct {
	User   string
	Signer ssh.Signer
}

func (a *PublicKeys) Name() string {
	return PublicKeysName
}

func (a *PublicKeys) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PublicKeys) clientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(a.Signer)},
	}
}

// PublicKeysCallback implements AuthMethod by asking a
// ssh.agent.Agent to act as a signer.
type PublicKeysCallback struct {
	User     string
	Callback func() (signers []ssh.Signer, err error)
}

func (a *PublicKeysCallback) Name() string {
	return PublicKeysCallbackName
}

func (a *PublicKeysCallback) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PublicKeysCallback) clientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeysCallback(a.Callback)},
	}
}

const DefaultSSHUsername = "git"

// NewSSHAgentAuth opens a pipe with the SSH agent and uses the pipe
// as the implementer of the public key callback function.
func NewSSHAgentAuth(user string) (*PublicKeysCallback, error) {
	if user == "" {
		user = DefaultSSHUsername
	}

	sshAgentAddr := os.Getenv("SSH_AUTH_SOCK")
	if sshAgentAddr == "" {
		return nil, ErrEmptySSHAgentAddr
	}

	pipe, err := net.Dial("unix", sshAgentAddr)
	if err != nil {
		return nil, fmt.Errorf("error connecting to SSH agent: %q", err)
	}

	return &PublicKeysCallback{
		User:     user,
		Callback: agent.NewClient(pipe).Signers,
	}, nil
}
