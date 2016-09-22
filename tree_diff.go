package git

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
)

type Action int

func (a Action) String() string {
	switch a {
	case Insert:
		return "Insert"
	case Delete:
		return "Delete"
	case Modify:
		return "Modify"
	default:
		panic(fmt.Sprintf("unsupported action: %d", a))
	}
}

const (
	Insert Action = iota
	Delete
	Modify
)

type Change struct {
	Action
	From ChangeEntry
	To   ChangeEntry
}

type ChangeEntry struct {
	Name      string
	Tree      *Tree
	TreeEntry TreeEntry
}

func (c *Change) Files() (from *File, to *File, err error) {
	if c.Action == Insert || c.Action == Modify {
		to, err = newFileFromTreeEntry(c.To.Tree, &c.To.TreeEntry)
		if err != nil {
			return
		}

	}

	if c.Action == Delete || c.Action == Modify {
		from, err = newFileFromTreeEntry(c.From.Tree, &c.From.TreeEntry)
		if err != nil {
			return
		}
	}

	return
}

func newFileFromTreeEntry(t *Tree, e *TreeEntry) (*File, error) {
	blob, err := t.r.Blob(e.Hash)
	if err != nil {
		return nil, err
	}

	return NewFile(e.Name, e.Mode, blob), nil
}

func (c *Change) String() string {
	return fmt.Sprintf("<Action: %s, Path: %s>", c.Action, c.name())
}

func (c *Change) name() string {
	if c.From.Name != "" {
		return c.From.Name
	}

	return c.To.Name
}

type Changes []*Change

func newEmpty() Changes {
	return make([]*Change, 0, 0)
}

func DiffTree(a, b *Tree) ([]*Change, error) {
	if a == b {
		return newEmpty(), nil
	}

	if a == nil || b == nil {
		return newWithEmpty(a, b)
	}

	return newDiffTree(a, b)
}

func (c Changes) Len() int {
	return len(c)
}

func (c Changes) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

func (c Changes) Less(i, j int) bool {
	return strings.Compare(c[i].name(), c[j].name()) < 0
}

func (c Changes) String() string {
	var buffer bytes.Buffer
	buffer.WriteString("[")
	comma := ""
	for _, v := range c {
		buffer.WriteString(comma)
		buffer.WriteString(v.String())
		comma = ", "
	}
	buffer.WriteString("]")

	return buffer.String()
}

func newWithEmpty(a, b *Tree) (Changes, error) {
	changes := newEmpty()

	var action Action
	var tree *Tree
	if a == nil {
		action = Insert
		tree = b
	} else {
		action = Delete
		tree = a
	}

	w := NewTreeIter(tree.r, tree, true)
	defer w.Close()

	for {
		path, entry, err := w.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("cannot get next file: %s", err)
		}

		if entry.Mode.IsDir() {
			continue
		}

		c := &Change{Action: action}

		if action == Insert {
			c.To.Name = path
			c.To.TreeEntry = entry
			c.To.Tree = tree
		} else {
			c.From.Name = path
			c.From.TreeEntry = entry
			c.From.Tree = tree
		}

		changes = append(changes, c)
	}

	return changes, nil
}

// FIXME: this is very inefficient, but correct.
// The proper way to do this is to implement a diff-tree algorithm,
// while taking advantage of the tree hashes to avoid traversing
// subtrees when the hash is equal in both inputs.
func newDiffTree(a, b *Tree) ([]*Change, error) {
	var result []*Change

	aChanges, err := newWithEmpty(a, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create nil-diff of source tree: %s", err)
	}
	sort.Sort(aChanges)

	bChanges, err := newWithEmpty(nil, b)
	if err != nil {
		return nil, fmt.Errorf("cannot create nil-diff of destination tree: %s", err)
	}
	sort.Sort(bChanges)

	for len(aChanges) > 0 && len(bChanges) > 0 {
		switch comp := strings.Compare(aChanges[0].name(), bChanges[0].name()); {
		case comp == 0: // append as "Modify" or ignore if not changed
			modified, err := hasChange(a, b, aChanges[0].name())
			if err != nil {
				return nil, err
			}

			if modified {
				c := mergeInsertAndDeleteIntoModify(aChanges[0], bChanges[0])
				result = append(result, c)
			}

			aChanges = aChanges[1:]
			bChanges = bChanges[1:]
		case comp < 0: // delete first a change
			result = append(result, aChanges[0])
			aChanges = aChanges[1:]
		case comp > 0: // insert first b change
			result = append(result, bChanges[0])
			bChanges = bChanges[1:]
		}
	}

	// append all remaining changes in aChanges, if any, as deletes
	// append all remaining changes in bChanges, if any, as inserts
	result = append(result, aChanges...)
	result = append(result, bChanges...)

	return result, nil
}

func mergeInsertAndDeleteIntoModify(a, b *Change) *Change {
	c := &Change{Action: Modify}
	c.From.Name = a.From.Name
	c.From.Tree = a.From.Tree
	c.From.TreeEntry = a.From.TreeEntry
	c.To.Name = b.To.Name
	c.To.Tree = b.To.Tree
	c.To.TreeEntry = b.To.TreeEntry

	return c
}

func hasChange(a, b *Tree, path string) (bool, error) {
	ha, err := hash(a, path)
	if err != nil {
		return false, err
	}

	hb, err := hash(b, path)
	if err != nil {
		return false, err
	}

	return ha != hb, nil
}

func hash(tree *Tree, path string) (core.Hash, error) {
	file, err := tree.File(path)
	if err != nil {
		var empty core.Hash
		return empty, fmt.Errorf("cannot find file %s in tree: %s", path, err)
	}

	return file.Hash, nil
}
