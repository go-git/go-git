// Package memory is a storage backend base on memory
package memory

import (
	"fmt"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

var ErrUnsupportedObjectType = fmt.Errorf("unsupported object type")

// Storage is an implementation of git.Storer that stores data on memory, being
// ephemeral. The use of this storage should be done in controlled envoriments,
// since the representation in memory of some repository can fill the machine
// memory. in the other hand this storage has the best performance.
type Storage struct {
	ConfigStorage
	ObjectStorage
	ReferenceStorage
}

// NewStorage returns a new Storage base on memory
func NewStorage() *Storage {
	return &Storage{
		ReferenceStorage: make(ReferenceStorage, 0),
		ConfigStorage:    ConfigStorage{},
		ObjectStorage: ObjectStorage{
			Objects: make(map[plumbing.Hash]plumbing.Object, 0),
			Commits: make(map[plumbing.Hash]plumbing.Object, 0),
			Trees:   make(map[plumbing.Hash]plumbing.Object, 0),
			Blobs:   make(map[plumbing.Hash]plumbing.Object, 0),
			Tags:    make(map[plumbing.Hash]plumbing.Object, 0),
		},
	}
}

type ConfigStorage struct {
	config *config.Config
}

func (c *ConfigStorage) SetConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	c.config = cfg
	return nil
}

func (c *ConfigStorage) Config() (*config.Config, error) {
	if c.config == nil {
		c.config = config.NewConfig()
	}

	return c.config, nil
}

type ObjectStorage struct {
	Objects map[plumbing.Hash]plumbing.Object
	Commits map[plumbing.Hash]plumbing.Object
	Trees   map[plumbing.Hash]plumbing.Object
	Blobs   map[plumbing.Hash]plumbing.Object
	Tags    map[plumbing.Hash]plumbing.Object
}

func (o *ObjectStorage) NewObject() plumbing.Object {
	return &plumbing.MemoryObject{}
}

func (o *ObjectStorage) SetObject(obj plumbing.Object) (plumbing.Hash, error) {
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

func (o *ObjectStorage) Object(t plumbing.ObjectType, h plumbing.Hash) (plumbing.Object, error) {
	obj, ok := o.Objects[h]
	if !ok || (plumbing.AnyObject != t && obj.Type() != t) {
		return nil, plumbing.ErrObjectNotFound
	}

	return obj, nil
}

func (o *ObjectStorage) IterObjects(t plumbing.ObjectType) (storer.ObjectIter, error) {
	var series []plumbing.Object
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

	return storer.NewObjectSliceIter(series), nil
}

func flattenObjectMap(m map[plumbing.Hash]plumbing.Object) []plumbing.Object {
	objects := make([]plumbing.Object, 0, len(m))
	for _, obj := range m {
		objects = append(objects, obj)
	}
	return objects
}

func (o *ObjectStorage) Begin() storer.Transaction {
	return &TxObjectStorage{
		Storage: o,
		Objects: make(map[plumbing.Hash]plumbing.Object, 0),
	}
}

type TxObjectStorage struct {
	Storage *ObjectStorage
	Objects map[plumbing.Hash]plumbing.Object
}

func (tx *TxObjectStorage) SetObject(obj plumbing.Object) (plumbing.Hash, error) {
	h := obj.Hash()
	tx.Objects[h] = obj

	return h, nil
}

func (tx *TxObjectStorage) Object(t plumbing.ObjectType, h plumbing.Hash) (plumbing.Object, error) {
	obj, ok := tx.Objects[h]
	if !ok || (plumbing.AnyObject != t && obj.Type() != t) {
		return nil, plumbing.ErrObjectNotFound
	}

	return obj, nil
}

func (tx *TxObjectStorage) Commit() error {
	for h, obj := range tx.Objects {
		delete(tx.Objects, h)
		if _, err := tx.Storage.SetObject(obj); err != nil {
			return err
		}
	}

	return nil
}

func (tx *TxObjectStorage) Rollback() error {
	tx.Objects = make(map[plumbing.Hash]plumbing.Object, 0)
	return nil
}

type ReferenceStorage map[plumbing.ReferenceName]*plumbing.Reference

func (r ReferenceStorage) SetReference(ref *plumbing.Reference) error {
	if ref != nil {
		r[ref.Name()] = ref
	}

	return nil
}

func (r ReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	ref, ok := r[n]
	if !ok {
		return nil, plumbing.ErrReferenceNotFound
	}

	return ref, nil
}

func (r ReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	var refs []*plumbing.Reference
	for _, ref := range r {
		refs = append(refs, ref)
	}

	return storer.NewReferenceSliceIter(refs), nil
}
