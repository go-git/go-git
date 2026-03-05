package gitignore

import (
	"slices"

	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

var _ noder.Noder = (*MatchNoder)(nil)

// MatchNoder is an implementation of [noder.Noder] that only includes nodes based on a pattern.
type MatchNoder struct {
	noder.Noder

	matcher  Matcher
	invert   bool
	path     []string
	children []noder.Noder
}

// IgnoreNoder returns a [MatchNoder] that filters out the given pattern.
func IgnoreNoder(m Matcher, n noder.Noder) *MatchNoder {
	var path []string
	if name := n.Name(); name != "." {
		path = []string{name}
	}

	return &MatchNoder{matcher: m, invert: true, Noder: n, path: path}
}

// Children returns matched children.
// It implements [noder.Noder].
func (n *MatchNoder) Children() ([]noder.Noder, error) {
	if len(n.children) > 0 {
		return n.children, nil
	}

	children, err := n.Noder.Children()
	if err != nil {
		return nil, err
	}

	n.children = n.ignoreChildren(children)

	return n.children, nil
}

func (n *MatchNoder) ignoreChildren(children []noder.Noder) []noder.Noder {
	found := make([]noder.Noder, 0, len(children))
	pathBuf := slices.Grow(n.path, len(n.path)+1)

	for _, child := range children {
		path := append(pathBuf, child.Name())
		if n.match(path, child.IsDir()) {
			continue
		}

		found = append(found, n.newChild(child, path))
	}

	return found
}

func (n *MatchNoder) match(path []string, isDir bool) bool {
	if n.matcher != nil && n.matcher.Match(path, isDir) {
		return n.invert
	}

	return !n.invert
}

func (n *MatchNoder) newChild(child noder.Noder, path []string) noder.Noder {
	if !child.IsDir() {
		return child
	}

	return &MatchNoder{
		matcher: n.matcher,
		invert:  n.invert,
		Noder:   child,
		path:    slices.Clone(path),
	}
}

// NumChildren returns the number of children.
// It implements [noder.Noder].
func (n *MatchNoder) NumChildren() (int, error) {
	children, err := n.Children()
	if err != nil {
		return 0, err
	}

	return len(children), nil
}

// PathIgnored returns true if the given [noder.Path] is ignored.
func (n *MatchNoder) PathIgnored(path noder.Path) bool {
	return n.match(n.noderPaths(path), path.IsDir())
}

// FindPath returns the corresponding [noder.Path] from the tree if there is one.
// It does not apply patterns, allowing retrieval of ignored nodes.
func (n *MatchNoder) FindPath(p noder.Path) (path noder.Path, found bool) {
	node := n.Noder

	for i := range p {
		node, found = n.findChild(node, p[i].Name())
		if !found {
			return nil, false
		}

		path = append(path, node)
	}

	return
}

func (n *MatchNoder) findChild(node noder.Noder, name string) (noder.Noder, bool) {
	children, _ := node.Children()

	for _, child := range children {
		if child.Name() == name {
			return child, true
		}
	}

	return nil, false
}

func (n *MatchNoder) noderPaths(path noder.Path) []string {
	parts := make([]string, len(path))

	for i, p := range path {
		parts[i] = p.Name()
	}

	return parts
}
