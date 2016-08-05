package git

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/src-d/go-git.v3/core"
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
	Name  string
	Files [2]*File
}

func (c *Change) String() string {
	return fmt.Sprintf("<Action: %s, Path: %s>", c.Action, c.Name)
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
	return strings.Compare(c[i].Name, c[j].Name) < 0
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

	iter := tree.Files()
	defer iter.Close()

	for {
		file, err := iter.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("cannot get next file: %s", err)
		}

		var files [2]*File
		if action == Insert {
			files[1] = file
		} else {
			files[0] = file
		}

		changes = append(changes, &Change{
			Action: action,
			Name:   file.Name,
			Files:  files,
		})
	}

	return changes, nil
}

// FIXME: this is very inefficient, but correct.
// The proper way to do this is to implement a diff-tree algorithm,
// while taking advantage of the tree hashes to avoid traversing
// subtrees when the hash is equal in both inputs.
func newDiffTree(a, b *Tree) ([]*Change, error) {
	result := make([]*Change, 0)

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
		switch comp := strings.Compare(aChanges[0].Name, bChanges[0].Name); {
		case comp == 0: // append as "Modify" or ignore if not changed
			modified, err := hasChange(a, b, aChanges[0].Name)
			if err != nil {
				return nil, err
			}

			if modified {
				result = append(result, &Change{
					Action: Modify,
					Name:   aChanges[0].Name,
					Files:  [2]*File{aChanges[0].Files[0], bChanges[0].Files[1]},
				})
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
