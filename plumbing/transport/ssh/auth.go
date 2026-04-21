package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"

	gossh "golang.org/x/crypto/ssh"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh/knownhosts"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh/sshagent"
	"github.com/go-git/go-git/v6/utils/trace"
)

// The names of the auth method implementations.
const (
	keyboardInteractiveName = "ssh-keyboard-interactive"
	passwordName            = "ssh-password"
	passwordCallbackName    = "ssh-password-callback"
	publicKeysName          = "ssh-public-keys"
	publicKeysCallbackName  = "ssh-public-key-callback"
)

// HostKeyCallbackHelper provides common functionality to configure
// HostKeyCallback on a *ssh.ClientConfig.
type HostKeyCallbackHelper struct {
	// HostKeyCallback is the function used for verifying server keys.
	// If nil, a default callback is created using NewKnownHostsCallback.
	HostKeyCallback gossh.HostKeyCallback
}

// SetHostKeyCallback sets HostKeyCallback on cfg. If HostKeyCallback is nil,
// a default callback is created from known_hosts files.
func (m *HostKeyCallbackHelper) SetHostKeyCallback(cfg *gossh.ClientConfig) (*gossh.ClientConfig, error) {
	if m.HostKeyCallback == nil {
		db, err := newKnownHostsDb()
		if err != nil {
			return cfg, err
		}
		m.HostKeyCallback = db.HostKeyCallback()
	}

	cfg.HostKeyCallback = m.traceHostKeyCallback
	return cfg, nil
}

func (m *HostKeyCallbackHelper) traceHostKeyCallback(hostname string, remote net.Addr, key gossh.PublicKey) error {
	trace.SSH.Printf(
		`ssh: hostkey callback hostname=%s remote=%s pubkey="%s %s"`,
		hostname, remote, key.Type(), gossh.FingerprintSHA256(key))
	return m.HostKeyCallback(hostname, remote, key)
}

// Password implements SSH password authentication.
type Password struct {
	User     string
	Password string
	HostKeyCallbackHelper
}

// ClientConfig returns the ssh.ClientConfig for password authentication.
func (a *Password) ClientConfig(_ context.Context, _ *transport.Request) (*gossh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", passwordName, a.User)
	return a.SetHostKeyCallback(&gossh.ClientConfig{
		User: a.User,
		Auth: []gossh.AuthMethod{gossh.Password(a.Password)},
	})
}

// PasswordCallback implements SSH password authentication using a callback
// to fetch the password.
type PasswordCallback struct {
	User     string
	Callback func() (pass string, err error)
	HostKeyCallbackHelper
}

// ClientConfig returns the ssh.ClientConfig for password callback authentication.
func (a *PasswordCallback) ClientConfig(_ context.Context, _ *transport.Request) (*gossh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", passwordCallbackName, a.User)
	return a.SetHostKeyCallback(&gossh.ClientConfig{
		User: a.User,
		Auth: []gossh.AuthMethod{gossh.PasswordCallback(a.Callback)},
	})
}

// PublicKeys implements SSH public key authentication using the given key pair.
type PublicKeys struct {
	User   string
	Signer gossh.Signer
	HostKeyCallbackHelper
}

// NewPublicKeys returns a PublicKeys from a PEM encoded private key. An
// encryption password should be given if the pemBytes contains a password
// encrypted PEM block otherwise password should be empty. It supports RSA
// (PKCS#1), PKCS#8, DSA (OpenSSL), ECDSA, and Ed25519 private keys.
func NewPublicKeys(user string, pemBytes []byte, password string) (*PublicKeys, error) {
	signer, err := gossh.ParsePrivateKey(pemBytes)
	if _, ok := err.(*gossh.PassphraseMissingError); ok {
		signer, err = gossh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(password))
	}
	if err != nil {
		return nil, err
	}
	return &PublicKeys{User: user, Signer: signer}, nil
}

// NewPublicKeysFromFile returns a PublicKeys from a file containing a PEM
// encoded private key. An encryption password should be given if the file
// contains a password encrypted PEM block otherwise password should be empty.
func NewPublicKeysFromFile(user, pemFile, password string) (*PublicKeys, error) {
	pemData, err := os.ReadFile(pemFile)
	if err != nil {
		return nil, err
	}
	return NewPublicKeys(user, pemData, password)
}

// ClientConfig returns the ssh.ClientConfig for public key authentication.
func (a *PublicKeys) ClientConfig(_ context.Context, _ *transport.Request) (*gossh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s signer=\"%s %s\"", publicKeysName, a.User,
		a.Signer.PublicKey().Type(),
		gossh.FingerprintSHA256(a.Signer.PublicKey()))
	return a.SetHostKeyCallback(&gossh.ClientConfig{
		User: a.User,
		Auth: []gossh.AuthMethod{gossh.PublicKeys(a.Signer)},
	})
}

// PublicKeysCallback implements SSH public key authentication using a callback
// to fetch signers, typically from an SSH agent.
type PublicKeysCallback struct {
	User     string
	Callback func() (signers []gossh.Signer, err error)
	HostKeyCallbackHelper
}

// NewSSHAgentAuth returns a PublicKeysCallback based on an SSH agent. It opens
// a pipe with the SSH agent and uses the pipe as the implementer of the public
// key callback function.
func NewSSHAgentAuth(u string) (*PublicKeysCallback, error) {
	var err error
	if u == "" {
		u, err = username()
		if err != nil {
			return nil, err
		}
	}

	a, _, err := sshagent.New()
	if err != nil {
		return nil, fmt.Errorf("error creating SSH agent: %w", err)
	}

	return &PublicKeysCallback{
		User:     u,
		Callback: a.Signers,
	}, nil
}

// ClientConfig returns the ssh.ClientConfig for public key callback authentication.
func (a *PublicKeysCallback) ClientConfig(_ context.Context, _ *transport.Request) (*gossh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", publicKeysCallbackName, a.User)
	return a.SetHostKeyCallback(&gossh.ClientConfig{
		User: a.User,
		Auth: []gossh.AuthMethod{tracePublicKeysCallback(a.Callback)},
	})
}

// KeyboardInteractive implements SSH keyboard-interactive authentication
// using a prompt/response sequence controlled by the server.
type KeyboardInteractive struct {
	User      string
	Challenge gossh.KeyboardInteractiveChallenge
	HostKeyCallbackHelper
}

// ClientConfig returns the ssh.ClientConfig for keyboard-interactive authentication.
func (a *KeyboardInteractive) ClientConfig(_ context.Context, _ *transport.Request) (*gossh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", keyboardInteractiveName, a.User)
	return a.SetHostKeyCallback(&gossh.ClientConfig{
		User: a.User,
		Auth: []gossh.AuthMethod{a.Challenge},
	})
}

// NewKnownHostsCallback returns ssh.HostKeyCallback based on a known_hosts
// file. http://man.openbsd.org/sshd#SSH_KNOWN_HOSTS_FILE_FORMAT
//
// If list of files is empty, then it will be read from the SSH_KNOWN_HOSTS
// environment variable, example:
//
//	/home/foo/custom_known_hosts_file:/etc/custom_known/hosts_file
//
// If SSH_KNOWN_HOSTS is not set the following file locations will be used:
//
//	~/.ssh/known_hosts
//	/etc/ssh/ssh_known_hosts
func NewKnownHostsCallback(files ...string) (gossh.HostKeyCallback, error) {
	db, err := newKnownHostsDb(files...)
	if db == nil {
		return nil, err
	}
	return db.HostKeyCallback(), err
}

func newKnownHostsDb(files ...string) (*knownhosts.HostKeyDB, error) {
	if len(files) == 0 {
		var err error
		if files, err = getDefaultKnownHostsFiles(); err != nil {
			return nil, err
		}
	}

	trace.SSH.Printf("ssh: known_hosts sources %v", files)

	files, err := filterKnownHostsFiles(files...)
	if err != nil {
		return nil, err
	}
	trace.SSH.Printf("ssh: filtered known_hosts sources %v", files)

	return knownhosts.NewDB(files...)
}

func getDefaultKnownHostsFiles() ([]string, error) {
	files := filepath.SplitList(os.Getenv("SSH_KNOWN_HOSTS"))
	if len(files) != 0 {
		trace.SSH.Printf("ssh: loading known_hosts from SSH_KNOWN_HOSTS")
		return files, nil
	}

	homeDirPath, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	return []string{
		filepath.Join(homeDirPath, ".ssh", "known_hosts"),
		"/etc/ssh/ssh_known_hosts",
	}, nil
}

func tracePublicKeysCallback(getSigners func() ([]gossh.Signer, error)) gossh.AuthMethod {
	signers, err := getSigners()
	if err != nil {
		trace.SSH.Printf("ssh: error calling getSigners: %v", err)
	}
	if len(signers) == 0 {
		trace.SSH.Printf("ssh: no signers found")
	}
	for _, s := range signers {
		trace.SSH.Printf("ssh: found key: %s %s", s.PublicKey().Type(),
			gossh.FingerprintSHA256(s.PublicKey()))
	}

	cb := func() ([]gossh.Signer, error) {
		return signers, err
	}
	return gossh.PublicKeysCallback(cb)
}

func filterKnownHostsFiles(files ...string) ([]string, error) {
	var out []string
	for _, file := range files {
		_, err := os.Stat(file)
		if err == nil {
			out = append(out, file)
			continue
		}

		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("unable to find any valid known_hosts file, set SSH_KNOWN_HOSTS env variable")
	}

	return out, nil
}

func username() (string, error) {
	var u string
	if current, err := user.Current(); err == nil {
		u = current.Username
		trace.SSH.Printf("ssh: Falling back to current user name %q", u)
	} else {
		u = os.Getenv("USER")
		trace.SSH.Printf("ssh: Falling back to environment variable USER %q", u)
	}

	if u == "" {
		return "", errors.New("failed to get username")
	}

	return u, nil
}
