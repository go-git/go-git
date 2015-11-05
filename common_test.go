package git

import (
	"io"
	"os"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/core"
)

func Test(t *testing.T) { TestingT(t) }

type MockGitUploadPackService struct {
	Auth common.AuthMethod
}

func (s *MockGitUploadPackService) Connect(url common.Endpoint) error {
	return nil
}

func (s *MockGitUploadPackService) ConnectWithAuth(url common.Endpoint, auth common.AuthMethod) error {
	s.Auth = auth
	return nil
}

func (s *MockGitUploadPackService) Info() (*common.GitUploadPackInfo, error) {
	hash := core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	cap := common.NewCapabilities()
	cap.Decode("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADmulti_ack thin-pack side-band side-band-64k ofs-delta shallow no-progress include-tag multi_ack_detailed no-done symref=HEAD:refs/heads/master agent=git/2:2.4.8~dbussink-fix-enterprise-tokens-compilation-1167-gc7006cf")

	return &common.GitUploadPackInfo{
		Capabilities: cap,
		Head:         hash,
		Refs:         map[string]core.Hash{"refs/heads/master": hash},
	}, nil
}

func (s *MockGitUploadPackService) Fetch(*common.GitUploadPackRequest) (io.ReadCloser, error) {
	r, _ := os.Open("formats/packfile/fixtures/git-fixture.ref-delta")
	return r, nil
}
