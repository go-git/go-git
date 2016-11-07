// Package memory is a storage backend base on memory
package memory

import (
	"fmt"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
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
			Objects: make(map[core.Hash]core.Object, 0),
			Commits: make(map[core.Hash]core.Object, 0),
			Trees:   make(map[core.Hash]core.Object, 0),
			Blobs:   make(map[core.Hash]core.Object, 0),
			Tags:    make(map[core.Hash]core.Object, 0),
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
	Objects map[core.Hash]core.Object
	Commits map[core.Hash]core.Object
	Trees   map[core.Hash]core.Object
	Blobs   map[core.Hash]core.Object
	Tags    map[core.Hash]core.Object
}

func (o *ObjectStorage) NewObject() core.Object {
	return &core.MemoryObject{}
}

func (o *ObjectStorage) SetObject(obj core.Object) (core.Hash, error) {
	h := obj.Hash()
	o.Objects[h] = obj

	switch obj.Type() {
	case core.CommitObject:
		o.Commits[h] = o.Objects[h]
	case core.TreeObject:
		o.Trees[h] = o.Objects[h]
	case core.BlobObject:
		o.Blobs[h] = o.Objects[h]
	case core.TagObject:
		o.Tags[h] = o.Objects[h]
	default:
		return h, ErrUnsupportedObjectType
	}

	return h, nil
}

func (o *ObjectStorage) Object(t core.ObjectType, h core.Hash) (core.Object, error) {
	obj, ok := o.Objects[h]
	if !ok || (core.AnyObject != t && obj.Type() != t) {
		return nil, core.ErrObjectNotFound
	}

	return obj, nil
}

func (o *ObjectStorage) IterObjects(t core.ObjectType) (core.ObjectIter, error) {
	var series []core.Object
	switch t {
	case core.AnyObject:
		series = flattenObjectMap(o.Objects)
	case core.CommitObject:
		series = flattenObjectMap(o.Commits)
	case core.TreeObject:
		series = flattenObjectMap(o.Trees)
	case core.BlobObject:
		series = flattenObjectMap(o.Blobs)
	case core.TagObject:
		series = flattenObjectMap(o.Tags)
	}

	return core.NewObjectSliceIter(series), nil
}

func flattenObjectMap(m map[core.Hash]core.Object) []core.Object {
	objects := make([]core.Object, 0, len(m))
	for _, obj := range m {
		objects = append(objects, obj)
	}
	return objects
}

func (o *ObjectStorage) Begin() core.Transaction {
	return &TxObjectStorage{
		Storage: o,
		Objects: make(map[core.Hash]core.Object, 0),
	}
}

type TxObjectStorage struct {
	Storage *ObjectStorage
	Objects map[core.Hash]core.Object
}

func (tx *TxObjectStorage) SetObject(obj core.Object) (core.Hash, error) {
	h := obj.Hash()
	tx.Objects[h] = obj

	return h, nil
}

func (tx *TxObjectStorage) Object(t core.ObjectType, h core.Hash) (core.Object, error) {
	obj, ok := tx.Objects[h]
	if !ok || (core.AnyObject != t && obj.Type() != t) {
		return nil, core.ErrObjectNotFound
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
	tx.Objects = make(map[core.Hash]core.Object, 0)
	return nil
}

type ReferenceStorage map[core.ReferenceName]*core.Reference

func (r ReferenceStorage) SetReference(ref *core.Reference) error {
	if ref != nil {
		r[ref.Name()] = ref
	}

	return nil
}

func (r ReferenceStorage) Reference(n core.ReferenceName) (*core.Reference, error) {
	ref, ok := r[n]
	if !ok {
		return nil, core.ErrReferenceNotFound
	}

	return ref, nil
}

func (r ReferenceStorage) IterReferences() (core.ReferenceIter, error) {
	var refs []*core.Reference
	for _, ref := range r {
		refs = append(refs, ref)
	}

	return core.NewReferenceSliceIter(refs), nil
}
