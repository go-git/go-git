package core

import "errors"

//ErrStop is used to stop a ForEach function in an Iter
var ErrStop = errors.New("stop iter")

// ObjectStorage generic storage of objects
type ObjectStorage interface {
	NewObject() Object
	Set(Object) (Hash, error)
	// Get an object by hash with the given ObjectType.
	//
	// Implementors should return (nil, core.ErrObjectNotFound) if an object
	// doesn't exist with both the given hash and object type.
	//
	// Valid ObjectType values are CommitObject, BlobObject, TagObject, TreeObject
	// and AnyObject.
	//
	// If AnyObject is given, the object must be looked up regardless of its type.
	Get(Hash, ObjectType) (Object, error)
	Iter(ObjectType) (ObjectIter, error)
}

// ObjectIter is a generic closable interface for iterating over objects.
type ObjectIter interface {
	Next() (Object, error)
	ForEach(func(Object) error) error
	Close()
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
