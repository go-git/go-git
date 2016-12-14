package merkletrie

import (
	"bytes"
	"fmt"
)

const sep = "/"

// A frame represents siblings in a trie, along with the path to get to
// them.  For example the frame for the node with key `b` in this trie:
//
//                    a
//                   / \
//                  /   \
//                 /     \
//                b       c
//               /|\     / \
//              y z x   d   e
//                |
//                g
//
// would be:
//
//     f := frame{
//         base: "a/b",           // path to the siblings
//         stack: []Node{z, y, x} // in reverse alphabetical order
//     }
type frame struct {
	base  string  // absolute key of their parents
	stack []Noder // siblings, sorted in reverse alphabetical order by key
}

// newFrame returns a frame for the children of a node n.
func newFrame(parentAbsoluteKey string, n Noder) *frame {
	return &frame{
		base:  parentAbsoluteKey + sep + n.Key(),
		stack: n.Children(),
	}
}

func (f *frame) String() string {
	var buf bytes.Buffer
	_, _ = buf.WriteString(fmt.Sprintf("base=%q, stack=[", f.base))

	sep := ""
	for _, e := range f.stack {
		_, _ = buf.WriteString(sep)
		sep = ", "
		_, _ = buf.WriteString(fmt.Sprintf("%q", e.Key()))
	}

	_ = buf.WriteByte(']')

	return buf.String()
}

func (f *frame) top() (Noder, bool) {
	if len(f.stack) == 0 {
		return nil, false
	}

	top := len(f.stack) - 1

	return f.stack[top], true
}

func (f *frame) pop() (Noder, bool) {
	if len(f.stack) == 0 {
		return nil, false
	}

	top := len(f.stack) - 1
	ret := f.stack[top]
	f.stack[top] = nil
	f.stack = f.stack[:top]

	return ret, true
}
