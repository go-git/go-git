package core

// Storage storage of objects and references
type Storage interface {
	ObjectStorage() (ObjectStorage, error)
	ReferenceStorage() (ReferenceStorage, error)
}

// ObjectStorage generic storage of objects
type ObjectStorage interface {
	NewObject() Object
	Set(Object) (Hash, error)
	Get(Hash) (Object, error)
	Iter(ObjectType) (ObjectIter, error)
}

// ObjectIter is a generic closable interface for iterating over objects.
type ObjectIter interface {
	Next() (Object, error)
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
	Close()
}
