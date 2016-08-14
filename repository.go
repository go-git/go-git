package git

import (
	"errors"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

var (
	// ErrObjectNotFound object not found
	ErrObjectNotFound = errors.New("object not found")
)

const (
	// DefaultRemoteName name of the default Remote, just like git command
	DefaultRemoteName = "origin"
)

// Repository giturl string, auth common.AuthMethod repository struct
type Repository struct {
	Remotes map[string]*Remote

	s core.Storage
}

// NewMemoryRepository creates a new repository, backed by a memory.Storage
func NewMemoryRepository() (*Repository, error) {
	return NewRepository(memory.NewStorage())
}

// NewFilesystemRepository creates a new repository, backed by a filesystem.Storage
func NewFilesystemRepository(fs fs.FS, path string) (*Repository, error) {
	s, err := filesystem.NewStorage(fs, path)
	if err != nil {
		return nil, err
	}

	return NewRepository(s)
}

// NewRepository creates a new repository with the given Storage
func NewRepository(s core.Storage) (*Repository, error) {
	return &Repository{s: s}, nil
}

// Clone clones a remote repository
func (r *Repository) Clone(o *CloneOptions) error {
	o.Default()

	remote, err := r.createDefaultRemote(o.URL, o.Auth)
	if err != nil {
		return err
	}

	if err = remote.Connect(); err != nil {
		return err
	}

	err = remote.Fetch(r.s.ObjectStorage(), &FetchOptions{ReferenceName: o.ReferenceName})
	if err != nil {
		return err
	}

	ref, err := remote.Ref(o.ReferenceName, true)
	if err != nil {
		return err
	}

	return r.createDefaultBranch(ref)
}

func (r *Repository) createDefaultRemote(url string, auth common.AuthMethod) (*Remote, error) {
	remote, err := NewAuthenticatedRemote(url, auth)
	if err != nil {
		return nil, err
	}

	r.Remotes = map[string]*Remote{
		DefaultRemoteName: remote,
	}

	return remote, nil
}

func (r *Repository) createDefaultBranch(ref *core.Reference) error {
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

// Commit return the commit with the given hash
func (r *Repository) Commit(h core.Hash) (*Commit, error) {
	obj, err := r.s.ObjectStorage().Get(h)
	if err != nil {
		if err == core.ErrObjectNotFound {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}

	commit := &Commit{r: r}
	return commit, commit.Decode(obj)
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
	obj, err := r.s.ObjectStorage().Get(h)
	if err != nil {
		if err == core.ErrObjectNotFound {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}

	tree := &Tree{r: r}
	return tree, tree.Decode(obj)
}

// Blob returns the blob with the given hash
func (r *Repository) Blob(h core.Hash) (*Blob, error) {
	obj, err := r.s.ObjectStorage().Get(h)
	if err != nil {
		if err == core.ErrObjectNotFound {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}

	blob := &Blob{}
	return blob, blob.Decode(obj)
}

// Tag returns a tag with the given hash.
func (r *Repository) Tag(h core.Hash) (*Tag, error) {
	obj, err := r.s.ObjectStorage().Get(h)
	if err != nil {
		if err == core.ErrObjectNotFound {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}

	t := &Tag{r: r}
	return t, t.Decode(obj)
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
func (r *Repository) Object(h core.Hash) (Object, error) {
	obj, err := r.s.ObjectStorage().Get(h)
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
func (r *Repository) Refs() core.ReferenceIter {
	i, _ := r.s.ReferenceStorage().Iter()
	return i
}
