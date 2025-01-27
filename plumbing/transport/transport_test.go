package transport

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

func TestSuiteCommon(t *testing.T) {
	suite.Run(t, new(SuiteCommon))
}

type SuiteCommon struct {
	suite.Suite
}

func (s *SuiteCommon) TestNewEndpointHTTP() {
	e, err := NewEndpoint("http://git:pass@github.com/user/repository.git?foo#bar")
	s.Nil(err)
	s.Equal("http", e.Protocol)
	s.Equal("git", e.User)
	s.Equal("pass", e.Password)
	s.Equal("github.com", e.Host)
	s.Equal(0, e.Port)
	s.Equal("/user/repository.git?foo#bar", e.Path)
	s.Equal("http://git:pass@github.com/user/repository.git?foo#bar", e.String())
}

func (s *SuiteCommon) TestNewEndpointPorts() {
	e, err := NewEndpoint("http://git:pass@github.com:8080/user/repository.git?foo#bar")
	s.Nil(err)
	s.Equal("http://git:pass@github.com:8080/user/repository.git?foo#bar", e.String())

	e, err = NewEndpoint("https://git:pass@github.com:443/user/repository.git?foo#bar")
	s.Nil(err)
	s.Equal("https://git:pass@github.com/user/repository.git?foo#bar", e.String())

	e, err = NewEndpoint("ssh://git:pass@github.com:22/user/repository.git?foo#bar")
	s.Nil(err)
	s.Equal("ssh://git:pass@github.com/user/repository.git?foo#bar", e.String())

	e, err = NewEndpoint("git://github.com:9418/user/repository.git?foo#bar")
	s.Nil(err)
	s.Equal("git://github.com/user/repository.git?foo#bar", e.String())
}

func (s *SuiteCommon) TestNewEndpointSSH() {
	e, err := NewEndpoint("ssh://git@github.com/user/repository.git")
	s.Nil(err)
	s.Equal("ssh", e.Protocol)
	s.Equal("git", e.User)
	s.Equal("", e.Password)
	s.Equal("github.com", e.Host)
	s.Equal(0, e.Port)
	s.Equal("/user/repository.git", e.Path)
	s.Equal("ssh://git@github.com/user/repository.git", e.String())
}

func (s *SuiteCommon) TestNewEndpointSSHNoUser() {
	e, err := NewEndpoint("ssh://github.com/user/repository.git")
	s.Nil(err)
	s.Equal("ssh", e.Protocol)
	s.Equal("", e.User)
	s.Equal("", e.Password)
	s.Equal("github.com", e.Host)
	s.Equal(0, e.Port)
	s.Equal("/user/repository.git", e.Path)
	s.Equal("ssh://github.com/user/repository.git", e.String())
}

func (s *SuiteCommon) TestNewEndpointSSHWithPort() {
	e, err := NewEndpoint("ssh://git@github.com:777/user/repository.git")
	s.Nil(err)
	s.Equal("ssh", e.Protocol)
	s.Equal("git", e.User)
	s.Equal("", e.Password)
	s.Equal("github.com", e.Host)
	s.Equal(777, e.Port)
	s.Equal("/user/repository.git", e.Path)
	s.Equal("ssh://git@github.com:777/user/repository.git", e.String())
}

func (s *SuiteCommon) TestNewEndpointSCPLike() {
	e, err := NewEndpoint("git@github.com:user/repository.git")
	s.Nil(err)
	s.Equal("ssh", e.Protocol)
	s.Equal("git", e.User)
	s.Equal("", e.Password)
	s.Equal("github.com", e.Host)
	s.Equal(22, e.Port)
	s.Equal("user/repository.git", e.Path)
	s.Equal("ssh://git@github.com/user/repository.git", e.String())
}

func (s *SuiteCommon) TestNewEndpointSCPLikeWithNumericPath() {
	e, err := NewEndpoint("git@github.com:9999/user/repository.git")
	s.Nil(err)
	s.Equal("ssh", e.Protocol)
	s.Equal("git", e.User)
	s.Equal("", e.Password)
	s.Equal("github.com", e.Host)
	s.Equal(22, e.Port)
	s.Equal("9999/user/repository.git", e.Path)
	s.Equal("ssh://git@github.com/9999/user/repository.git", e.String())
}

func (s *SuiteCommon) TestNewEndpointSCPLikeWithPort() {
	e, err := NewEndpoint("git@github.com:8080:9999/user/repository.git")
	s.Nil(err)
	s.Equal("ssh", e.Protocol)
	s.Equal("git", e.User)
	s.Equal("", e.Password)
	s.Equal("github.com", e.Host)
	s.Equal(8080, e.Port)
	s.Equal("9999/user/repository.git", e.Path)
	s.Equal("ssh://git@github.com:8080/9999/user/repository.git", e.String())
}

func (s *SuiteCommon) TestNewEndpointFileAbs() {
	var err error
	abs := "/foo.git"

	if runtime.GOOS == "windows" {
		abs, err = filepath.Abs(abs)
		s.Nil(err)
	}

	e, err := NewEndpoint("/foo.git")
	s.Nil(err)
	s.Equal("file", e.Protocol)
	s.Equal("", e.User)
	s.Equal("", e.Password)
	s.Equal("", e.Host)
	s.Equal(0, e.Port)
	s.Equal(abs, e.Path)
	s.Equal("file://"+abs, e.String())
}

func (s *SuiteCommon) TestNewEndpointFileRel() {
	abs := "foo.git"
	e, err := NewEndpoint("foo.git")
	s.Nil(err)
	s.Equal("file", e.Protocol)
	s.Equal("", e.User)
	s.Equal("", e.Password)
	s.Equal("", e.Host)
	s.Equal(0, e.Port)
	s.Equal(abs, e.Path)
	s.Equal("file://"+abs, e.String())
}

func (s *SuiteCommon) TestNewEndpointFileWindows() {
	abs := "C:\\foo.git"

	e, err := NewEndpoint("C:\\foo.git")
	s.Nil(err)
	s.Equal("file", e.Protocol)
	s.Equal("", e.User)
	s.Equal("", e.Password)
	s.Equal("", e.Host)
	s.Equal(0, e.Port)
	s.Equal(abs, e.Path)
	s.Equal("file://"+abs, e.String())
}

func (s *SuiteCommon) TestNewEndpointFileURL() {
	e, err := NewEndpoint("file:///foo.git")
	s.Nil(err)
	s.Equal("file", e.Protocol)
	s.Equal("", e.User)
	s.Equal("", e.Password)
	s.Equal("", e.Host)
	s.Equal(0, e.Port)
	s.Equal("/foo.git", e.Path)
	s.Equal("file:///foo.git", e.String())
}

func (s *SuiteCommon) TestValidEndpoint() {
	user := "person@mail.com"
	pass := " !\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	e, err := NewEndpoint(fmt.Sprintf(
		"http://%s:%s@github.com/user/repository.git",
		url.PathEscape(user),
		url.PathEscape(pass),
	))
	s.Nil(err)
	s.NotNil(e)
	s.Equal(user, e.User)
	s.Equal(pass, e.Password)
	s.Equal("github.com", e.Host)
	s.Equal("/user/repository.git", e.Path)

	s.Equal("http://person@mail.com:%20%21%22%23$%25&%27%28%29%2A+%2C-.%2F:%3B%3C=%3E%3F@%5B%5C%5D%5E_%60%7B%7C%7D~@github.com/user/repository.git", e.String())
}

func (s *SuiteCommon) TestNewEndpointInvalidURL() {
	e, err := NewEndpoint("http://\\")
	s.NotNil(err)
	s.Nil(e)
}

func (s *SuiteCommon) TestFilterUnsupportedCapabilities() {
	l := capability.NewList()
	l.Set(capability.MultiACK)
	l.Set(capability.MultiACKDetailed)

	s.False(l.Supports(capability.ThinPack))
}

func (s *SuiteCommon) TestNewEndpointIPv6() {
	e, err := NewEndpoint("http://[::1]:8080/foo.git")
	s.Nil(err)
	s.Equal("[::1]", e.Host)
	s.Equal("http://[::1]:8080/foo.git", e.String())
}
