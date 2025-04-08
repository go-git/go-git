package ssh

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	"github.com/gliderlabs/ssh"
	"github.com/kevinburke/ssh_config"
	stdssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
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
	defer func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	}()

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
	defer func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	}()

	DefaultSSHConfig = nil

	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	s.NoError(err)

	cmd := &command{endpoint: ep}
	s.Equal("github.com:22", cmd.getHostWithPort())
}

func (s *SuiteCommon) TestDefaultSSHConfigWildcard() {
	defer func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	}()

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

type IgnoreHostKeyCallbackSuite struct {
	UploadPackSuite
}

func TestIgnoreHostKeyCallback(t *testing.T) {
	suite.Run(t, new(IgnoreHostKeyCallbackSuite))
}

func (s *IgnoreHostKeyCallbackSuite) SetupTest() {
	s.opts = []ssh.Option{
		ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
	}
	// Use the default client, which does not have a host key callback
	s.Client = DefaultTransport
	s.UploadPackSuite.SetupTest()
}

func (s *IgnoreHostKeyCallbackSuite) TestIgnoreHostKeyCallback() {
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.Nil(err)
	s.NotNil(auth)
	auth.HostKeyCallback = stdssh.InsecureIgnoreHostKey()
	ep := newEndpoint(s.T(), s.base, s.port, "bar.git")
	st := memory.NewStorage()
	ps, err := s.Client.NewSession(st, ep, auth)
	s.Nil(err)
	s.NotNil(ps)
}

type FixedHostKeyCallbackSuite struct {
	UploadPackSuite
}

func TestFixedHostKeyCallback(t *testing.T) {
	suite.Run(t, new(FixedHostKeyCallbackSuite))
}

func (s *FixedHostKeyCallbackSuite) SetupTest() {
	s.opts = []ssh.Option{
		ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
	}
	// Use the default client, which does not have a host key callback
	s.Client = DefaultTransport
	s.UploadPackSuite.SetupTest()
}

func (s *FixedHostKeyCallbackSuite) TestFixedHostKeyCallback() {
	hostKey, err := stdssh.ParsePrivateKey(testdata.PEMBytes["ed25519"])
	s.Nil(err)
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.Nil(err)
	s.NotNil(auth)
	auth.HostKeyCallback = stdssh.FixedHostKey(hostKey.PublicKey())
	ep := newEndpoint(s.T(), s.base, s.port, "bar.git")
	st := memory.NewStorage()
	ps, err := s.Client.NewSession(st, ep, auth)
	s.Nil(err)
	s.NotNil(ps)
}

type FailHostKeyCallbackSuite struct {
	UploadPackSuite
}

func TestFailHostKeyCallback(t *testing.T) {
	suite.Run(t, new(FailHostKeyCallbackSuite))
}

func (s *FailHostKeyCallbackSuite) SetupTest() {
	s.opts = []ssh.Option{
		ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
	}
	// Use the default client, which does not have a host key callback
	s.Client = DefaultTransport
	s.UploadPackSuite.SetupTest()
}

func (s *FailHostKeyCallbackSuite) TestFailHostKeyCallback() {
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.Nil(err)
	s.NotNil(auth)
	ep := newEndpoint(s.T(), s.base, s.port, "bar.git")
	st := memory.NewStorage()
	sess, err := s.Client.NewSession(st, ep, auth)
	s.NoError(err)
	_, err = sess.Handshake(context.TODO(), transport.UploadPackService)
	s.NotNil(err)
}

type Issue70Suite struct {
	UploadPackSuite
}

func TestIssue70Suite(t *testing.T) {
	suite.Run(t, new(Issue70Suite))
}

func (s *Issue70Suite) SetupTest() {
	s.UploadPackSuite.SetupTest()
}

func (s *Issue70Suite) TestIssue70() {
	config := &stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	}
	r := &runner{
		config: config,
	}

	cmd, err := r.Command(context.TODO(), "command", newEndpoint(s.T(), s.base, s.port, "endpoint"), s.EmptyAuth)
	s.Require().NoError(err)

	s.Require().NoError(cmd.(*command).client.Close())

	err = cmd.Close()
	s.Require().NoError(err)
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
