package merkletrie

// The Noder interface is implemented by the elements of a Merkle Trie.
type Noder interface {
	// Hash returns the hash of the element.
	Hash() []byte
	// Key returns the key of the element.
	Key() string
	// Children returns the children of the element, sorted
	// in reverse key alphabetical order.
	Children() []Noder
	// NumChildren returns the number of children this element has.
	//
	// This method is an optimization: the number of children is easily
	// calculated as the length of the value returned by the Children
	// method (above); yet, some implementations will be able to
	// implement NumChildren in O(1) while Children is usually more
	// complex.
	NumChildren() int
}
