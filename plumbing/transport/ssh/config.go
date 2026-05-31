package ssh

import (
	"context"
	"errors"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
	xknownhosts "golang.org/x/crypto/ssh/knownhosts"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/trace"
)

// HostKeyCheck controls how server host keys are verified, mirroring OpenSSH's
// StrictHostKeyChecking option.
type HostKeyCheck int

const (
	// HostKeyCheckKnownHosts verifies the server key against known_hosts and
	// rejects unknown or changed keys (StrictHostKeyChecking=yes). This is the
	// zero value and the default.
	HostKeyCheckKnownHosts HostKeyCheck = iota

	// HostKeyCheckAcceptNew trusts host keys for hosts not yet in known_hosts
	// but still rejects changed keys for known hosts (accept-new). New keys are
	// trusted only for the life of the process; they are not written back.
	HostKeyCheckAcceptNew

	// HostKeyCheckInsecureIgnore disables host key verification entirely
	// (StrictHostKeyChecking=no).
	HostKeyCheckInsecureIgnore
)

// Identity is the authentication identity for an SSH connection: the login user
// and the auth methods to offer. Build it with [KeyAuth], [KeyAuthBytes] or
// [AgentAuth], or assemble it directly. An empty User is resolved from the
// request URL, then [DefaultUsername].
type Identity struct {
	User    string
	Methods []gossh.AuthMethod
}

// HostConfig is the host verification and connection policy for an SSH
// connection. Zero-valued fields use the default.
type HostConfig struct {
	// StrictHostKeyChecking selects the host key verification policy. It is
	// ignored when HostKeyCallback is set.
	StrictHostKeyChecking HostKeyCheck

	// KnownHostsFiles overrides the known_hosts sources (ssh -o
	// UserKnownHostsFile). Empty uses the defaults (see [NewKnownHostsCallback]).
	KnownHostsFiles []string

	// HostKeyCallback, when set, verifies the server key directly and takes
	// precedence over StrictHostKeyChecking and KnownHostsFiles.
	HostKeyCallback gossh.HostKeyCallback

	// ConnectTimeout bounds the SSH handshake (ssh -o ConnectTimeout).
	ConnectTimeout time.Duration
}

// KeyAuth builds an Identity from a PEM-encoded private key file (ssh -i),
// decrypting it with passphrase when needed.
func KeyAuth(user, pemFile, passphrase string) (*Identity, error) {
	pk, err := NewPublicKeysFromFile(user, pemFile, passphrase)
	if err != nil {
		return nil, err
	}
	return &Identity{User: user, Methods: []gossh.AuthMethod{gossh.PublicKeys(pk.Signer)}}, nil
}

// KeyAuthBytes builds an Identity from PEM-encoded private key bytes.
func KeyAuthBytes(user string, pem []byte, passphrase string) (*Identity, error) {
	pk, err := NewPublicKeys(user, pem, passphrase)
	if err != nil {
		return nil, err
	}
	return &Identity{User: user, Methods: []gossh.AuthMethod{gossh.PublicKeys(pk.Signer)}}, nil
}

// AgentAuth builds an Identity backed by the running SSH agent.
func AgentAuth(user string) (*Identity, error) {
	pk, err := NewSSHAgentAuth(user)
	if err != nil {
		return nil, err
	}
	return &Identity{User: pk.User, Methods: []gossh.AuthMethod{gossh.PublicKeysCallback(pk.Callback)}}, nil
}

// ClientConfig assembles a *gossh.ClientConfig from the host policy and an
// identity, resolving the login user and the host key callback.
func (h *HostConfig) ClientConfig(_ context.Context, req *transport.Request, id *Identity) (*gossh.ClientConfig, error) {
	cb := h.HostKeyCallback
	if cb == nil {
		var err error
		if cb, err = knownHostsCallback(h.StrictHostKeyChecking, h.KnownHostsFiles...); err != nil {
			return nil, err
		}
	}

	cfg := &gossh.ClientConfig{
		User:            resolveUser(id, req),
		HostKeyCallback: cb,
		Timeout:         h.ConnectTimeout,
	}
	if id != nil {
		cfg.Auth = id.Methods
	}
	return cfg, nil
}

// knownHostsCallback builds a HostKeyCallback for the given policy. For
// HostKeyCheckKnownHosts and HostKeyCheckAcceptNew the keys are read from files
// (or the default sources when files is empty); HostKeyCheckInsecureIgnore
// disables verification.
func knownHostsCallback(check HostKeyCheck, files ...string) (gossh.HostKeyCallback, error) {
	switch check {
	case HostKeyCheckInsecureIgnore:
		return gossh.InsecureIgnoreHostKey(), nil //nolint:gosec // explicit opt-in
	case HostKeyCheckAcceptNew:
		cb, err := NewKnownHostsCallback(files...)
		if err != nil {
			return nil, err
		}
		return acceptNewHostKeyCallback(cb), nil
	default: // HostKeyCheckKnownHosts
		return NewKnownHostsCallback(files...)
	}
}

func resolveUser(id *Identity, req *transport.Request) string {
	if id != nil && id.User != "" {
		return id.User
	}
	if req != nil && req.URL != nil && req.URL.User != nil {
		if u := req.URL.User.Username(); u != "" {
			return u
		}
	}
	return DefaultUsername
}

// acceptNewHostKeyCallback wraps a known_hosts callback so hosts not yet present
// are accepted (and trusted for this process), while changed keys for known
// hosts are still rejected.
func acceptNewHostKeyCallback(inner gossh.HostKeyCallback) gossh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		err := inner(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *xknownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			trace.SSH.Printf("ssh: accept-new trusting unknown host %s key %s",
				hostname, gossh.FingerprintSHA256(key))
			return nil
		}
		return err
	}
}
