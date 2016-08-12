package git

import (
	"errors"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/memory"
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
	Storage core.Storage

	os core.ObjectStorage
	rs core.ReferenceStorage
}

// NewMemoryRepository creates a new repository, backed by a memory.Storage
func NewMemoryRepository() (*Repository, error) {
	return NewRepository(memory.NewStorage())
}

// NewRepository creates a new repository with the given Storage
func NewRepository(s core.Storage) (*Repository, error) {
	os, err := s.ObjectStorage()
	if err != nil {
		return nil, err
	}

	rs, err := s.ReferenceStorage()
	if err != nil {
		return nil, err
	}

	return &Repository{
		Storage: s,
		os:      os,
		rs:      rs,
	}, nil
}

// Clone clones a remote repository
func (r *Repository) Clone(o *CloneOptions) error {
	remote, err := r.createDefaultRemote(o.URL, o.Auth)
	if err != nil {
		return err
	}

	if err = remote.Connect(); err != nil {
		return err
	}

	h, err := remote.Fetch(r.os, &FetchOptions{
		ReferenceName: core.HEAD,
	})

	if err != nil {
		return err
	}

	return r.rs.Set(core.NewHashReference(core.HEAD, h))
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

// Commit return the commit with the given hash
func (r *Repository) Commit(h core.Hash) (*Commit, error) {
	obj, err := r.os.Get(h)
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
	iter, err := r.os.Iter(core.CommitObject)
	if err != nil {
		return nil, err
	}

	return NewCommitIter(r, iter), nil
}

// Tree return the tree with the given hash
func (r *Repository) Tree(h core.Hash) (*Tree, error) {
	obj, err := r.os.Get(h)
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
	obj, err := r.os.Get(h)
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
	obj, err := r.os.Get(h)
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
	iter, err := r.os.Iter(core.TagObject)
	if err != nil {
		return nil, err
	}

	return NewTagIter(r, iter), nil
}

// Object returns an object with the given hash.
func (r *Repository) Object(h core.Hash) (Object, error) {
	obj, err := r.os.Get(h)
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

// Head returns the hash of the HEAD of the repository or the head of a
// remote, if one is passed.
func (r *Repository) Head(resolved bool) (*core.Reference, error) {
	if resolved {
		return core.ResolveReference(r.rs, core.HEAD)
	}

	return r.rs.Get(core.HEAD)
}
