package core

import (
	"errors"
	"io"
)

var (
	//ErrStop is used to stop a ForEach function in an Iter
	ErrStop = errors.New("stop iter")
)

// ObjectStorer generic storage of objects
type ObjectStorer interface {
	// NewObject returns a new Object, the real type of the object can be a
	// custom implementation or the defaul one, MemoryObject
	NewObject() Object
	// SetObject save an object into the storage, the object shuld be create
	// with the NewObject, method, and file if the type is not supported.
	SetObject(Object) (Hash, error)
	// Object an object by hash with the given ObjectType. Implementors
	// should return (nil, ErrObjectNotFound) if an object doesn't exist with
	// both the given hash and object type.
	//
	// Valid ObjectType values are CommitObject, BlobObject, TagObject,
	// TreeObject and AnyObject. If AnyObject is given, the object must be
	// looked up regardless of its type.
	Object(ObjectType, Hash) (Object, error)
	// IterObjects returns a custom ObjectIter over all the object on the
	// storage.
	//
	// Valid ObjectType values are CommitObject, BlobObject, TagObject,
	IterObjects(ObjectType) (ObjectIter, error)
}

// Transactioner is a optional method for ObjectStorer, it enable transaction
// base write and read operations in the storage
type Transactioner interface {
	// Begin starts a transaction.
	Begin() Transaction
}

// PackfileWriter is a optional method for ObjectStorer, it enable direct write
// of packfile to the storage
type PackfileWriter interface {
	// PackfileWriter retuns a writer for writing a packfile to the Storage,
	// this method is optional, if not implemented the ObjectStorer should
	// return a ErrNotImplemented error.
	//
	// If the implementation not implements Writer the objects should be written
	// using the Set method.
	PackfileWriter() (io.WriteCloser, error)
}

// ObjectIter is a generic closable interface for iterating over objects.
type ObjectIter interface {
	Next() (Object, error)
	ForEach(func(Object) error) error
	Close()
}

// Transaction is an in-progress storage transaction. A transaction must end
// with a call to Commit or Rollback.
type Transaction interface {
	SetObject(Object) (Hash, error)
	Object(ObjectType, Hash) (Object, error)
	Commit() error
	Rollback() error
}

// ReferenceStorer generic storage of references
type ReferenceStorer interface {
	SetReference(*Reference) error
	Reference(ReferenceName) (*Reference, error)
	IterReferences() (ReferenceIter, error)
}

// ReferenceIter is a generic closable interface for iterating over references
type ReferenceIter interface {
	Next() (*Reference, error)
	ForEach(func(*Reference) error) error
	Close()
}
