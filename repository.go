package git

import (
	"errors"
	"fmt"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	osfs "gopkg.in/src-d/go-git.v4/utils/fs/os"
)

var (
	ErrObjectNotFound     = errors.New("object not found")
	ErrInvalidReference   = errors.New("invalid reference, should be a tag or a branch")
	ErrRepositoryNonEmpty = errors.New("repository non empty")
	ErrRemoteNotFound     = errors.New("remote not found")
	ErrRemoteExists       = errors.New("remote already exists")
)

// Repository giturl string, auth common.AuthMethod repository struct
type Repository struct {
	r map[string]*Remote
	s Storer
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
	s, err := filesystem.NewStorage(osfs.New(path))
	if err != nil {
		return nil, err
	}

	return NewRepository(s)
}

// NewRepository creates a new repository with the given Storage
func NewRepository(s Storer) (*Repository, error) {
	return &Repository{
		s: s,
		r: make(map[string]*Remote, 0),
	}, nil
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
	if !o.SingleBranch {
		return nil
	}

	head, err := core.ResolveReference(remote.Info().Refs, o.ReferenceName)
	if err != nil {
		return err
	}

	c.Fetch = []config.RefSpec{
		config.RefSpec(fmt.Sprintf(refspecSingleBranch, head.Name().Short(), c.Name)),
	}

	cfg, err := r.s.Config()
	if err != nil {
		return err
	}

	cfg.Remotes[c.Name] = c
	return r.s.SetConfig(cfg)

}

func (r *Repository) createReferences(ref *core.Reference) error {
	if !ref.IsBranch() {
		// detached HEAD mode
		head := core.NewHashReference(core.HEAD, ref.Hash())
		return r.s.SetReference(head)
	}

	if err := r.s.SetReference(ref); err != nil {
		return err
	}

	head := core.NewSymbolicReference(core.HEAD, ref.Name())
	return r.s.SetReference(head)
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
	iter, err := r.s.IterObjects(core.CommitObject)
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

// Trees decodes the objects into trees
func (r *Repository) Trees() (*TreeIter, error) {
	iter, err := r.s.IterObjects(core.TreeObject)
	if err != nil {
		return nil, err
	}

	return NewTreeIter(r, iter), nil
}

// Blob returns the blob with the given hash
func (r *Repository) Blob(h core.Hash) (*Blob, error) {
	blob, err := r.Object(core.BlobObject, h)
	if err != nil {
		return nil, err
	}

	return blob.(*Blob), nil
}

// Blobs decodes the objects into blobs
func (r *Repository) Blobs() (*BlobIter, error) {
	iter, err := r.s.IterObjects(core.BlobObject)
	if err != nil {
		return nil, err
	}

	return NewBlobIter(r, iter), nil
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
	iter, err := r.s.IterObjects(core.TagObject)
	if err != nil {
		return nil, err
	}

	return NewTagIter(r, iter), nil
}

// Object returns an object with the given hash.
func (r *Repository) Object(t core.ObjectType, h core.Hash) (Object, error) {
	obj, err := r.s.Object(t, h)
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

// Objects returns an ObjectIter that can step through all of the annotated tags
// in the repository.
func (r *Repository) Objects() (*ObjectIter, error) {
	iter, err := r.s.IterObjects(core.AnyObject)
	if err != nil {
		return nil, err
	}

	return NewObjectIter(r, iter), nil
}

// Head returns the reference where HEAD is pointing
func (r *Repository) Head() (*core.Reference, error) {
	return core.ResolveReference(r.s, core.HEAD)
}

// Ref returns the Hash pointing the given refName
func (r *Repository) Ref(name core.ReferenceName, resolved bool) (*core.Reference, error) {
	if resolved {
		return core.ResolveReference(r.s, name)
	}

	return r.s.Reference(name)
}

// Refs returns a map with all the References
func (r *Repository) Refs() (core.ReferenceIter, error) {
	return r.s.IterReferences()
}
