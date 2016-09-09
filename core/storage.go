package core

import (
	"errors"
	"io"
)

var (
	//ErrStop is used to stop a ForEach function in an Iter
	ErrStop           = errors.New("stop iter")
	ErrNotImplemented = errors.New("method not-implemented")
)

// ObjectStorage generic storage of objects
type ObjectStorage interface {
	// NewObject returns a new Object, the real type of the object can be a
	// custom implementation or the defaul one, MemoryObject
	NewObject() Object
	// Set save an object into the storage, the object shuld be create with
	// the NewObject, method, and file if the type is not supported.
	Set(Object) (Hash, error)
	// Get an object by hash with the given ObjectType. Implementors should
	// return (nil, ErrObjectNotFound) if an object doesn't exist with both the
	// given hash and object type.
	//
	// Valid ObjectType values are CommitObject, BlobObject, TagObject,
	// TreeObject and AnyObject.
	//
	// If AnyObject is given, the object must be looked up regardless of its type.
	Get(ObjectType, Hash) (Object, error)
	// Iter returns a custom ObjectIter over all the object on the storage.
	//
	// Valid ObjectType values are CommitObject, BlobObject, TagObject,
	Iter(ObjectType) (ObjectIter, error)
	// Begin starts a transaction.
	Begin() TxObjectStorage
}

// ObjectStorageWrite is a optional method for ObjectStorage, it enable direct
// write of packfile to the storage
type ObjectStorageWrite interface {
	// Writer retuns a writer for writing a packfile to the Storage, this method
	// is optional, if not implemented the ObjectStorage should return a
	// ErrNotImplemented error.
	//
	// If the implementation not implements Writer the objects should be written
	// using the Set method.
	Writer() (io.WriteCloser, error)
}

// ObjectIter is a generic closable interface for iterating over objects.
type ObjectIter interface {
	Next() (Object, error)
	ForEach(func(Object) error) error
	Close()
}

// TxObjectStorage is an in-progress storage transaction.
// A transaction must end with a call to Commit or Rollback.
type TxObjectStorage interface {
	Set(Object) (Hash, error)
	Get(ObjectType, Hash) (Object, error)
	Commit() error
	Rollback() error
}

// ReferenceStorage generic storage of references
type ReferenceStorage interface {
	Set(*Reference) error
	Get(ReferenceName) (*Reference, error)
	Iter() (ReferenceIter, error)
}

// ReferenceIter is a generic closable interface for iterating over references
type ReferenceIter interface {
	Next() (*Reference, error)
	ForEach(func(*Reference) error) error
	Close()
}
