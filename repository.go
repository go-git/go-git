package git

import (
	"errors"
	"fmt"
	"os"

	"srcd.works/go-git.v4/config"
	"srcd.works/go-git.v4/plumbing"
	"srcd.works/go-git.v4/plumbing/object"
	"srcd.works/go-git.v4/plumbing/storer"
	"srcd.works/go-git.v4/storage/filesystem"

	"srcd.works/go-billy.v1"
	"srcd.works/go-billy.v1/osfs"
)

var (
	ErrObjectNotFound          = errors.New("object not found")
	ErrInvalidReference        = errors.New("invalid reference, should be a tag or a branch")
	ErrRepositoryNotExists     = errors.New("repository not exists")
	ErrRepositoryAlreadyExists = errors.New("repository already exists")
	ErrRemoteNotFound          = errors.New("remote not found")
	ErrRemoteExists            = errors.New("remote already exists")
	ErrWorktreeNotProvided     = errors.New("worktree should be provided")
	ErrIsBareRepository        = errors.New("worktree not available in a bare repository")
)

// Repository giturl string, auth common.AuthMethod repository struct
type Repository struct {
	r  map[string]*Remote
	s  Storer
	wt billy.Filesystem
}

// Init creates an empty git repository, based on the given Storer and worktree.
// The worktree Filesystem is optional, if nil a bare repository is created. If
// the given storer is not empty ErrRepositoryAlreadyExists is returned
func Init(s Storer, worktree billy.Filesystem) (*Repository, error) {
	r := newRepository(s, worktree)
	_, err := r.Reference(plumbing.HEAD, false)
	switch err {
	case plumbing.ErrReferenceNotFound:
	case nil:
		return nil, ErrRepositoryAlreadyExists
	default:
		return nil, err
	}

	h := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)
	if err := s.SetReference(h); err != nil {
		return nil, err
	}

	if worktree == nil {
		r.setIsBare(true)
	}

	return r, nil
}

// Open opens a git repository using the given Storer and worktree filesystem,
// if the given storer is complete empty ErrRepositoryNotExists is returned.
// The worktree can be nil when the repository being opened is bare, if the
// repository is a normal one (not bare) and worktree is nil the err
// ErrWorktreeNotProvided is returned
func Open(s Storer, worktree billy.Filesystem) (*Repository, error) {
	_, err := s.Reference(plumbing.HEAD)
	if err == plumbing.ErrReferenceNotFound {
		return nil, ErrRepositoryNotExists
	}

	if err != nil {
		return nil, err
	}

	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}

	if !cfg.Core.IsBare && worktree == nil {
		return nil, ErrWorktreeNotProvided
	}

	return newRepository(s, worktree), nil
}

// Clone a repository into the given Storer and worktree Filesystem with the
// given options, if worktree is nil a bare repository is created. If the given
// storer is not empty ErrRepositoryAlreadyExists is returned
func Clone(s Storer, worktree billy.Filesystem, o *CloneOptions) (*Repository, error) {
	r, err := Init(s, worktree)
	if err != nil {
		return nil, err
	}

	return r, r.clone(o)
}

// PlainInit create an empty git repository at the given path. isBare defines
// if the repository will have worktree (non-bare) or not (bare), if the path
// is not empty ErrRepositoryAlreadyExists is returned
func PlainInit(path string, isBare bool) (*Repository, error) {
	var wt, dot billy.Filesystem

	if isBare {
		dot = osfs.New(path)
	} else {
		wt = osfs.New(path)
		dot = wt.Dir(".git")
	}

	s, err := filesystem.NewStorage(dot)
	if err != nil {
		return nil, err
	}

	return Init(s, wt)
}

// PlainOpen opens a git repository from the given path. It detects is the
// repository is bare or a normal one. If the path doesn't contain a valid
// repository ErrRepositoryNotExists is returned
func PlainOpen(path string) (*Repository, error) {
	var wt, dot billy.Filesystem

	fs := osfs.New(path)
	if _, err := fs.Stat(".git"); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		dot = fs
	} else {
		wt = fs
		dot = fs.Dir(".git")
	}

	s, err := filesystem.NewStorage(dot)
	if err != nil {
		return nil, err
	}

	return Open(s, wt)
}

// PlainClone a repository into the path with the given options, isBare defines
// if the new repository will be bare or normal. If the path is not empty
// ErrRepositoryAlreadyExists is returned
func PlainClone(path string, isBare bool, o *CloneOptions) (*Repository, error) {
	r, err := PlainInit(path, isBare)
	if err != nil {
		return nil, err
	}

	return r, r.clone(o)
}

func newRepository(s Storer, worktree billy.Filesystem) *Repository {
	return &Repository{
		s:  s,
		wt: worktree,
		r:  make(map[string]*Remote, 0),
	}
}

// Config return the repository config
func (r *Repository) Config() (*config.Config, error) {
	return r.s.Config()
}

// Remote return a remote if exists
func (r *Repository) Remote(name string) (*Remote, error) {
	cfg, err := r.s.Config()
	if err != nil {
		return nil, err
	}

	c, ok := cfg.Remotes[name]
	if !ok {
		return nil, ErrRemoteNotFound
	}

	return newRemote(r.s, c), nil
}

// Remotes return all the remotes
func (r *Repository) Remotes() ([]*Remote, error) {
	cfg, err := r.s.Config()
	if err != nil {
		return nil, err
	}

	remotes := make([]*Remote, len(cfg.Remotes))

	var i int
	for _, c := range cfg.Remotes {
		remotes[i] = newRemote(r.s, c)
		i++
	}

	return remotes, nil
}

// CreateRemote creates a new remote
func (r *Repository) CreateRemote(c *config.RemoteConfig) (*Remote, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	remote := newRemote(r.s, c)

	cfg, err := r.s.Config()
	if err != nil {
		return nil, err
	}

	if _, ok := cfg.Remotes[c.Name]; ok {
		return nil, ErrRemoteExists
	}

	cfg.Remotes[c.Name] = c
	return remote, r.s.SetConfig(cfg)
}

// DeleteRemote delete a remote from the repository and delete the config
func (r *Repository) DeleteRemote(name string) error {
	cfg, err := r.s.Config()
	if err != nil {
		return err
	}

	if _, ok := cfg.Remotes[name]; !ok {
		return ErrRemoteNotFound
	}

	delete(cfg.Remotes, name)
	return r.s.SetConfig(cfg)
}

// Clone clones a remote repository
func (r *Repository) clone(o *CloneOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	// marks the repository as bare in the config, until we have Worktree, all
	// the repository are bare
	if err := r.setIsBare(true); err != nil {
		return err
	}

	c := &config.RemoteConfig{
		Name: o.RemoteName,
		URL:  o.URL,
	}

	remote, err := r.CreateRemote(c)
	if err != nil {
		return err
	}

	remoteRefs, err := remote.fetch(&FetchOptions{
		RefSpecs: r.cloneRefSpec(o, c),
		Depth:    o.Depth,
		Auth:     o.Auth,
		Progress: o.Progress,
	})
	if err != nil {
		return err
	}

	head, err := storer.ResolveReference(remoteRefs, o.ReferenceName)
	if err != nil {
		return err
	}

	if _, err := r.updateReferences(c.Fetch, o.ReferenceName, head); err != nil {
		return err
	}

	if err := r.updateWorktree(); err != nil {
		return err
	}

	return r.updateRemoteConfig(remote, o, c, head)
}

func (r *Repository) cloneRefSpec(o *CloneOptions,
	c *config.RemoteConfig) []config.RefSpec {

	if !o.SingleBranch {
		return c.Fetch
	}

	var rs string

	if o.ReferenceName == plumbing.HEAD {
		rs = fmt.Sprintf(refspecSingleBranchHEAD, c.Name)
	} else {
		rs = fmt.Sprintf(refspecSingleBranch,
			o.ReferenceName.Short(), c.Name)
	}

	return []config.RefSpec{config.RefSpec(rs)}
}

func (r *Repository) setIsBare(isBare bool) error {
	cfg, err := r.s.Config()
	if err != nil {
		return err
	}

	cfg.Core.IsBare = isBare
	return r.s.SetConfig(cfg)
}

const (
	refspecSingleBranch     = "+refs/heads/%s:refs/remotes/%s/%[1]s"
	refspecSingleBranchHEAD = "+HEAD:refs/remotes/%s/HEAD"
)

func (r *Repository) updateRemoteConfig(remote *Remote, o *CloneOptions,
	c *config.RemoteConfig, head *plumbing.Reference) error {

	if !o.SingleBranch {
		return nil
	}

	c.Fetch = []config.RefSpec{config.RefSpec(fmt.Sprintf(
		refspecSingleBranch, head.Name().Short(), c.Name,
	))}

	cfg, err := r.s.Config()
	if err != nil {
		return err
	}

	cfg.Remotes[c.Name] = c
	return r.s.SetConfig(cfg)
}

func (r *Repository) updateReferences(spec []config.RefSpec,
	headName plumbing.ReferenceName, resolvedHead *plumbing.Reference) (updated bool, err error) {

	if !resolvedHead.IsBranch() {
		// Detached HEAD mode
		head := plumbing.NewHashReference(plumbing.HEAD, resolvedHead.Hash())
		return updateReferenceStorerIfNeeded(r.s, head)
	}

	refs := []*plumbing.Reference{
		// Create local reference for the resolved head
		resolvedHead,
		// Create local symbolic HEAD
		plumbing.NewSymbolicReference(plumbing.HEAD, resolvedHead.Name()),
	}

	refs = append(refs, r.calculateRemoteHeadReference(spec, resolvedHead)...)

	for _, ref := range refs {
		u, err := updateReferenceStorerIfNeeded(r.s, ref)
		if err != nil {
			return updated, err
		}

		if u {
			updated = true
		}
	}

	return
}

func (r *Repository) calculateRemoteHeadReference(spec []config.RefSpec,
	resolvedHead *plumbing.Reference) []*plumbing.Reference {

	var refs []*plumbing.Reference

	// Create resolved HEAD reference with remote prefix if it does not
	// exist. This is needed when using single branch and HEAD.
	for _, rs := range spec {
		name := resolvedHead.Name()
		if !rs.Match(name) {
			continue
		}

		name = rs.Dst(name)
		_, err := r.s.Reference(name)
		if err == plumbing.ErrReferenceNotFound {
			refs = append(refs, plumbing.NewHashReference(name, resolvedHead.Hash()))
		}
	}

	return refs
}

func updateReferenceStorerIfNeeded(
	s storer.ReferenceStorer, r *plumbing.Reference) (updated bool, err error) {

	p, err := s.Reference(r.Name())
	if err != nil && err != plumbing.ErrReferenceNotFound {
		return false, err
	}

	// we use the string method to compare references, is the easiest way
	if err == plumbing.ErrReferenceNotFound || r.String() != p.String() {
		if err := s.SetReference(r); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

// Pull incorporates changes from a remote repository into the current branch.
// Returns nil if the operation is successful, NoErrAlreadyUpToDate if there are
// no changes to be fetched, or an error.
func (r *Repository) Pull(o *PullOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	remote, err := r.Remote(o.RemoteName)
	if err != nil {
		return err
	}

	remoteRefs, err := remote.fetch(&FetchOptions{
		Depth:    o.Depth,
		Auth:     o.Auth,
		Progress: o.Progress,
	})

	updated := true
	if err == NoErrAlreadyUpToDate {
		updated = false
	} else if err != nil {
		return err
	}

	head, err := storer.ResolveReference(remoteRefs, o.ReferenceName)
	if err != nil {
		return err
	}

	refsUpdated, err := r.updateReferences(remote.c.Fetch, o.ReferenceName, head)
	if err != nil {
		return err
	}

	if refsUpdated {
		updated = refsUpdated
	}

	if !updated {
		return NoErrAlreadyUpToDate
	}

	return r.updateWorktree()
}

func (r *Repository) updateWorktree() error {
	if r.wt == nil {
		return nil
	}

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	h, err := r.Head()
	if err != nil {
		return err
	}

	return w.Checkout(h.Hash())
}

// Fetch fetches changes from a remote repository.
// Returns nil if the operation is successful, NoErrAlreadyUpToDate if there are
// no changes to be fetched, or an error.
func (r *Repository) Fetch(o *FetchOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	remote, err := r.Remote(o.RemoteName)
	if err != nil {
		return err
	}

	return remote.Fetch(o)
}

// Push pushes changes to a remote.
func (r *Repository) Push(o *PushOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	remote, err := r.Remote(o.RemoteName)
	if err != nil {
		return err
	}

	return remote.Push(o)
}

// Commit return the commit with the given hash
func (r *Repository) Commit(h plumbing.Hash) (*object.Commit, error) {
	return object.GetCommit(r.s, h)
}

// Commits decode the objects into commits
func (r *Repository) Commits() (*object.CommitIter, error) {
	iter, err := r.s.IterEncodedObjects(plumbing.CommitObject)
	if err != nil {
		return nil, err
	}

	return object.NewCommitIter(r.s, iter), nil
}

// Tree return the tree with the given hash
func (r *Repository) Tree(h plumbing.Hash) (*object.Tree, error) {
	return object.GetTree(r.s, h)
}

// Trees decodes the objects into trees
func (r *Repository) Trees() (*object.TreeIter, error) {
	iter, err := r.s.IterEncodedObjects(plumbing.TreeObject)
	if err != nil {
		return nil, err
	}

	return object.NewTreeIter(r.s, iter), nil
}

// Blob returns the blob with the given hash
func (r *Repository) Blob(h plumbing.Hash) (*object.Blob, error) {
	return object.GetBlob(r.s, h)
}

// Blobs decodes the objects into blobs
func (r *Repository) Blobs() (*object.BlobIter, error) {
	iter, err := r.s.IterEncodedObjects(plumbing.BlobObject)
	if err != nil {
		return nil, err
	}

	return object.NewBlobIter(r.s, iter), nil
}

// Tag returns a tag with the given hash.
func (r *Repository) Tag(h plumbing.Hash) (*object.Tag, error) {
	return object.GetTag(r.s, h)
}

// Tags returns a object.TagIter that can step through all of the annotated tags
// in the repository.
func (r *Repository) Tags() (*object.TagIter, error) {
	iter, err := r.s.IterEncodedObjects(plumbing.TagObject)
	if err != nil {
		return nil, err
	}

	return object.NewTagIter(r.s, iter), nil
}

// Object returns an object with the given hash.
func (r *Repository) Object(t plumbing.ObjectType, h plumbing.Hash) (object.Object, error) {
	obj, err := r.s.EncodedObject(t, h)
	if err != nil {
		if err == plumbing.ErrObjectNotFound {
			return nil, ErrObjectNotFound
		}

		return nil, err
	}

	return object.DecodeObject(r.s, obj)
}

// Objects returns an object.ObjectIter that can step through all of the annotated tags
// in the repository.
func (r *Repository) Objects() (*object.ObjectIter, error) {
	iter, err := r.s.IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		return nil, err
	}

	return object.NewObjectIter(r.s, iter), nil
}

// Head returns the reference where HEAD is pointing to.
func (r *Repository) Head() (*plumbing.Reference, error) {
	return storer.ResolveReference(r.s, plumbing.HEAD)
}

// Reference returns the reference for a given reference name. If resolved is
// true, any symbolic reference will be resolved.
func (r *Repository) Reference(name plumbing.ReferenceName, resolved bool) (
	*plumbing.Reference, error) {

	if resolved {
		return storer.ResolveReference(r.s, name)
	}

	return r.s.Reference(name)
}

// References returns a ReferenceIter for all references.
func (r *Repository) References() (storer.ReferenceIter, error) {
	return r.s.IterReferences()
}

// Worktree returns a worktree based on the given fs, if nil the default
// worktree will be used.
func (r *Repository) Worktree() (*Worktree, error) {
	if r.wt == nil {
		return nil, ErrIsBareRepository
	}

	return &Worktree{r: r, fs: r.wt}, nil
}
