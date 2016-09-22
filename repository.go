package git

import (
	"errors"
	"fmt"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

var (
	ErrObjectNotFound     = errors.New("object not found")
	ErrInvalidReference   = errors.New("invalid reference, should be a tag or a branch")
	ErrRepositoryNonEmpty = errors.New("repository non empty")
)

// Repository giturl string, auth common.AuthMethod repository struct
type Repository struct {
	r map[string]*Remote
	s Storage
}

// NewMemoryRepository creates a new repository, backed by a memory.Storage
func NewMemoryRepository() *Repository {
	r, _ := NewRepository(memory.NewStorage())
	return r
}

// NewFilesystemRepository creates a new repository, backed by a filesystem.Storage
// based on a fs.OS, if you want to use a custom one you need to use the function
// NewRepository and build you filesystem.Storage
func NewFilesystemRepository(path string) (*Repository, error) {
	s, err := filesystem.NewStorage(fs.NewOS(path))
	if err != nil {
		return nil, err
	}

	return NewRepository(s)
}

// NewRepository creates a new repository with the given Storage
func NewRepository(s Storage) (*Repository, error) {
	return &Repository{
		s: s,
		r: make(map[string]*Remote, 0),
	}, nil
}

// Remote return a remote if exists
func (r *Repository) Remote(name string) (*Remote, error) {
	c, err := r.s.ConfigStorage().Remote(name)
	if err != nil {
		return nil, err
	}

	return newRemote(r.s, c), nil
}

// Remotes return all the remotes
func (r *Repository) Remotes() ([]*Remote, error) {
	config, err := r.s.ConfigStorage().Remotes()
	if err != nil {
		return nil, err
	}

	remotes := make([]*Remote, len(config))
	for i, c := range config {
		remotes[i] = newRemote(r.s, c)
	}

	return remotes, nil
}

// CreateRemote creates a new remote
func (r *Repository) CreateRemote(c *config.RemoteConfig) (*Remote, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	remote := newRemote(r.s, c)
	if err := r.s.ConfigStorage().SetRemote(c); err != nil {
		return nil, err
	}

	return remote, nil
}

// DeleteRemote delete a remote from the repository and delete the config
func (r *Repository) DeleteRemote(name string) error {
	return r.s.ConfigStorage().DeleteRemote(name)
}

// Clone clones a remote repository
func (r *Repository) Clone(o *CloneOptions) error {
	empty, err := r.IsEmpty()
	if err != nil {
		return err
	}

	if !empty {
		return ErrRepositoryNonEmpty
	}

	if err := o.Validate(); err != nil {
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

	if err = remote.Connect(); err != nil {
		return err
	}

	defer remote.Disconnect()

	if err := r.updateRemoteConfig(remote, o, c); err != nil {
		return err
	}

	if err = remote.Fetch(&FetchOptions{Depth: o.Depth}); err != nil {
		return err
	}

	head, err := remote.Ref(o.ReferenceName, true)
	if err != nil {
		return err
	}

	return r.createReferences(head)
}

const refspecSingleBranch = "+refs/heads/%s:refs/remotes/%s/%[1]s"

func (r *Repository) updateRemoteConfig(
	remote *Remote, o *CloneOptions, c *config.RemoteConfig,
) error {
	if o.SingleBranch {
		head, err := core.ResolveReference(remote.Info().Refs, o.ReferenceName)
		if err != nil {
			return err
		}

		c.Fetch = []config.RefSpec{
			config.RefSpec(fmt.Sprintf(refspecSingleBranch, head.Name().Short(), c.Name)),
		}

		return r.s.ConfigStorage().SetRemote(c)
	}

	return nil
}

func (r *Repository) createReferences(ref *core.Reference) error {
	if !ref.IsBranch() {
		// detached HEAD mode
		head := core.NewHashReference(core.HEAD, ref.Hash())
		return r.s.ReferenceStorage().Set(head)
	}

	if err := r.s.ReferenceStorage().Set(ref); err != nil {
		return err
	}

	head := core.NewSymbolicReference(core.HEAD, ref.Name())
	return r.s.ReferenceStorage().Set(head)
}

// IsEmpty returns true if the repository is empty
func (r *Repository) IsEmpty() (bool, error) {
	iter, err := r.Refs()
	if err != nil {
		return false, err
	}

	var count int
	return count == 0, iter.ForEach(func(r *core.Reference) error {
		count++
		return nil
	})
}

// Pull incorporates changes from a remote repository into the current branch
func (r *Repository) Pull(o *PullOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	remote, err := r.Remote(o.RemoteName)
	if err != nil {
		return err
	}

	if err = remote.Connect(); err != nil {
		return err
	}

	defer remote.Disconnect()

	head, err := remote.Ref(o.ReferenceName, true)
	if err != nil {
		return err
	}

	if err = remote.Connect(); err != nil {
		return err
	}

	defer remote.Disconnect()

	err = remote.Fetch(&FetchOptions{
		Depth: o.Depth,
	})

	if err != nil {
		return err
	}

	return r.createReferences(head)
}

// Commit return the commit with the given hash
func (r *Repository) Commit(h core.Hash) (*Commit, error) {
	commit, err := r.Object(core.CommitObject, h)
	if err != nil {
		return nil, err
	}

	return commit.(*Commit), nil
}

// Commits decode the objects into commits
func (r *Repository) Commits() (*CommitIter, error) {
	iter, err := r.s.ObjectStorage().Iter(core.CommitObject)
	if err != nil {
		return nil, err
	}

	return NewCommitIter(r, iter), nil
}

// Tree return the tree with the given hash
func (r *Repository) Tree(h core.Hash) (*Tree, error) {
	tree, err := r.Object(core.TreeObject, h)
	if err != nil {
		return nil, err
	}

	return tree.(*Tree), nil
}

// Blob returns the blob with the given hash
func (r *Repository) Blob(h core.Hash) (*Blob, error) {
	blob, err := r.Object(core.BlobObject, h)
	if err != nil {
		return nil, err
	}

	return blob.(*Blob), nil
}

// Tag returns a tag with the given hash.
func (r *Repository) Tag(h core.Hash) (*Tag, error) {
	tag, err := r.Object(core.TagObject, h)
	if err != nil {
		return nil, err
	}

	return tag.(*Tag), nil
}

// Tags returns a TagIter that can step through all of the annotated tags
// in the repository.
func (r *Repository) Tags() (*TagIter, error) {
	iter, err := r.s.ObjectStorage().Iter(core.TagObject)
	if err != nil {
		return nil, err
	}

	return NewTagIter(r, iter), nil
}

// Object returns an object with the given hash.
func (r *Repository) Object(t core.ObjectType, h core.Hash) (Object, error) {
	obj, err := r.s.ObjectStorage().Get(t, h)
	if err != nil {
		if err == core.ErrObjectNotFound {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}

	switch obj.Type() {
	case core.CommitObject:
		commit := &Commit{r: r}
		return commit, commit.Decode(obj)
	case core.TreeObject:
		tree := &Tree{r: r}
		return tree, tree.Decode(obj)
	case core.BlobObject:
		blob := &Blob{}
		return blob, blob.Decode(obj)
	case core.TagObject:
		tag := &Tag{r: r}
		return tag, tag.Decode(obj)
	default:
		return nil, core.ErrInvalidType
	}
}

// Head returns the reference where HEAD is pointing
func (r *Repository) Head() (*core.Reference, error) {
	return core.ResolveReference(r.s.ReferenceStorage(), core.HEAD)
}

// Ref returns the Hash pointing the given refName
func (r *Repository) Ref(name core.ReferenceName, resolved bool) (*core.Reference, error) {
	if resolved {
		return core.ResolveReference(r.s.ReferenceStorage(), name)
	}

	return r.s.ReferenceStorage().Get(name)
}

// Refs returns a map with all the References
func (r *Repository) Refs() (core.ReferenceIter, error) {
	return r.s.ReferenceStorage().Iter()
}
