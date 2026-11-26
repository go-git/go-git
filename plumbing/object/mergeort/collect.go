package mergeort

import (
	"sort"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// collect walks the three trees together registering per-path stages,
// merge-ort's collect_merge_info. Early-resolve rules mirror
// collect_merge_info_callback:
//
//   - all three sides equal (files or trees): resolved on the spot, and
//     subtrees are not descended into (no renames can live under an
//     unchanged tree);
//   - two sides equal and all three present as files: resolved with the
//     differing side.
//
// Tree-vs-tree two-way matches are NOT early-resolved (unlike files):
// rename sources from the base may live inside, so the walk descends.
// Directory/file splits register both the file stages and the directory
// sides, to be reconciled during processing.

func (m *merger) collect(dirPath string, base, ours, theirs *object.Tree) error {
	dn := &dirNode{path: dirPath, nodes: map[string]*node{}}
	m.dirs[dirPath] = dn

	ib := indexTree(base)
	io := indexTree(ours)
	it := indexTree(theirs)

	names := make([]string, 0, len(ib)+len(io)+len(it))
	seen := map[string]struct{}{}
	for _, idx := range []map[string]*object.TreeEntry{ib, io, it} {
		for n := range idx {
			if _, ok := seen[n]; !ok {
				seen[n] = struct{}{}
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)

	for _, name := range names {
		eb := ib[name]
		eo := io[name]
		et := it[name]
		path := joinPath(dirPath, name)

		allMatch := eb != nil && sameEntry(eb, eo) && sameEntry(eb, et)
		allFiles := isFileEntry(eb) && isFileEntry(eo) && isFileEntry(et)

		if allMatch {
			m.register(dn, name, path, func(n *node) {
				n.resolved = true
				n.resV = treeEntryVersion(*eb)
			})
			continue
		}
		if allFiles {
			// Two matching file sides resolve to the third (match_mask
			// 3/5/6 in process_entry, folded into collect like ort's
			// early-resolve branches).
			var res *object.TreeEntry
			switch {
			case sameEntry(eo, et):
				res = eo
			case sameEntry(eb, eo):
				res = et
			case sameEntry(eb, et):
				res = eo
			}
			if res != nil {
				m.register(dn, name, path, func(n *node) {
					n.resolved = true
					n.resV = treeEntryVersion(*res)
				})
				continue
			}
		}

		n := &node{name: name}
		for i, e := range []*object.TreeEntry{eb, eo, et} {
			if e == nil {
				continue
			}
			if e.Mode == filemode.Dir {
				n.dirs[i] = true
				n.dirsHash[i] = e.Hash
			} else {
				n.stages[i] = treeEntryVersion(*e)
			}
		}
		// Collect-time match_mask over present file sides
		// (collect_merge_info_callback); early-resolve handled the
		// 7 case above.
		eq01 := isFileEntry(eb) && isFileEntry(eo) && sameEntry(eb, eo)
		eq02 := isFileEntry(eb) && isFileEntry(et) && sameEntry(eb, et)
		eq12 := isFileEntry(eo) && isFileEntry(et) && sameEntry(eo, et)
		switch {
		case eq01 && eq02:
			n.matchMask = 7
		case eq01:
			n.matchMask = 3
		case eq02:
			n.matchMask = 5
		case eq12:
			n.matchMask = 6
		}
		dn.nodes[name] = n
		m.byPath[path] = n

		if n.dirs[0] || n.dirs[1] || n.dirs[2] {
			subBase, err := m.subTree(n.dirs[0], n.dirsHash[0])
			if err != nil {
				return err
			}
			subOurs, err := m.subTree(n.dirs[1], n.dirsHash[1])
			if err != nil {
				return err
			}
			subTheirs, err := m.subTree(n.dirs[2], n.dirsHash[2])
			if err != nil {
				return err
			}
			if err := m.collect(path, subBase, subOurs, subTheirs); err != nil {
				return err
			}
		}
	}
	return nil
}

// register adds an early-resolved node (no stages needed downstream).
func (m *merger) register(dn *dirNode, name, path string, fill func(*node)) {
	n := &node{name: name, resolved: true}
	fill(n)
	dn.nodes[name] = n
	m.byPath[path] = n
}

func (m *merger) subTree(has bool, h plumbing.Hash) (*object.Tree, error) {
	if !has {
		return nil, nil
	}
	return object.GetTree(m.s, h)
}

// indexTree maps entry names to entries for one side of a directory.
func indexTree(t *object.Tree) map[string]*object.TreeEntry {
	if t == nil {
		return nil
	}
	idx := make(map[string]*object.TreeEntry, len(t.Entries))
	for i := range t.Entries {
		idx[t.Entries[i].Name] = &t.Entries[i]
	}
	return idx
}

func isFileEntry(e *object.TreeEntry) bool {
	return e != nil && e.Mode != filemode.Dir
}

func sameEntry(a, b *object.TreeEntry) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Mode == b.Mode && a.Hash == b.Hash
}

func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}
