package mergeort

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// This file ports merge-ort's process_entries as a recursive tree build:
// each directory's entries are decided bottom-up, files go through the
// process_entry decision table, and directory/file conflicts reconcile by
// outcome — a directory that merges to nothing lets the file stay, a
// surviving directory keeps the path and moves the file to path~branch.

// fileDecision is one node's merge outcome: the version to record, plus an
// entry-name override when a distinct-types conflict relocated it.
type fileDecision struct {
	v    version
	name string
}

// processDirectory merges one directory, returning its written tree hash
// and whether the merged tree has entries.
func (m *merger) processDirectory(dirPath string) (plumbing.Hash, bool, error) {
	dn := m.dirs[dirPath]
	if dn == nil {
		h, err := m.putTree(nil)
		return h, false, err
	}

	names := make([]string, 0, len(dn.nodes))
	for n := range dn.nodes {
		names = append(names, n)
	}
	sort.Strings(names)

	var entries []object.TreeEntry
	for _, name := range names {
		n := dn.nodes[name]
		childPath := joinPath(dirPath, name)

		if n.resolved {
			if v := n.resV; v.mode != filemode.Empty {
				entries = append(entries, object.TreeEntry{Name: name, Mode: v.mode, Hash: v.hash})
				m.resultPaths[childPath] = true
			}
			continue
		}

		hasDir := n.dirs[0] || n.dirs[1] || n.dirs[2]
		if !hasDir {
			decisions, clean, err := m.decideFile(childPath, n)
			if err != nil {
				return plumbing.ZeroHash, false, err
			}
			for _, d := range decisions {
				if d.v.mode == filemode.Empty {
					continue
				}
				en := d.name
				if en == "" {
					en = name
				}
				entries = append(entries, object.TreeEntry{Name: en, Mode: d.v.mode, Hash: d.v.hash})
				m.resultPaths[joinPath(dirPath, en)] = true
			}
			if !clean {
				m.markConflict(childPath, n)
			}
			continue
		}

		// A directory is present on at least one side: merge it first,
		// then reconcile with the file stages (D/F).
		subHash, subEntries, err := m.processDirectory(childPath)
		if err != nil {
			return plumbing.ZeroHash, false, err
		}

		decisions, clean, err := m.decideFile(childPath, n)
		if err != nil {
			return plumbing.ZeroHash, false, err
		}
		v := firstVersion(decisions)

		switch {
		case !subEntries:
			// The directory merges to nothing: any surviving file
			// simply keeps the path.
			for _, d := range decisions {
				if d.v.mode == filemode.Empty {
					continue
				}
				en := d.name
				if en == "" {
					en = name
				}
				entries = append(entries, object.TreeEntry{Name: en, Mode: d.v.mode, Hash: d.v.hash})
				m.resultPaths[joinPath(dirPath, en)] = true
			}
			if !clean {
				m.markConflict(childPath, n)
			}
		case v.mode == filemode.Empty:
			// Directory survives, file merges away.
			entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: subHash})
			m.resultPaths[childPath] = true
			if !clean {
				m.markConflict(childPath, n)
			}
		default:
			// Directory survives and the file survives: the directory
			// keeps the path, the file relocates to path~branch
			// (df_file_index picks the side without a directory).
			branchSide := 1
			if n.dirs[1] {
				branchSide = 2
			}
			branch := m.branchLabels[branchSide-1]
			relocated := m.uniquePath(childPath, branch)
			entries = append(entries, object.TreeEntry{
				Name: baseName(relocated),
				Mode: v.mode,
				Hash: v.hash,
			})
			m.resultPaths[relocated] = true
			entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: subHash})
			m.resultPaths[childPath] = true
			m.markConflict(relocated, n)
			m.message(relocated, "CONFLICT (file/directory): directory in the way of %s from %s; moving it to %s instead.",
				childPath, branch, relocated)
		}
	}

	h, err := m.putTree(entries)
	return h, len(entries) > 0, err
}

// decideFile applies the process_entry decision table to a node's file
// stages, returning the outcome versions (usually one; two when a
// distinct-types conflict relocates entries) and overall cleanliness.
func (m *merger) decideFile(path string, n *node) ([]fileDecision, bool, error) {
	s0, s1, s2 := &n.stages[0], &n.stages[1], &n.stages[2]
	present := func(v *version) bool { return v.mode != filemode.Empty }

	// match_mask as computed at collect time: rename rewrites do not
	// invalidate it (process_entry semantics).
	matchMask := n.matchMask

	if matchMask != 0 {
		var v version
		switch matchMask {
		case 6:
			v = *s1
		case 3:
			v = *s2 // ours matches base: take theirs (absent = deletion)
		default:
			v = *s1 // theirs matches base: take ours
		}
		return []fileDecision{{v: v}}, !n.pathConflict, nil
	}

	switch {
	case present(s1) && present(s2):
		if fileType(s1.mode) != fileType(s2.mode) {
			// Two different items from file/submodule/symlink.
			if m.callDepth > 0 {
				// Just use the version from the merge base.
				return []fileDecision{{v: *s0}}, false, nil
			}
			return m.distinctTypes(path, n), false, nil
		}
		// Need a two-way or three-way content merge.
		pathnames := [3]string{s0.path, s1.path, s2.path}
		cr, err := m.contentMerge(path, *s0, *s1, *s2, pathnames, 0)
		if err != nil {
			return nil, false, err
		}
		if !cr.clean {
			reason := "content"
			switch {
			// Submodule trumps add/add, like process_entry.
			case s1.mode == filemode.Submodule:
				reason = "submodule"
			case !present(s0):
				reason = "add/add"
			}
			m.message(path, "CONFLICT (%s): Merge conflict in %s", reason, path)
		}
		return []fileDecision{{v: cr.v}}, cr.clean && !n.pathConflict, nil

	case present(s0) && (present(s1) || present(s2)):
		// Modify/delete.
		side := 1
		if present(s2) {
			side = 2
		}
		idx := side
		if m.callDepth > 0 {
			idx = 0
		}
		if !(n.pathConflict && sameHash(*s0, n.stages[side])) {
			modifyBranch := m.branchLabels[side-1]
			deleteBranch := m.branchLabels[2-side]
			m.message(path, "CONFLICT (modify/delete): %s deleted in %s and modified in %s. Version %s of %s left in tree.",
				path, deleteBranch, modifyBranch, modifyBranch, path)
		}
		return []fileDecision{{v: n.stages[idx]}}, false, nil

	case present(s1) || present(s2):
		// Added on one side.
		side := 1
		if present(s2) {
			side = 2
		}
		return []fileDecision{{v: n.stages[side]}}, !n.pathConflict, nil

	default:
		// Deleted on both sides (base only).
		return []fileDecision{{v: version{}}}, !n.pathConflict, nil
	}
}

// distinctTypes splits a file/submodule/symlink clash into separate paths
// (process_entry's distinct-types handling): the regular file relocates to
// path~branch; when neither side is regular, both relocate.
func (m *merger) distinctTypes(path string, n *node) []fileDecision {
	s1, s2 := &n.stages[1], &n.stages[2]
	renameA := isRegularFile(s1.mode)
	renameB := isRegularFile(s2.mode)
	if !renameA && !renameB {
		renameA, renameB = true, true
	}

	aPath, bPath := path, path
	if renameA {
		aPath = m.uniquePath(path, m.branchLabels[0])
	}
	if renameB {
		bPath = m.uniquePath(path, m.branchLabels[1])
	}
	if renameA && renameB {
		m.message(path, "CONFLICT (distinct types): %s had different types on each side; renamed both of them so each can be recorded somewhere.", path)
	} else {
		m.message(path, "CONFLICT (distinct types): %s had different types on each side; renamed one of them so each can be recorded somewhere.", path)
	}

	m.markConflict(path, n)
	return []fileDecision{
		{v: *s1, name: baseName(aPath)},
		{v: *s2, name: baseName(bPath)},
	}
}

// firstVersion returns the first non-empty decision version, if any.
func firstVersion(decisions []fileDecision) version {
	for _, d := range decisions {
		if d.v.mode != filemode.Empty {
			return d.v
		}
	}
	return version{}
}

// markConflict records a conflicted path with its current stages.
func (m *merger) markConflict(path string, n *node) {
	m.clean = false
	c := Conflict{Path: path}
	for i := range n.stages {
		if n.stages[i].mode == filemode.Empty {
			continue
		}
		p := path
		if n.stages[i].path != "" {
			p = n.stages[i].path
		}
		c.Stages[i] = &Stage{Mode: n.stages[i].mode, Hash: n.stages[i].hash, Path: p}
	}
	m.conflicts = append(m.conflicts, c)
}

// uniquePath allocates path~branch (then path~branch_0, ...) for entries
// that must move out of the way; branch slashes flatten like git's
// add_flattened_path.
func (m *merger) uniquePath(path, branch string) string {
	cand := path + "~" + strings.ReplaceAll(branch, "/", "_")
	if !m.pathTaken(cand) {
		return cand
	}
	for i := 0; ; i++ {
		c := fmt.Sprintf("%s_%d", cand, i)
		if !m.pathTaken(c) {
			return c
		}
	}
}

// pathTaken consults the collected paths and the entries already placed in
// the result; it is conservative for unresolved nodes.
func (m *merger) pathTaken(path string) bool {
	if m.resultPaths[path] {
		return true
	}
	if n, ok := m.byPath[path]; ok {
		if n.resolved {
			return n.resV.mode != filemode.Empty
		}
		return true
	}
	if _, ok := m.dirs[path]; ok {
		return true
	}
	return false
}

func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// sortTreeEntries orders entries in git tree order: directories sort as
// name+"/" (treeEntrySortName in package object).
func sortTreeEntries(entries []object.TreeEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return treeSortName(entries[i]) < treeSortName(entries[j])
	})
}

func treeSortName(e object.TreeEntry) string {
	if e.Mode == filemode.Dir {
		return e.Name + "/"
	}
	return e.Name
}
