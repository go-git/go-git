package clients

import (
	"fmt"
	"io"
	"testing"

	"gopkg.in/src-d/go-git.v4/plumbing/client/common"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteCommon struct{}

var _ = Suite(&SuiteCommon{})

func (s *SuiteCommon) TestNewGitUploadPackServiceHTTP(c *C) {
	e, err := common.NewEndpoint("http://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err := NewGitUploadPackService(e)
	c.Assert(err, IsNil)
	c.Assert(typeAsString(output), Equals, "*http.GitUploadPackService")

	e, err = common.NewEndpoint("https://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err = NewGitUploadPackService(e)
	c.Assert(err, IsNil)
	c.Assert(typeAsString(output), Equals, "*http.GitUploadPackService")
}

func (s *SuiteCommon) TestNewGitUploadPackServiceSSH(c *C) {
	e, err := common.NewEndpoint("ssh://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err := NewGitUploadPackService(e)
	c.Assert(err, IsNil)
	c.Assert(typeAsString(output), Equals, "*ssh.GitUploadPackService")
}

func (s *SuiteCommon) TestNewGitUploadPackServiceUnknown(c *C) {
	e, err := common.NewEndpoint("unknown://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = NewGitUploadPackService(e)
	c.Assert(err, NotNil)
}

func (s *SuiteCommon) TestInstallProtocol(c *C) {
	InstallProtocol("newscheme", newDummyProtocolService)
	c.Assert(Protocols["newscheme"], NotNil)
}

type dummyProtocolService struct{}

func newDummyProtocolService(common.Endpoint) common.GitUploadPackService {
	return &dummyProtocolService{}
}

func (s *dummyProtocolService) Connect() error {
	return nil
}

func (s *dummyProtocolService) SetAuth(auth common.AuthMethod) error {
	return nil
}

func (s *dummyProtocolService) Info() (*common.GitUploadPackInfo, error) {
	return nil, nil
}

func (s *dummyProtocolService) Fetch(r *common.GitUploadPackRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (s *dummyProtocolService) Disconnect() error {
	return nil
}

func typeAsString(v interface{}) string {
	return fmt.Sprintf("%T", v)
}
