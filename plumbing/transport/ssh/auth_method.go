package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh/knownhosts"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh/sshagent"
	"github.com/go-git/go-git/v6/utils/trace"

	"golang.org/x/crypto/ssh"
)

const DefaultUsername = "git"

// AuthMethod is the interface all auth methods for the ssh client
// must implement. The clientConfig method returns the ssh client
// configuration needed to establish an ssh connection.
type AuthMethod interface {
	transport.AuthMethod
	// ClientConfig should return a valid ssh.ClientConfig to be used to create
	// a connection to the SSH server.
	ClientConfig() (*ssh.ClientConfig, error)
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
	HostKeyCallbackHelper
}

func (a *KeyboardInteractive) Name() string {
	return KeyboardInteractiveName
}

func (a *KeyboardInteractive) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *KeyboardInteractive) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", KeyboardInteractiveName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{
			a.Challenge,
		},
	})
}

// Password implements AuthMethod by using the given password.
type Password struct {
	User     string
	Password string
	HostKeyCallbackHelper
}

func (a *Password) Name() string {
	return PasswordName
}

func (a *Password) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *Password) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", PasswordName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.Password(a.Password)},
	})
}

// PasswordCallback implements AuthMethod by using a callback
// to fetch the password.
type PasswordCallback struct {
	User     string
	Callback func() (pass string, err error)
	HostKeyCallbackHelper
}

func (a *PasswordCallback) Name() string {
	return PasswordCallbackName
}

func (a *PasswordCallback) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PasswordCallback) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", PasswordCallbackName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.PasswordCallback(a.Callback)},
	})
}

// PublicKeys implements AuthMethod by using the given key pairs.
type PublicKeys struct {
	User   string
	Signer ssh.Signer
	HostKeyCallbackHelper
}

// NewPublicKeys returns a PublicKeys from a PEM encoded private key. An
// encryption password should be given if the pemBytes contains a password
// encrypted PEM block otherwise password should be empty. It supports RSA
// (PKCS#1), PKCS#8, DSA (OpenSSL), and ECDSA private keys.
func NewPublicKeys(user string, pemBytes []byte, password string) (*PublicKeys, error) {
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if _, ok := err.(*ssh.PassphraseMissingError); ok {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(password))
	}
	if err != nil {
		return nil, err
	}
	return &PublicKeys{User: user, Signer: signer}, nil
}

// NewPublicKeysFromFile returns a PublicKeys from a file containing a PEM
// encoded private key. An encryption password should be given if the pemBytes
// contains a password encrypted PEM block otherwise password should be empty.
func NewPublicKeysFromFile(user, pemFile, password string) (*PublicKeys, error) {
	bytes, err := os.ReadFile(pemFile)
	if err != nil {
		return nil, err
	}

	return NewPublicKeys(user, bytes, password)
}

func (a *PublicKeys) Name() string {
	return PublicKeysName
}

func (a *PublicKeys) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PublicKeys) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s signer=\"%s %s\"", PublicKeysName, a.User,
		a.Signer.PublicKey().Type(),
		ssh.FingerprintSHA256(a.Signer.PublicKey()))
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(a.Signer)},
	})
}

func username() (string, error) {
	var username string
	if user, err := user.Current(); err == nil {
		username = user.Username
		trace.SSH.Printf("ssh: Falling back to current user name %q", username)
	} else {
		username = os.Getenv("USER")
		trace.SSH.Printf("ssh: Falling back to environment variable USER %q", username)
	}

	if username == "" {
		return "", errors.New("failed to get username")
	}

	return username, nil
}

// PublicKeysCallback implements AuthMethod by asking a
// ssh.agent.Agent to act as a signer.
type PublicKeysCallback struct {
	User     string
	Callback func() (signers []ssh.Signer, err error)
	HostKeyCallbackHelper
}

// NewSSHAgentAuth returns a PublicKeysCallback based on a SSH agent, it opens
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
		return nil, fmt.Errorf("error creating SSH agent: %q", err)
	}

	return &PublicKeysCallback{
		User:     u,
		Callback: a.Signers,
	}, nil
}

func (a *PublicKeysCallback) Name() string {
	return PublicKeysCallbackName
}

func (a *PublicKeysCallback) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PublicKeysCallback) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", PublicKeysCallbackName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{tracePublicKeysCallback(a.Callback)},
	})
}

// NewKnownHostsCallback returns ssh.HostKeyCallback based on a file based on a
// known_hosts file. http://man.openbsd.org/sshd#SSH_KNOWN_HOSTS_FILE_FORMAT
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
func NewKnownHostsCallback(files ...string) (ssh.HostKeyCallback, error) {
	db, err := newKnownHostsDb(files...)
	return db.HostKeyCallback(), err
}

func newKnownHostsDb(files ...string) (*knownhosts.HostKeyDB, error) {
	var err error
	if len(files) == 0 {
		if files, err = getDefaultKnownHostsFiles(); err != nil {
			return nil, err
		}
	}
	trace.SSH.Printf("ssh: known_hosts sources %s", files)

	if files, err = filterKnownHostsFiles(files...); err != nil {
		return nil, err
	}
	trace.SSH.Printf("ssh: filtered known_hosts sources %s", files)

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
		filepath.Join(homeDirPath, "/.ssh/known_hosts"),
		"/etc/ssh/ssh_known_hosts",
	}, nil
}

func filterKnownHostsFiles(files ...string) ([]string, error) {
	var out []string
	for _, file := range files {
		_, err := os.Stat(file)
		if err == nil {
			out = append(out, file)
			continue
		}

		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("unable to find any valid known_hosts file, set SSH_KNOWN_HOSTS env variable")
	}

	return out, nil
}

// HostKeyCallbackHelper is a helper that provides common functionality to
// configure HostKeyCallback into a ssh.ClientConfig.
type HostKeyCallbackHelper struct {
	// HostKeyCallback is the function type used for verifying server keys.
	// If nil default callback will be create using NewKnownHostsCallback
	// without argument.
	HostKeyCallback ssh.HostKeyCallback
}

// SetHostKeyCallback sets the field HostKeyCallback in the given cfg. If
// HostKeyCallback is empty a default callback is created using
// NewKnownHostsCallback.
func (m *HostKeyCallbackHelper) SetHostKeyCallback(cfg *ssh.ClientConfig) (*ssh.ClientConfig, error) {
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

func (m *HostKeyCallbackHelper) traceHostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	trace.SSH.Printf(
		`ssh: hostkey callback hostname=%s remote=%s pubkey="%s %s"`,
		hostname, remote, key.Type(), ssh.FingerprintSHA256(key))
	return m.HostKeyCallback(hostname, remote, key)
}

func tracePublicKeysCallback(getSigners func() ([]ssh.Signer, error)) ssh.AuthMethod {
	signers, err := getSigners()
	if err != nil {
		trace.SSH.Printf("ssh: error calling getSigners: %v", err)
	}
	if len(signers) == 0 {
		trace.SSH.Printf("ssh: no signers found")
	}
	for _, s := range signers {
		trace.SSH.Printf("ssh: found key: %s %s", s.PublicKey().Type(),
			ssh.FingerprintSHA256(s.PublicKey()))
	}

	cb := func() ([]ssh.Signer, error) {
		return signers, err
	}
	return ssh.PublicKeysCallback(cb)
}
