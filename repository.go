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
	ErrObjectNotFound = errors.New("object not found")
	ErrUnknownRemote  = errors.New("unknown remote")
)

// Repository giturl string, auth common.AuthMethod repository struct
type Repository struct {
	Remotes map[string]*Remote
	s       core.Storage
}

// NewMemoryRepository creates a new repository, backed by a memory.Storage
func NewMemoryRepository() (*Repository, error) {
	return NewRepository(memory.NewStorage())
}

// NewFilesystemRepository creates a new repository, backed by a filesystem.Storage
// based on a fs.OS, if you want to use a custom one you need to use the function
// NewRepository and build you filesystem.Storage
func NewFilesystemRepository(path string) (*Repository, error) {
	s, err := filesystem.NewStorage(fs.NewOS(), path)
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
func (r *Repository) Clone(o *RepositoryCloneOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	remote, err := r.createRemote(o.RemoteName, o.URL, o.Auth)
	if err != nil {
		return err
	}

	if err = remote.Connect(); err != nil {
		return err
	}

	var single core.ReferenceName
	if o.SingleBranch {
		single = o.ReferenceName
	}

	head, err := remote.Ref(o.ReferenceName, true)
	if err != nil {
		return err
	}

	refs, err := r.getRemoteRefences(remote, single)
	if err != nil {
		return err
	}

	err = remote.Fetch(r.s.ObjectStorage(), &RemoteFetchOptions{
		References: refs,
		Depth:      o.Depth,
	})

	if err != nil {
		return err
	}

	if err := r.createLocalReferences(head); err != nil {
		return err
	}

	return r.createRemoteReferences(remote, refs)
}

func (r *Repository) createRemote(name, url string, auth common.AuthMethod) (*Remote, error) {
	remote, err := NewAuthenticatedRemote(name, url, auth)
	if err != nil {
		return nil, err
	}

	r.Remotes = map[string]*Remote{name: remote}
	return remote, nil
}

func (r *Repository) getRemoteRefences(
	remote *Remote, single core.ReferenceName,
) ([]*core.Reference, error) {
	if single == "" {
		return r.getAllRemoteRefences(remote)
	}

	ref, err := remote.Ref(single, true)
	if err != nil {
		return nil, err
	}

	refs := []*core.Reference{ref}
	head, err := remote.Ref(core.HEAD, false)
	if err != nil {
		return nil, err
	}

	if head.Target() == ref.Name() {
		refs = append(refs, head)
	}

	return refs, nil
}

func (r *Repository) getAllRemoteRefences(remote *Remote) ([]*core.Reference, error) {
	var refs []*core.Reference
	i := remote.Refs()
	defer i.Close()

	return refs, i.ForEach(func(ref *core.Reference) error {
		if !ref.IsBranch() {
			return nil
		}

		refs = append(refs, ref)
		return nil
	})
}

func (r *Repository) createLocalReferences(ref *core.Reference) error {
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

func (r *Repository) createRemoteReferences(remote *Remote, remoteRefs []*core.Reference) error {
	for _, ref := range remoteRefs {
		if err := r.createRemoteReference(remote, ref); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) createRemoteReference(remote *Remote, ref *core.Reference) error {
	name := ref.Name().AsRemote(remote.Name)

	var n *core.Reference
	switch ref.Type() {
	case core.HashReference:
		n = core.NewHashReference(name, ref.Hash())
	case core.SymbolicReference:
		n = core.NewSymbolicReference(name, ref.Target().AsRemote(remote.Name))
		target, err := remote.Ref(ref.Target(), false)
		if err != nil {
			return err
		}

		if err := r.createRemoteReference(remote, target); err != nil {
			return err
		}
	}

	return r.s.ReferenceStorage().Set(n)
}

// Pull incorporates changes from a remote repository into the current branch
func (r *Repository) Pull(o *RepositoryPullOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}

	remote, ok := r.Remotes[o.RemoteName]
	if !ok {
		return ErrUnknownRemote
	}

	head, err := remote.Ref(o.ReferenceName, true)
	if err != nil {
		return err
	}

	refs, err := r.getLocalReferences()
	if err != nil {
		return err
	}

	err = remote.Fetch(r.s.ObjectStorage(), &RemoteFetchOptions{
		References:      []*core.Reference{head},
		LocalReferences: refs,
		Depth:           o.Depth,
	})

	if err != nil {
		return err
	}

	return r.createLocalReferences(head)
}

func (r *Repository) getLocalReferences() ([]*core.Reference, error) {
	var refs []*core.Reference
	i := r.Refs()
	defer i.Close()

	return refs, i.ForEach(func(ref *core.Reference) error {
		if ref.Type() == core.SymbolicReference {
			return nil
		}

		refs = append(refs, ref)
		return nil
	})
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
