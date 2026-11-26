package object

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v6/utils/merkletrie"
)

// Change values represent a detected change between two git trees.  For
// modifications, From is the original status of the node and To is its
// final status.  For insertions, From is the zero value and for
// deletions To is the zero value.
type Change struct {
	From ChangeEntry
	To   ChangeEntry
}

var empty ChangeEntry

// Action returns the kind of action represented by the change, an
// insertion, a deletion or a modification.
func (c *Change) Action() (merkletrie.Action, error) {
	if c.From == empty && c.To == empty {
		return merkletrie.Action(0),
			fmt.Errorf("malformed change: empty from and to")
	}

	if c.From == empty {
		return merkletrie.Insert, nil
	}

	if c.To == empty {
		return merkletrie.Delete, nil
	}

	return merkletrie.Modify, nil
}

// Files returns the files before and after a change.
// For insertions from will be nil. For deletions to will be nil.
func (c *Change) Files() (from, to *File, err error) {
	action, err := c.Action()
	if err != nil {
		return from, to, err
	}

	if action == merkletrie.Insert || action == merkletrie.Modify {
		to, err = c.To.Tree.TreeEntryFile(&c.To.TreeEntry)
		if !c.To.TreeEntry.Mode.IsFile() {
			return nil, nil, nil
		}

		if err != nil {
			return from, to, err
		}
	}

	if action == merkletrie.Delete || action == merkletrie.Modify {
		from, err = c.From.Tree.TreeEntryFile(&c.From.TreeEntry)
		if !c.From.TreeEntry.Mode.IsFile() {
			return nil, nil, nil
		}

		if err != nil {
			return from, to, err
		}
	}

	return from, to, err
}

func (c *Change) String() string {
	action, err := c.Action()
	if err != nil {
		return "malformed change"
	}

	return fmt.Sprintf("<Action: %s, Path: %s>", action, c.name())
}

// Patch returns a Patch with metadata and text differences for the file.
func (c *Change) Patch() (*Patch, error) {
	return c.PatchContext(context.Background())
}

// PatchContext returns a Patch with metadata and text differences for the file.
// If the context expires, a non-nil error is returned.
// Provided context must be non-nil.
func (c *Change) PatchContext(ctx context.Context) (*Patch, error) {
	return c.PatchWithOptions(ctx, nil)
}

// PatchWithOptions creates a patch with the selected text backend.
// A nil options value uses the default backend. The context must be non-nil.
func (c *Change) PatchWithOptions(ctx context.Context, options *PatchOptions) (*Patch, error) {
	return getPatchWithOptions(ctx, "", options, c)
}

func (c *Change) name() string {
	if c.From != empty {
		return c.From.Name
	}

	return c.To.Name
}

// ChangeEntry values represent a node that has suffered a change.
type ChangeEntry struct {
	// Full path of the node using "/" as separator.
	Name string
	// Parent tree of the node that has changed.
	Tree *Tree
	// The entry of the node.
	TreeEntry TreeEntry
}

// Changes represents a collection of changes between two git trees.
// Implements sort.Interface lexicographically over the path of the
// changed files.
type Changes []*Change

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

// Patch returns a Patch with metadata and text differences for the changes.
func (c Changes) Patch() (*Patch, error) {
	return c.PatchContext(context.Background())
}

// PatchContext returns a Patch with metadata and text differences for the changes.
// If the context expires, a non-nil error is returned.
// Provided context must be non-nil.
func (c Changes) PatchContext(ctx context.Context) (*Patch, error) {
	return c.PatchWithOptions(ctx, nil)
}

// PatchWithOptions creates patches with the selected text backend.
// A nil options value uses the default backend. The context must be non-nil.
func (c Changes) PatchWithOptions(ctx context.Context, options *PatchOptions) (*Patch, error) {
	return getPatchWithOptions(ctx, "", options, c...)
}
