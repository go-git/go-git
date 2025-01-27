package ssh

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"

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

func (s *SuiteCommon) TestIgnoreHostKeyCallback() {
	uploadPack := &UploadPackSuite{
		opts: []ssh.Option{
			ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
		},
	}
	uploadPack.Suite = s.Suite
	uploadPack.SetupSuite()
	// Use the default client, which does not have a host key callback
	uploadPack.Client = DefaultTransport
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.Nil(err)
	s.NotNil(auth)
	auth.HostKeyCallback = stdssh.InsecureIgnoreHostKey()
	ep := uploadPack.newEndpoint("bar.git")
	st := memory.NewStorage()
	ps, err := uploadPack.Client.NewSession(st, ep, auth)
	s.Nil(err)
	s.NotNil(ps)
}

func (s *SuiteCommon) TestFixedHostKeyCallback() {
	hostKey, err := stdssh.ParsePrivateKey(testdata.PEMBytes["ed25519"])
	s.Nil(err)
	uploadPack := &UploadPackSuite{
		opts: []ssh.Option{
			ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
		},
	}
	uploadPack.Suite = s.Suite
	uploadPack.SetupSuite()
	// Use the default client, which does not have a host key callback
	uploadPack.Client = DefaultTransport
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.Nil(err)
	s.NotNil(auth)
	auth.HostKeyCallback = stdssh.FixedHostKey(hostKey.PublicKey())
	ep := uploadPack.newEndpoint("bar.git")
	st := memory.NewStorage()
	ps, err := uploadPack.Client.NewSession(st, ep, auth)
	s.Nil(err)
	s.NotNil(ps)
}

func (s *SuiteCommon) TestFailHostKeyCallback() {
	uploadPack := &UploadPackSuite{
		opts: []ssh.Option{
			ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
		},
	}
	uploadPack.Suite = s.Suite
	uploadPack.SetupSuite()
	// Use the default client, which does not have a host key callback
	uploadPack.Client = DefaultTransport
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.Nil(err)
	s.NotNil(auth)
	ep := uploadPack.newEndpoint("bar.git")
	st := memory.NewStorage()
	sess, err := uploadPack.Client.NewSession(st, ep, auth)
	s.NoError(err)
	_, err = sess.Handshake(context.TODO(), transport.UploadPackService)
	s.NotNil(err)
}

func (s *SuiteCommon) TestIssue70() {
	uploadPack := &UploadPackSuite{}
	uploadPack.Suite = s.Suite
	uploadPack.SetupSuite()

	config := &stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	}
	r := &runner{
		config: config,
	}

	cmd, err := r.Command(context.TODO(), "command", uploadPack.newEndpoint("endpoint"), uploadPack.EmptyAuth)
	s.NoError(err)

	s.NoError(cmd.(*command).client.Close())

	err = cmd.Close()
	s.NoError(err)
}

func (s *SuiteCommon) TestInvalidSocks5Proxy() {
	st := memory.NewStorage()
	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	s.NoError(err)
	ep.Proxy.URL = "socks5://127.0.0.1:1080"

	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.NoError(err)
	s.NotNil(auth)

	ps, err := DefaultTransport.NewSession(st, ep, auth)
	s.NoError(err)
	s.NotNil(ps)
	conn, err := ps.Handshake(context.TODO(), transport.UploadPackService)
	// Since the proxy server is not running, we expect an error.
	s.Nil(conn)
	s.Error(err)
	s.Regexp("socks connect .* dial tcp 127.0.0.1:1080: .*", err.Error())
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

	_, err := r.Command(context.TODO(), "command", s.newEndpoint("endpoint"), auth)

	s.Error(err)
	s.Equal("invalid auth method", err.Error())
}
