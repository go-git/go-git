package git

import (
	"gopkg.in/src-d/go-git.v4/clients"
	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
)

// Remote represents a connection to a remote repository
type Remote struct {
	Name     string
	Endpoint common.Endpoint
	Auth     common.AuthMethod

	upSrv  common.GitUploadPackService
	upInfo *common.GitUploadPackInfo
}

// NewRemote returns a new Remote, using as client http.DefaultClient
func NewRemote(name, url string) (*Remote, error) {
	return NewAuthenticatedRemote(name, url, nil)
}

// NewAuthenticatedRemote returns a new Remote using the given AuthMethod, using as
// client http.DefaultClient
func NewAuthenticatedRemote(name, url string, auth common.AuthMethod) (*Remote, error) {
	endpoint, err := common.NewEndpoint(url)
	if err != nil {
		return nil, err
	}

	upSrv, err := clients.NewGitUploadPackService(endpoint)
	if err != nil {
		return nil, err
	}

	return &Remote{
		Endpoint: endpoint,
		Name:     name,
		Auth:     auth,
		upSrv:    upSrv,
	}, nil
}

// Connect with the endpoint
func (r *Remote) Connect() error {
	var err error
	if r.Auth == nil {
		err = r.upSrv.Connect()
	} else {
		err = r.upSrv.ConnectWithAuth(r.Auth)
	}

	if err != nil {
		return err
	}

	return r.retrieveUpInfo()
}

func (r *Remote) retrieveUpInfo() error {
	var err error
	if r.upInfo, err = r.upSrv.Info(); err != nil {
		return err
	}

	return nil
}

// Info returns the git-upload-pack info
func (r *Remote) Info() *common.GitUploadPackInfo {
	return r.upInfo
}

// Capabilities returns the remote capabilities
func (r *Remote) Capabilities() *common.Capabilities {
	return r.upInfo.Capabilities
}

// Fetch returns a reader using the request
func (r *Remote) Fetch(s core.ObjectStorage, o *RemoteFetchOptions) (err error) {
	if err := o.Validate(); err != nil {
		return err
	}

	req := &common.GitUploadPackRequest{}
	req.Depth = o.Depth

	for _, ref := range o.References {
		if ref.Type() != core.HashReference {
			continue
		}

		req.Want(ref.Hash())
	}

	reader, err := r.upSrv.Fetch(req)
	if err != nil {
		return err
	}

	defer checkClose(reader, &err)
	stream := packfile.NewStream(reader)

	d := packfile.NewDecoder(stream)
	return d.Decode(s)
}

// Head returns the Reference of the HEAD
func (r *Remote) Head() *core.Reference {
	return r.upInfo.Head()
}

// Ref returns the Hash pointing the given refName
func (r *Remote) Ref(name core.ReferenceName, resolved bool) (*core.Reference, error) {
	if resolved {
		return core.ResolveReference(r.upInfo.Refs, name)
	}

	return r.upInfo.Refs.Get(name)
}

// Refs returns a map with all the References
func (r *Remote) Refs() core.ReferenceIter {
	i, _ := r.upInfo.Refs.Iter()
	return i
}
