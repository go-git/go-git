package ssh

import (
	"context"
	"testing"

	"github.com/gliderlabs/ssh"
	"github.com/kevinburke/ssh_config"
	"github.com/stretchr/testify/require"
	stdssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

func (s *SuiteCommon) TestOverrideConfig() {
	config := &stdssh.ClientConfig{
		User: "foo",
		Auth: []stdssh.AuthMethod{
			stdssh.Password("yourpassword"),
		},
		HostKeyCallback: stdssh.FixedHostKey(nil),
	}

	target := &stdssh.ClientConfig{}
	overrideConfig(config, target)

	s.Equal("foo", target.User)
	s.Len(target.Auth, 1)
	s.NotNil(target.HostKeyCallback)
}

func (s *SuiteCommon) TestOverrideConfigKeep() {
	config := &stdssh.ClientConfig{
		User: "foo",
	}

	target := &stdssh.ClientConfig{
		User: "bar",
	}

	overrideConfig(config, target)
	s.Equal("foo", target.User)
}

func (s *SuiteCommon) TestDefaultSSHConfig() {
	s.T().Cleanup(func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	})

	DefaultSSHConfig = &mockSSHConfig{map[string]map[string]string{
		"github.com": {
			"Hostname": "foo.local",
			"Port":     "42",
		},
	}}

	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	s.NoError(err)

	cmd := &command{endpoint: ep}
	s.Equal("foo.local:42", cmd.getHostWithPort())
}

func (s *SuiteCommon) TestDefaultSSHConfigNil() {
	s.T().Cleanup(func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	})

	DefaultSSHConfig = nil

	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	s.NoError(err)

	cmd := &command{endpoint: ep}
	s.Equal("github.com:22", cmd.getHostWithPort())
}

func (s *SuiteCommon) TestDefaultSSHConfigWildcard() {
	s.T().Cleanup(func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	})

	DefaultSSHConfig = &mockSSHConfig{Values: map[string]map[string]string{
		"*": {
			"Port": "42",
		},
	}}

	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	s.NoError(err)

	cmd := &command{endpoint: ep}
	s.Equal("github.com:22", cmd.getHostWithPort())
}

func TestIgnoreHostKeyCallback(t *testing.T) {
	t.Parallel()
	opts := []ssh.Option{
		ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
	}
	base, port, _ := setupTest(t, opts...)
	// Use the default client, which does not have a host key callback
	client := DefaultTransport
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	require.NoError(t, err)
	require.NotNil(t, auth)
	auth.HostKeyCallback = stdssh.InsecureIgnoreHostKey()
	ep := newEndpoint(t, base, port, "bar.git")
	st := memory.NewStorage()
	ps, err := client.NewSession(st, ep, auth)
	require.NoError(t, err)
	require.NotNil(t, ps)
}

func TestFixedHostKeyCallback(t *testing.T) {
	t.Parallel()
	opts := []ssh.Option{
		ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
	}
	base, port, _ := setupTest(t, opts...)
	// Use the default client, which does not have a host key callback
	client := DefaultTransport
	hostKey, err := stdssh.ParsePrivateKey(testdata.PEMBytes["ed25519"])
	require.NoError(t, err)
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	require.NoError(t, err)
	require.NotNil(t, auth)
	auth.HostKeyCallback = stdssh.FixedHostKey(hostKey.PublicKey())
	ep := newEndpoint(t, base, port, "bar.git")
	st := memory.NewStorage()
	ps, err := client.NewSession(st, ep, auth)
	require.NoError(t, err)
	require.NotNil(t, ps)
}

func TestFailHostKeyCallback(t *testing.T) {
	t.Parallel()
	opts := []ssh.Option{
		ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
	}
	base, port, _ := setupTest(t, opts...)
	// Use the default client, which does not have a host key callback
	client := DefaultTransport
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	require.NoError(t, err)
	require.NotNil(t, auth)
	ep := newEndpoint(t, base, port, "bar.git")
	st := memory.NewStorage()
	sess, err := client.NewSession(st, ep, auth)
	require.NoError(t, err)
	_, err = sess.Handshake(context.TODO(), transport.UploadPackService)
	require.Error(t, err)
}

func TestIssue70Suite(t *testing.T) { //nolint: paralleltest // modifies global DefaultAuthBuilder
	authBuilder := DefaultAuthBuilder
	t.Cleanup(func() { DefaultAuthBuilder = authBuilder })
	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}
	config := &stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	}
	r := &runner{
		config: config,
	}
	base, port, _ := setupTest(t)
	var emptyAuth AuthMethod
	cmd, err := r.Command(context.TODO(), "command", newEndpoint(t, base, port, "endpoint"), emptyAuth)
	require.NoError(t, err)
	require.NoError(t, cmd.(*command).client.Close())
	require.NoError(t, cmd.Close())
}

func (s *SuiteCommon) TestInvalidSocks5Proxy() {
	st := memory.NewStorage()
	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	s.Require().NoError(err)
	ep.Proxy.URL = "socks5://127.0.0.1:1080"

	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.Require().NoError(err)
	s.Require().NotNil(auth)

	ps, err := DefaultTransport.NewSession(st, ep, auth)
	s.Require().NoError(err)
	s.Require().NotNil(ps)
	conn, err := ps.Handshake(context.TODO(), transport.UploadPackService)
	// Since the proxy server is not running, we expect an error.
	s.Require().Nil(conn)
	s.Require().Error(err)
	s.Require().Regexp("socks connect .* dial tcp 127.0.0.1:1080: .*", err.Error())
}

type mockSSHConfig struct {
	Values map[string]map[string]string
}

func (c *mockSSHConfig) Get(alias, key string) string {
	a, ok := c.Values[alias]
	if !ok {
		return c.Values["*"][key]
	}

	return a[key]
}

type invalidAuthMethod struct{}

func (a *invalidAuthMethod) Name() string {
	return "invalid"
}

func (a *invalidAuthMethod) String() string {
	return "invalid"
}

func (s *UploadPackSuite) TestCommandWithInvalidAuthMethod() {
	r := &runner{}
	auth := &invalidAuthMethod{}

	_, err := r.Command(context.TODO(), "command", newEndpoint(s.T(), s.base, s.port, "endpoint"), auth)

	s.Error(err)
	s.Equal("invalid auth method", err.Error())
}
