package merkletrie

import (
	"sort"
	"strings"
)

// A node is a Noder implementation for testing purposes:  It is easier
// to create test trees using nodes than using real git tree objects.
type node struct {
	hash     []byte
	key      string
	children []*node
}

// newNode returns a new Node with the given hash, key and children
// (children can be specified in any order).
func newNode(hash []byte, key string, children []*node) *node {
	sort.Sort(reverseAlphabeticallyByKey(children))

	return &node{
		hash:     hash,
		key:      key,
		children: children,
	}
}

// Hash returns the hash of the node.
func (n *node) Hash() []byte {
	return n.hash
}

// Key returns the key of the node.
func (n *node) Key() string {
	return n.key
}

// NumChildren returns the number of children.
func (n *node) NumChildren() int {
	return len(n.children)
}

// Children returns the node's children in reverse key alphabetical
// order.
func (n *node) Children() []Noder {
	ret := make([]Noder, n.NumChildren())
	for i := range n.children {
		ret[i] = n.children[i]
	}
	return ret
}

type reverseAlphabeticallyByKey []*node

func (a reverseAlphabeticallyByKey) Len() int {
	return len(a)
}

func (a reverseAlphabeticallyByKey) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a reverseAlphabeticallyByKey) Less(i, j int) bool {
	return strings.Compare(a[i].key, a[j].key) > 0
}
