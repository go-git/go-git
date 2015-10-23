package git

import (
	"io"

	"gopkg.in/src-d/go-git.v2/clients"
	"gopkg.in/src-d/go-git.v2/clients/common"
)

type Remote struct {
	Endpoint common.Endpoint

	upSrv  clients.GitUploadPackService
	upInfo *common.GitUploadPackInfo
}

// NewRemote returns a new Remote, using as client http.DefaultClient
func NewRemote(url string) (*Remote, error) {
	end, err := common.NewEndpoint(url)
	if err != nil {
		return nil, err
	}

	return &Remote{
		Endpoint: end,
		upSrv:    clients.NewGitUploadPackService(),
	}, nil
}

// Connect with the endpoint
func (r *Remote) Connect() error {
	if err := r.upSrv.Connect(r.Endpoint); err != nil {
		return err
	}

	var err error
	if r.upInfo, err = r.upSrv.Info(); err != nil {
		return err
	}

	return nil
}

// Capabilities returns the remote capabilities
func (r *Remote) Capabilities() common.Capabilities {
	return r.upInfo.Capabilities
}

// DefaultBranch retrieve the name of the remote's default branch
func (r *Remote) DefaultBranch() string {
	return r.upInfo.Capabilities.SymbolicReference("HEAD")
}

// Fetch returns a reader using the request
func (r *Remote) Fetch(req *common.GitUploadPackRequest) (io.ReadCloser, error) {
	return r.upSrv.Fetch(req)
}

// FetchDefaultBranch returns a reader for the default branch
func (r *Remote) FetchDefaultBranch() (io.ReadCloser, error) {
	return r.Fetch(&common.GitUploadPackRequest{
		Want: []string{r.upInfo.Refs[r.DefaultBranch()].Id},
	})
}
