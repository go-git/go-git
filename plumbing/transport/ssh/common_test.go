package ssh

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/gliderlabs/ssh"
	"github.com/kevinburke/ssh_config"
	stdssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

func (s *SuiteCommon) TestOverrideConfig(c *C) {
	config := &stdssh.ClientConfig{
		User: "foo",
		Auth: []stdssh.AuthMethod{
			stdssh.Password("yourpassword"),
		},
		HostKeyCallback: stdssh.FixedHostKey(nil),
	}

	target := &stdssh.ClientConfig{}
	overrideConfig(config, target)

	c.Assert(target.User, Equals, "foo")
	c.Assert(target.Auth, HasLen, 1)
	c.Assert(target.HostKeyCallback, NotNil)
}

func (s *SuiteCommon) TestOverrideConfigKeep(c *C) {
	config := &stdssh.ClientConfig{
		User: "foo",
	}

	target := &stdssh.ClientConfig{
		User: "bar",
	}

	overrideConfig(config, target)
	c.Assert(target.User, Equals, "foo")
}

func (s *SuiteCommon) TestDefaultSSHConfig(c *C) {
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
	c.Assert(err, IsNil)

	cmd := &command{endpoint: ep}
	c.Assert(cmd.getHostWithPort(), Equals, "foo.local:42")
}

func (s *SuiteCommon) TestDefaultSSHConfigNil(c *C) {
	defer func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	}()

	DefaultSSHConfig = nil

	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	c.Assert(err, IsNil)

	cmd := &command{endpoint: ep}
	c.Assert(cmd.getHostWithPort(), Equals, "github.com:22")
}

func (s *SuiteCommon) TestDefaultSSHConfigWildcard(c *C) {
	defer func() {
		DefaultSSHConfig = ssh_config.DefaultUserSettings
	}()

	DefaultSSHConfig = &mockSSHConfig{Values: map[string]map[string]string{
		"*": {
			"Port": "42",
		},
	}}

	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	c.Assert(err, IsNil)

	cmd := &command{endpoint: ep}
	c.Assert(cmd.getHostWithPort(), Equals, "github.com:22")
}

func (s *SuiteCommon) TestIgnoreHostKeyCallback(c *C) {
	uploadPack := &UploadPackSuite{
		opts: []ssh.Option{
			ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
		},
	}
	uploadPack.SetUpSuite(c)
	// Use the default client, which does not have a host key callback
	uploadPack.Client = DefaultClient
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	c.Assert(err, IsNil)
	c.Assert(auth, NotNil)
	auth.HostKeyCallback = stdssh.InsecureIgnoreHostKey()
	ep := uploadPack.newEndpoint(c, "bar.git")
	ps, err := uploadPack.Client.NewUploadPackSession(ep, auth)
	c.Assert(err, IsNil)
	c.Assert(ps, NotNil)
}

func (s *SuiteCommon) TestFixedHostKeyCallback(c *C) {
	hostKey, err := stdssh.ParsePrivateKey(testdata.PEMBytes["ed25519"])
	c.Assert(err, IsNil)
	uploadPack := &UploadPackSuite{
		opts: []ssh.Option{
			ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
		},
	}
	uploadPack.SetUpSuite(c)
	// Use the default client, which does not have a host key callback
	uploadPack.Client = DefaultClient
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	c.Assert(err, IsNil)
	c.Assert(auth, NotNil)
	auth.HostKeyCallback = stdssh.FixedHostKey(hostKey.PublicKey())
	ep := uploadPack.newEndpoint(c, "bar.git")
	ps, err := uploadPack.Client.NewUploadPackSession(ep, auth)
	c.Assert(err, IsNil)
	c.Assert(ps, NotNil)
}

func (s *SuiteCommon) TestFailHostKeyCallback(c *C) {
	uploadPack := &UploadPackSuite{
		opts: []ssh.Option{
			ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]),
		},
	}
	uploadPack.SetUpSuite(c)
	// Use the default client, which does not have a host key callback
	uploadPack.Client = DefaultClient
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	c.Assert(err, IsNil)
	c.Assert(auth, NotNil)
	ep := uploadPack.newEndpoint(c, "bar.git")
	_, err = uploadPack.Client.NewUploadPackSession(ep, auth)
	c.Assert(err, NotNil)
}

func (s *SuiteCommon) TestIssue70(c *C) {
	uploadPack := &UploadPackSuite{}
	uploadPack.SetUpSuite(c)

	config := &stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	}
	r := &runner{
		config: config,
	}

	cmd, err := r.Command("command", uploadPack.newEndpoint(c, "endpoint"), uploadPack.EmptyAuth)
	c.Assert(err, IsNil)

	c.Assert(cmd.(*command).client.Close(), IsNil)

	err = cmd.Close()
	c.Assert(err, IsNil)
}

/*
Given, an endpoint to a git server with a socks5 proxy URL,
When, the socks5 proxy server is not reachable,
Then, there should not be any panic and an error with appropriate message should be returned.
Related issue : https://github.com/go-git/go-git/pull/900
*/
func (s *SuiteCommon) TestInvalidSocks5Proxy(c *C) {
	ep, err := transport.NewEndpoint("git@github.com:foo/bar.git")
	c.Assert(err, IsNil)
	ep.Proxy.URL = "socks5://127.0.0.1:1080"

	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	c.Assert(err, IsNil)
	c.Assert(auth, NotNil)

	ps, err := DefaultClient.NewUploadPackSession(ep, auth)
	//Since the proxy server is not running, we expect an error.
	c.Assert(ps, IsNil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "socks connect .* dial tcp 127.0.0.1:1080: .*")
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
