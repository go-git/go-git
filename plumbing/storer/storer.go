package storer

// Storer is a basic storer for encoded objects and references.
type Storer interface {
	EncodedObjectStorer
	ReferenceStorer
}
