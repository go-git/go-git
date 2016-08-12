package git

import (
	"gopkg.in/src-d/go-git.v4/clients"
	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
)

// Remote represents a connection to a remote repository
type Remote struct {
	Endpoint common.Endpoint
	Auth     common.AuthMethod

	upSrv  common.GitUploadPackService
	upInfo *common.GitUploadPackInfo
}

// NewRemote returns a new Remote, using as client http.DefaultClient
func NewRemote(url string) (*Remote, error) {
	return NewAuthenticatedRemote(url, nil)
}

// NewAuthenticatedRemote returns a new Remote using the given AuthMethod, using as
// client http.DefaultClient
func NewAuthenticatedRemote(url string, auth common.AuthMethod) (*Remote, error) {
	end, err := common.NewEndpoint(url)
	if err != nil {
		return nil, err
	}

	upSrv, err := clients.NewGitUploadPackService(url)
	if err != nil {
		return nil, err
	}
	return &Remote{
		Endpoint: end,
		Auth:     auth,
		upSrv:    upSrv,
	}, nil
}

// Connect with the endpoint
func (r *Remote) Connect() error {
	var err error
	if r.Auth == nil {
		err = r.upSrv.Connect(r.Endpoint)
	} else {
		err = r.upSrv.ConnectWithAuth(r.Endpoint, r.Auth)
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
func (r *Remote) Fetch(s core.ObjectStorage, o *FetchOptions) (h core.Hash, err error) {
	ref, err := r.Ref(o.ReferenceName, true)
	if err != nil {
		return core.ZeroHash, err
	}

	h = ref.Hash()
	req := &common.GitUploadPackRequest{}
	req.Want(h)

	reader, err := r.upSrv.Fetch(req)
	if err != nil {
		return core.ZeroHash, err
	}

	defer checkClose(reader, &err)
	stream := packfile.NewStream(reader)

	d := packfile.NewDecoder(stream)
	return h, d.Decode(s)
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
