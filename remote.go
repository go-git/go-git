package git

import (
	"errors"
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/sideband"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/client"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	"gopkg.in/src-d/go-git.v4/utils/ioutil"
)

var NoErrAlreadyUpToDate = errors.New("already up-to-date")

// Remote represents a connection to a remote repository
type Remote struct {
	c *config.RemoteConfig
	s Storer
	p sideband.Progress

	// cache fields, there during the connection is open
	endpoint     transport.Endpoint
	client       transport.Client
	fetchSession transport.FetchPackSession
	advRefs      *packp.AdvRefs
	refs         memory.ReferenceStorage
}

func newRemote(s Storer, p sideband.Progress, c *config.RemoteConfig) *Remote {
	return &Remote{s: s, p: p, c: c}
}

// Config return the config
func (r *Remote) Config() *config.RemoteConfig {
	return r.c
}

// Connect with the endpoint
func (r *Remote) Connect() error {
	if err := r.initClient(); err != nil {
		return err
	}

	var err error
	r.fetchSession, err = r.client.NewFetchPackSession(r.endpoint)
	if err != nil {
		return err
	}

	return r.retrieveAdvertisedReferences()
}

func (r *Remote) initClient() error {
	var err error
	r.endpoint, err = transport.NewEndpoint(r.c.URL)
	if err != nil {
		return err
	}

	if r.client != nil {
		return nil
	}

	r.client, err = client.NewClient(r.endpoint)
	if err != nil {
		return err
	}

	return nil
}

func (r *Remote) retrieveAdvertisedReferences() error {
	var err error
	r.advRefs, err = r.fetchSession.AdvertisedReferences()
	if err != nil {
		return err
	}

	r.refs, err = r.advRefs.AllReferences()
	return err
}

// AdvertisedReferences returns the git-upload-pack advertised references.
func (r *Remote) AdvertisedReferences() *packp.AdvRefs {
	return r.advRefs
}

// Capabilities returns the remote capabilities
func (r *Remote) Capabilities() *capability.List {
	return r.advRefs.Capabilities
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

	req, err := r.buildRequest(r.s, o, refs)
	if err != nil {
		return err
	}

	reader, err := r.fetchSession.FetchPack(req)
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(reader, &err)

	if err := r.updateShallow(o, reader); err != nil {
		return err
	}

	if err = r.updateObjectStorage(
		r.buildSidebandIfSupported(req.Capabilities, reader),
	); err != nil {
		return err
	}

	return r.updateLocalReferenceStorage(o.RefSpecs, refs)
}

func (r *Remote) getWantedReferences(spec []config.RefSpec) ([]*plumbing.Reference, error) {
	var refs []*plumbing.Reference
	iter, err := r.References()
	if err != nil {
		return refs, err
	}

	wantTags := true
	for _, s := range spec {
		if !s.IsWildcard() {
			wantTags = false
			break
		}
	}

	return refs, iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}

		if !config.MatchAny(spec, ref.Name()) {
			if !ref.IsTag() || !wantTags {
				return nil
			}
		}

		_, err := r.s.EncodedObject(plumbing.CommitObject, ref.Hash())
		if err == plumbing.ErrObjectNotFound {
			refs = append(refs, ref)
			return nil
		}

		return err
	})
}

func (r *Remote) buildRequest(
	s storer.ReferenceStorer, o *FetchOptions, refs []*plumbing.Reference,
) (*packp.UploadPackRequest, error) {
	req := packp.NewUploadPackRequestFromCapabilities(r.advRefs.Capabilities)

	if o.Depth != 0 {
		req.Depth = packp.DepthCommits(o.Depth)
		req.Capabilities.Set(capability.Shallow)
	}

	if r.p == nil && r.advRefs.Capabilities.Supports(capability.NoProgress) {
		req.Capabilities.Set(capability.NoProgress)
	}

	for _, ref := range refs {
		req.Wants = append(req.Wants, ref.Hash())
	}

	i, err := s.IterReferences()
	if err != nil {
		return nil, err
	}

	err = i.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}

		req.Haves = append(req.Haves, ref.Hash())
		return nil
	})

	return req, err
}

func (r *Remote) updateObjectStorage(reader io.Reader) error {
	if sw, ok := r.s.(storer.PackfileWriter); ok {
		w, err := sw.PackfileWriter()
		if err != nil {
			return err
		}

		defer w.Close()
		_, err = io.Copy(w, reader)
		return err
	}

	stream := packfile.NewScanner(reader)
	d, err := packfile.NewDecoder(stream, r.s)
	if err != nil {
		return err
	}

	_, err = d.Decode()
	return err
}

func (r *Remote) buildSidebandIfSupported(l *capability.List, reader io.Reader) io.Reader {
	var t sideband.Type

	switch {
	case l.Supports(capability.Sideband):
		t = sideband.Sideband
	case l.Supports(capability.Sideband64k):
		t = sideband.Sideband64k
	default:
		return reader
	}

	d := sideband.NewDemuxer(t, reader)
	d.Progress = r.p

	return d
}

func (r *Remote) updateLocalReferenceStorage(specs []config.RefSpec, refs []*plumbing.Reference) error {
	for _, spec := range specs {
		for _, ref := range refs {
			if !spec.Match(ref.Name()) {
				continue
			}

			if ref.Type() != plumbing.HashReference {
				continue
			}

			name := spec.Dst(ref.Name())
			n := plumbing.NewHashReference(name, ref.Hash())
			if err := r.s.SetReference(n); err != nil {
				return err
			}
		}
	}

	return r.buildFetchedTags()
}

func (r *Remote) buildFetchedTags() error {
	iter, err := r.References()
	if err != nil {
		return err
	}

	return iter.ForEach(func(ref *plumbing.Reference) error {
		if !ref.IsTag() {
			return nil
		}

		_, err := r.s.EncodedObject(plumbing.AnyObject, ref.Hash())
		if err == plumbing.ErrObjectNotFound {
			return nil
		}

		if err != nil {
			return err
		}

		return r.s.SetReference(ref)
	})
}

func (r *Remote) updateShallow(o *FetchOptions, resp *packp.UploadPackResponse) error {
	if o.Depth == 0 {
		return nil
	}

	return r.s.SetShallow(resp.Shallows)
}

// Head returns the Reference of the HEAD
func (r *Remote) Head() *plumbing.Reference {
	ref, err := storer.ResolveReference(r.refs, plumbing.HEAD)
	if err != nil {
		return nil
	}

	return ref
}

// Reference returns a Reference for a ReferenceName.
func (r *Remote) Reference(name plumbing.ReferenceName, resolved bool) (*plumbing.Reference, error) {
	if resolved {
		return storer.ResolveReference(r.refs, name)
	}

	return r.refs.Reference(name)
}

// References returns an iterator for all references.
func (r *Remote) References() (storer.ReferenceIter, error) {
	return r.refs.IterReferences()
}

// Disconnect from the remote and save the config
func (r *Remote) Disconnect() error {
	r.advRefs = nil
	return r.fetchSession.Close()
}

func (r *Remote) String() string {
	fetch := r.c.URL
	push := r.c.URL

	return fmt.Sprintf("%s\t%s (fetch)\n%[1]s\t%s (push)", r.c.Name, fetch, push)
}
