package git

import (
	"errors"
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/clients"
	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
)

var NoErrAlreadyUpToDate = errors.New("already up-to-date")

// Remote represents a connection to a remote repository
type Remote struct {
	c *config.RemoteConfig
	s Storage

	// cache fields, there during the connection is open
	upSrv  common.GitUploadPackService
	upInfo *common.GitUploadPackInfo
}

func newRemote(s Storage, c *config.RemoteConfig) *Remote {
	return &Remote{s: s, c: c}
}

// Config return the config
func (r *Remote) Config() *config.RemoteConfig {
	return r.c
}

// Connect with the endpoint
func (r *Remote) Connect() error {
	if err := r.connectUploadPackService(); err != nil {
		return err
	}

	return r.retrieveUpInfo()
}

func (r *Remote) connectUploadPackService() error {
	endpoint, err := common.NewEndpoint(r.c.URL)
	if err != nil {
		return err
	}

	r.upSrv, err = clients.NewGitUploadPackService(endpoint)
	if err != nil {
		return err
	}

	return r.upSrv.Connect()
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
func (r *Remote) Fetch(o *FetchOptions) (err error) {
	if err := o.Validate(); err != nil {
		return err
	}

	if len(o.RefSpecs) == 0 {
		o.RefSpecs = r.c.Fetch
	}

	refs, err := r.getWantedReferences(o.RefSpecs)
	if err != nil {
		return err
	}

	if len(refs) == 0 {
		return NoErrAlreadyUpToDate
	}

	req, err := r.buildRequest(r.s.ReferenceStorage(), o, refs)
	if err != nil {
		return err
	}

	reader, err := r.upSrv.Fetch(req)
	if err != nil {
		return err
	}

	defer checkClose(reader, &err)
	if err := r.updateObjectStorage(reader); err != nil {
		return err
	}

	return r.updateLocalReferenceStorage(o.RefSpecs, refs)
}

func (r *Remote) getWantedReferences(spec []config.RefSpec) ([]*core.Reference, error) {
	var refs []*core.Reference
	iter, err := r.Refs()
	if err != nil {
		return refs, err
	}

	return refs, iter.ForEach(func(ref *core.Reference) error {
		if ref.Type() != core.HashReference {
			return nil
		}

		if !config.MatchAny(spec, ref.Name()) {
			return nil
		}

		_, err := r.s.ObjectStorage().Get(core.CommitObject, ref.Hash())
		if err == core.ErrObjectNotFound {
			refs = append(refs, ref)
			return nil
		}

		return err
	})
}

func (r *Remote) buildRequest(
	s core.ReferenceStorage, o *FetchOptions, refs []*core.Reference,
) (*common.GitUploadPackRequest, error) {
	req := &common.GitUploadPackRequest{}
	req.Depth = o.Depth

	for _, ref := range refs {
		req.Want(ref.Hash())
	}

	i, err := s.Iter()
	if err != nil {
		return nil, err
	}

	err = i.ForEach(func(ref *core.Reference) error {
		if ref.Type() != core.HashReference {
			return nil
		}

		req.Have(ref.Hash())
		return nil
	})

	return req, err
}

func (r *Remote) updateObjectStorage(reader io.Reader) error {
	s := r.s.ObjectStorage()
	if sw, ok := s.(core.ObjectStorageWrite); ok {
		w, err := sw.Writer()
		if err != nil {
			return err
		}

		defer w.Close()
		_, err = io.Copy(w, reader)
		return err
	}

	stream := packfile.NewScanner(reader)
	d, err := packfile.NewDecoder(stream, s)
	if err != nil {
		return err
	}

	_, err = d.Decode()
	return err
}

func (r *Remote) updateLocalReferenceStorage(specs []config.RefSpec, refs []*core.Reference) error {
	for _, ref := range refs {
		for _, spec := range specs {
			if !spec.Match(ref.Name()) {
				continue
			}

			if ref.Type() != core.HashReference {
				continue
			}

			name := spec.Dst(ref.Name())
			n := core.NewHashReference(name, ref.Hash())
			if err := r.s.ReferenceStorage().Set(n); err != nil {
				return err
			}
		}
	}

	return nil
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
func (r *Remote) Refs() (core.ReferenceIter, error) {
	return r.upInfo.Refs.Iter()
}

// Disconnect from the remote and save the config
func (r *Remote) Disconnect() error {
	r.upInfo = nil
	return r.upSrv.Disconnect()
}

func (r *Remote) String() string {
	fetch := r.c.URL
	push := r.c.URL

	return fmt.Sprintf("%s\t%s (fetch)\n%[1]s\t%s (push)", r.c.Name, fetch, push)
}
