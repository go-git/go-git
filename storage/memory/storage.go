// Package memory is a storage backend base on memory
package memory

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ErrUnsupportedObjectType is returned when an unsupported object type is used.
var ErrUnsupportedObjectType = fmt.Errorf("unsupported object type")

// Storage is an implementation of git.Storer that stores data on memory, being
// ephemeral. The use of this storage should be done in controlled environments,
// since the representation in memory of some repository can fill the machine
// memory. in the other hand this storage has the best performance.
type Storage struct {
	ConfigStorage
	ObjectStorage
	ShallowStorage
	IndexStorage
	ReferenceStorage
	ModuleStorage
	options options
}

// NewStorage returns a new in memory Storage base.
func NewStorage(o ...StorageOption) *Storage {
	opts := newOptions()
	for _, opt := range o {
		opt(&opts)
	}

	s := &Storage{
		options:          opts,
		ReferenceStorage: make(ReferenceStorage),
		ConfigStorage:    ConfigStorage{},
		ShallowStorage:   ShallowStorage{},
		ObjectStorage: ObjectStorage{
			Objects: make(map[plumbing.Hash]plumbing.EncodedObject),
			Commits: make(map[plumbing.Hash]plumbing.EncodedObject),
			Trees:   make(map[plumbing.Hash]plumbing.EncodedObject),
			Blobs:   make(map[plumbing.Hash]plumbing.EncodedObject),
			Tags:    make(map[plumbing.Hash]plumbing.EncodedObject),
		},
		ModuleStorage: make(ModuleStorage),
	}

	if opts.objectFormat != formatcfg.UnsetObjectFormat {
		cfg, _ := s.Config()
		cfg.Extensions.ObjectFormat = opts.objectFormat
		cfg.Core.RepositoryFormatVersion = formatcfg.Version1
		s.oh = plumbing.FromObjectFormat(opts.objectFormat)
	} else {
		s.oh = plumbing.FromObjectFormat(formatcfg.DefaultObjectFormat)
	}

	return s
}

func (s *Storage) ObjectFormat() formatcfg.ObjectFormat {
	cfg, _ := s.Config()

	return cfg.Extensions.ObjectFormat
}

func (s *Storage) SetObjectFormat(of formatcfg.ObjectFormat) error {
	switch of {
	case formatcfg.SHA1, formatcfg.SHA256:
	default:
		return fmt.Errorf("invalid object format: %s", of)
	}

	// Presently, storage only supports a single object format at a
	// time. Changing the format of an existing (and populated) object
	// storage is yet to be supported.
	if len(s.Blobs) > 0 ||
		len(s.Commits) > 0 ||
		len(s.Objects) > 0 ||
		len(s.Tags) > 0 ||
		len(s.Trees) > 0 {
		return errors.New("cannot change object format of existing object storage")
	}

	if s.options.objectFormat == of {
		return nil
	}

	cfg, _ := s.Config()
	cfg.Extensions.ObjectFormat = of
	cfg.Core.RepositoryFormatVersion = formatcfg.Version1
	s.options.objectFormat = of
	s.oh = plumbing.FromObjectFormat(of)
	return nil
}

// SupportsExtension checks whether the Storer supports the given
// Git extension defined by name.
func (s *Storage) SupportsExtension(name, value string) bool {
	if name != "objectformat" {
		return false
	}

	switch value {
	case "sha1", "sha256", "":
		return true
	default:
		return false
	}
}

// ConfigStorage implements config.ConfigStorer for in-memory storage.
type ConfigStorage struct {
	config *config.Config
}

// SetConfig stores the given config.
func (c *ConfigStorage) SetConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	c.config = cfg
	return nil
}

// Config returns the stored config.
func (c *ConfigStorage) Config() (*config.Config, error) {
	if c.config == nil {
		c.config = config.NewConfig()
	}

	return c.config, nil
}

// IndexStorage implements storer.IndexStorer for in-memory storage.
type IndexStorage struct {
	index *index.Index
}

// SetIndex stores the given index.
// Note: this method sets idx.ModTime to simulate filesystem storage behavior.
func (c *IndexStorage) SetIndex(idx *index.Index) error {
	// Set ModTime to enable racy git detection in the metadata optimization.
	idx.ModTime = time.Now()
	c.index = idx
	return nil
}

// Index returns the stored index.
func (c *IndexStorage) Index() (*index.Index, error) {
	if c.index == nil {
		c.index = &index.Index{Version: 2}
	}

	return c.index, nil
}

// ObjectStorage implements storer.EncodedObjectStorer for in-memory storage.
type ObjectStorage struct {
	oh      *plumbing.ObjectHasher
	Objects map[plumbing.Hash]plumbing.EncodedObject
	Commits map[plumbing.Hash]plumbing.EncodedObject
	Trees   map[plumbing.Hash]plumbing.EncodedObject
	Blobs   map[plumbing.Hash]plumbing.EncodedObject
	Tags    map[plumbing.Hash]plumbing.EncodedObject
}

type lazyCloser struct {
	storage *ObjectStorage
	obj     plumbing.EncodedObject
	closer  io.Closer
}

func (c *lazyCloser) Close() error {
	err := c.closer.Close()
	if err != nil {
		return fmt.Errorf("failed to close memory encoded object: %w", err)
	}

	_, err = c.storage.SetEncodedObject(c.obj)
	return err
}

// RawObjectWriter returns a writer for writing a raw object.
func (o *ObjectStorage) RawObjectWriter(typ plumbing.ObjectType, sz int64) (w io.WriteCloser, err error) {
	obj := o.NewEncodedObject()
	obj.SetType(typ)
	obj.SetSize(sz)

	w, err = obj.Writer()
	if err != nil {
		return nil, err
	}

	wc := ioutil.NewWriteCloser(w,
		&lazyCloser{storage: o, obj: obj, closer: w},
	)

	return wc, nil
}

// NewEncodedObject returns a new EncodedObject.
func (o *ObjectStorage) NewEncodedObject() plumbing.EncodedObject {
	return plumbing.NewMemoryObject(o.oh)
}

// SetEncodedObject stores the given EncodedObject.
func (o *ObjectStorage) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	h := obj.Hash()
	o.Objects[h] = obj

	switch obj.Type() {
	case plumbing.CommitObject:
		o.Commits[h] = o.Objects[h]
	case plumbing.TreeObject:
		o.Trees[h] = o.Objects[h]
	case plumbing.BlobObject:
		o.Blobs[h] = o.Objects[h]
	case plumbing.TagObject:
		o.Tags[h] = o.Objects[h]
	default:
		return h, ErrUnsupportedObjectType
	}

	return h, nil
}

// HasEncodedObject returns nil if the object exists, or an error otherwise.
func (o *ObjectStorage) HasEncodedObject(h plumbing.Hash) (err error) {
	if _, ok := o.Objects[h]; !ok {
		return plumbing.ErrObjectNotFound
	}
	return nil
}

// EncodedObjectSize returns the size of the object with the given hash.
func (o *ObjectStorage) EncodedObjectSize(h plumbing.Hash) (
	size int64, err error,
) {
	obj, ok := o.Objects[h]
	if !ok {
		return 0, plumbing.ErrObjectNotFound
	}

	return obj.Size(), nil
}

// EncodedObject returns the object with the given type and hash.
func (o *ObjectStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, ok := o.Objects[h]
	if !ok || (plumbing.AnyObject != t && obj.Type() != t) {
		return nil, plumbing.ErrObjectNotFound
	}

	return obj, nil
}

// IterEncodedObjects returns an iterator for all objects of the given type.
func (o *ObjectStorage) IterEncodedObjects(t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	var series []plumbing.EncodedObject
	switch t {
	case plumbing.AnyObject:
		series = flattenObjectMap(o.Objects)
	case plumbing.CommitObject:
		series = flattenObjectMap(o.Commits)
	case plumbing.TreeObject:
		series = flattenObjectMap(o.Trees)
	case plumbing.BlobObject:
		series = flattenObjectMap(o.Blobs)
	case plumbing.TagObject:
		series = flattenObjectMap(o.Tags)
	}

	return storer.NewEncodedObjectSliceIter(series), nil
}

func flattenObjectMap(m map[plumbing.Hash]plumbing.EncodedObject) []plumbing.EncodedObject {
	objects := make([]plumbing.EncodedObject, 0, len(m))
	for _, obj := range m {
		objects = append(objects, obj)
	}
	return objects
}

// Begin returns a new transaction.
func (o *ObjectStorage) Begin() storer.Transaction {
	return &TxObjectStorage{
		Storage: o,
		Objects: make(map[plumbing.Hash]plumbing.EncodedObject),
	}
}

// ForEachObjectHash calls the given function for each object hash.
func (o *ObjectStorage) ForEachObjectHash(fun func(plumbing.Hash) error) error {
	for h := range o.Objects {
		err := fun(h)
		if err != nil {
			if errors.Is(err, storer.ErrStop) {
				return nil
			}
			return err
		}
	}
	return nil
}

// ObjectPacks returns the list of object packs (always empty for in-memory storage).
func (o *ObjectStorage) ObjectPacks() ([]plumbing.Hash, error) {
	return nil, nil
}

// DeleteOldObjectPackAndIndex is a no-op for in-memory storage.
func (o *ObjectStorage) DeleteOldObjectPackAndIndex(plumbing.Hash, time.Time) error {
	return nil
}

var errNotSupported = fmt.Errorf("not supported")

// LooseObjectTime returns an error as loose objects are not supported.
func (o *ObjectStorage) LooseObjectTime(_ plumbing.Hash) (time.Time, error) {
	return time.Time{}, errNotSupported
}

// DeleteLooseObject returns an error as loose objects are not supported.
func (o *ObjectStorage) DeleteLooseObject(plumbing.Hash) error {
	return errNotSupported
}

// AddAlternate returns an error as alternates are not supported.
func (o *ObjectStorage) AddAlternate(_ string) error {
	return errNotSupported
}

// TxObjectStorage implements storer.Transaction for in-memory storage.
type TxObjectStorage struct {
	Storage *ObjectStorage
	Objects map[plumbing.Hash]plumbing.EncodedObject
}

// SetEncodedObject stores the given EncodedObject in the transaction.
func (tx *TxObjectStorage) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	h := obj.Hash()
	tx.Objects[h] = obj

	return h, nil
}

// EncodedObject returns the object with the given type and hash from the transaction.
func (tx *TxObjectStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, ok := tx.Objects[h]
	if !ok || (plumbing.AnyObject != t && obj.Type() != t) {
		return nil, plumbing.ErrObjectNotFound
	}

	return obj, nil
}

// Commit commits all objects in the transaction to the storage.
func (tx *TxObjectStorage) Commit() error {
	for h, obj := range tx.Objects {
		delete(tx.Objects, h)
		if _, err := tx.Storage.SetEncodedObject(obj); err != nil {
			return err
		}
	}

	return nil
}

// Rollback discards all objects in the transaction.
func (tx *TxObjectStorage) Rollback() error {
	clear(tx.Objects)
	return nil
}

// ReferenceStorage implements storer.ReferenceStorer for in-memory storage.
type ReferenceStorage map[plumbing.ReferenceName]*plumbing.Reference

// SetReference stores the given reference.
func (r ReferenceStorage) SetReference(ref *plumbing.Reference) error {
	if ref != nil {
		r[ref.Name()] = ref
	}

	return nil
}

// CheckAndSetReference stores the reference if the old reference matches.
func (r ReferenceStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	if ref == nil {
		return nil
	}

	if old != nil {
		tmp := r[ref.Name()]
		if tmp != nil && tmp.Hash() != old.Hash() {
			return storage.ErrReferenceHasChanged
		}
	}
	r[ref.Name()] = ref
	return nil
}

// Reference returns the reference with the given name.
func (r ReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	ref, ok := r[n]
	if !ok {
		return nil, plumbing.ErrReferenceNotFound
	}

	return ref, nil
}

// IterReferences returns an iterator for all references.
func (r ReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	refs := make([]*plumbing.Reference, 0, len(r))
	for _, ref := range r {
		refs = append(refs, ref)
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// CountLooseRefs returns the number of references.
func (r ReferenceStorage) CountLooseRefs() (int, error) {
	return len(r), nil
}

// PackRefs is a no-op.
func (r ReferenceStorage) PackRefs() error {
	return nil
}

// RemoveReference removes the reference with the given name.
func (r ReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	delete(r, n)
	return nil
}

// ShallowStorage implements storer.ShallowStorer for in-memory storage.
type ShallowStorage []plumbing.Hash

// SetShallow stores the shallow commits.
func (s *ShallowStorage) SetShallow(commits []plumbing.Hash) error {
	*s = commits
	return nil
}

// Shallow returns the shallow commits.
func (s ShallowStorage) Shallow() ([]plumbing.Hash, error) {
	return s, nil
}

// ModuleStorage implements storer.ModuleStorer for in-memory storage.
type ModuleStorage map[string]*Storage

// Module returns the storage for the given submodule.
func (s ModuleStorage) Module(name string) (storage.Storer, error) {
	if m, ok := s[name]; ok {
		return m, nil
	}

	m := NewStorage()
	s[name] = m

	return m, nil
}
