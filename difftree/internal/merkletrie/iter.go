package merkletrie

// Iter is a radix tree iterator that will traverse the trie in
// depth-first pre-order.  Entries are traversed in (case-sensitive)
// alphabetical order for each level.
//
// This is the kind of traversal you will expect when listing
// ordinary files and directories recursively, for example:
//
//             Trie           Traversal order
//             ----           ---------------
//              .
//            / | \           a
//           /  |  \          b
//          b   a   z   ===>  b/a
//         / \                b/c
//        c   a               z
//
//
// The Step method will return the next item, the Next method will do
// the same but without descending deeper into the tree (i.e. skipping
// the contents of "directories").
//
// The name of the type and its methods are based on the well known "next"
// and "step" operations, quite common in debuggers, like gdb.
type Iter struct {
	// tells if the iteration has started.
	hasStarted bool
	// Each level of the tree is represented as a frame, this stack
	// keeps track of the frames wrapping the current iterator position.
	// The iterator will "step" into a node by adding its frame to the
	// stack, or go to the next element at the same level by poping the
	// current frame.
	frameStack []*frame
}

// NewIter returns a new iterator for the trie with its root at n.
func NewIter(n Noder) *Iter {
	ret := &Iter{}
	ret.push(newFrame("", n))

	return ret
}

func (iter *Iter) top() (*frame, bool) {
	if len(iter.frameStack) == 0 {
		return nil, false
	}

	top := len(iter.frameStack) - 1

	return iter.frameStack[top], true
}

func (iter *Iter) pop() (*frame, bool) {
	if len(iter.frameStack) == 0 {
		return nil, false
	}

	top := len(iter.frameStack) - 1
	ret := iter.frameStack[top]
	iter.frameStack[top] = nil
	iter.frameStack = iter.frameStack[:top]

	return ret, true
}

func (iter *Iter) push(f *frame) {
	iter.frameStack = append(iter.frameStack, f)
}

const (
	descend     = true
	dontDescend = false
)

// Next returns the next node without descending deeper into the tree
// and true.  If there are no more entries it returns nil and false.
func (iter *Iter) Next() (Noder, bool) {
	return iter.advance(dontDescend)
}

// Step returns the next node in the tree, descending deeper into it if
// needed. If there are no more nodes in the tree, it returns nil and
// false.
func (iter *Iter) Step() (Noder, bool) {
	return iter.advance(descend)
}

// advances the iterator in whatever direction you want: descend or
// dontDescend.
func (iter *Iter) advance(mustDescend bool) (Noder, bool) {
	node, ok := iter.current()
	if !ok {
		return nil, false
	}

	// The first time we just return the current node.
	if !iter.hasStarted {
		iter.hasStarted = true
		return node, ok
	}
	// following advances will involve dropping already seen nodes
	// or getting into their children

	ignoreChildren := node.NumChildren() == 0 || !mustDescend
	if ignoreChildren {
		// if we must ignore the current node children, just drop
		// it and find the next one in the existing frames.
		_ = iter.drop()
		node, ok = iter.current()
		return node, ok
	}

	// if we must descend into the current's node children, drop the
	// parent and add a new frame with its children.
	_ = iter.drop()
	iter.push(newFrame(node.Key(), node))
	node, _ = iter.current()

	return node, true
}

// returns the current frame and the current node (i.e. the ones at the
// top of their respective stacks.
func (iter *Iter) current() (Noder, bool) {
	f, ok := iter.top()
	if !ok {
		return nil, false
	}

	n, ok := f.top()
	if !ok {
		return nil, false
	}

	return n, true
}

// removes the current node and all the frames that become empty as a
// consecuence of this action. It returns true if something was dropped,
// and false if there were no more nodes in the iterator.
func (iter *Iter) drop() bool {
	frame, ok := iter.top()
	if !ok {
		return false
	}

	_, ok = frame.pop()
	if !ok {
		return false
	}

	for { // remove empty frames
		if len(frame.stack) != 0 {
			break
		}

		_, _ = iter.pop()
		frame, ok = iter.top()
		if !ok {
			break
		}
	}

	return true
}
