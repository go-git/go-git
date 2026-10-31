package mergeort

import (
	"fmt"
	"sort"

	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/merkletrie"
)

// This file ports merge-ort's detect_and_process_renames for regular files:
// renames are detected per side with go-git's own detector (exact hash
// matches first, then content similarity) over the base→side tree diffs,
// then applied to the collected stages following process_renames:
//
//   - rename/rename(1to1): both sides renamed identically — the base stage
//     moves to the target and the source is removed;
//   - rename/rename(1to2): both targets receive the content-merged result
//     (each side keeping its own version for binary), the source keeps a
//     conflicted stage-1 entry;
//   - rename/delete: the target survives, flagged unclean;
//   - rename/add (or 2to1 collision): the target gets a content merge of
//     the renamed blob and the colliding file;
//   - plain rename: the other side's version of the source becomes the
//     other side's stage at the target, setting up a normal three-way
//     content merge there.
//
// Directory rename detection is deliberately absent (see package docs).

type renamePair struct {
	oldPath, newPath string
	side             int // 1 = ours, 2 = theirs
}

// processRenames detects and applies renames for both sides.
func (m *merger) processRenames(base, ours, theirs *object.Tree) error {
	oursPairs, err := m.detectRenames(base, ours, 1)
	if err != nil {
		return err
	}
	theirsPairs, err := m.detectRenames(base, theirs, 2)
	if err != nil {
		return err
	}

	all := append(oursPairs, theirsPairs...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].oldPath != all[j].oldPath {
			return all[i].oldPath < all[j].oldPath
		}
		return all[i].side < all[j].side
	})

	for i := 0; i < len(all); i++ {
		p := all[i]
		if p.oldPath == p.newPath {
			continue
		}
		oldN := m.byPath[p.oldPath]
		if oldN == nil || oldN.resolved {
			// The source lives in a subtree resolved during collect —
			// nothing underneath can need this rename.
			continue
		}
		newN := m.byPath[p.newPath]
		if newN == nil {
			continue
		}

		if i+1 < len(all) && all[i+1].oldPath == p.oldPath {
			q := all[i+1]
			if q.newPath == p.newPath {
				m.applyRename1to1(oldN, newN, p)
			} else {
				newN2 := m.byPath[q.newPath]
				if newN2 != nil {
					if err := m.applyRename1to2(oldN, newN, newN2, p, q); err != nil {
						return err
					}
				}
			}
			i++ // both renames of the source handled
			continue
		}

		if err := m.applyRename(oldN, newN, p); err != nil {
			return err
		}
	}
	return nil
}

// applyRename1to1 handles both sides renaming a source to the same target.
func (m *merger) applyRename1to1(oldN, newN *node, p renamePair) {
	// merge-ort does not rewrite pathnames for 1to1 renames, keeping
	// plain branch labels in any content merge at the target.
	newN.stages[0] = oldN.stages[0]
	m.resolveRemoval(oldN)
}

// applyRename1to2 handles a source renamed to different targets on each
// side: both targets get the content-merged version, the source keeps its
// base stage as a conflicted entry.
func (m *merger) applyRename1to2(oldN, newN, newN2 *node, p, q renamePair) error {
	cr, err := m.contentMerge(p.oldPath, oldN.stages[0], newN.stages[1], newN2.stages[2],
		[3]string{p.oldPath, p.newPath, q.newPath}, 1)
	if err != nil {
		return err
	}
	newN.stages[1] = cr.v
	// Binary merges take one side; do not copy side 1's bytes to side 2's
	// path (at recursion depth both get the base, like git).
	if !(cr.binary && m.callDepth == 0) {
		newN2.stages[2] = cr.v
	}
	newN.pathConflict = true
	newN2.pathConflict = true
	oldN.pathConflict = true
	m.message(p.oldPath, "CONFLICT (rename/rename): %s renamed to %s in %s and to %s in %s.",
		p.oldPath, p.newPath, m.branchLabels[0], q.newPath, m.branchLabels[1])
	return nil
}

// applyRename applies a single-sided rename per process_renames.
func (m *merger) applyRename(oldN, newN *node, p renamePair) error {
	t, o := p.side, 3-p.side // renaming side, other side
	sourceDeleted := oldN.stages[o].mode == filemode.Empty
	collision := newN.stages[o].mode != filemode.Empty
	typeChanged := !sourceDeleted &&
		isRegularFile(oldN.stages[o].mode) != isRegularFile(newN.stages[t].mode)

	// A type-changing rename colliding with the other side's file is a
	// double rename that eluded detection (no break detection); let the
	// plain branches sort it out (merge-ort does the same).
	if typeChanged && collision {
		collision = false
	}

	renameBranch := m.branchLabels[t-1]
	deleteBranch := m.branchLabels[o-1]

	if collision && !sourceDeleted {
		// rename/add or rename/rename(2to1): content merge with the
		// other side's version at the SOURCE path (not the colliding
		// file), the result becoming the renaming side's stage at the
		// target; the colliding file keeps its own stage, so
		// process_entry later merges the two (possibly with nested
		// markers — hence the message wording).
		var o, a, b version
		pathnames := [3]string{p.oldPath, p.oldPath, p.oldPath}
		if t == 1 {
			o, a, b = oldN.stages[0], newN.stages[1], oldN.stages[2]
			pathnames[1] = p.newPath
		} else {
			o, a, b = oldN.stages[0], oldN.stages[1], newN.stages[2]
			pathnames[2] = p.newPath
		}
		cr, err := m.contentMerge(p.oldPath, o, a, b, pathnames, 1)
		if err != nil {
			return err
		}
		newN.stages[t] = cr.v
		if !cr.clean {
			m.message(p.newPath, "CONFLICT (rename involved in collision): rename of %s -> %s has content conflicts AND collides with another path; this may result in nested conflict markers.",
				p.oldPath, p.newPath)
		}
	} else if collision && sourceDeleted {
		// rename/add/delete: looks like an add/add at the target.
		newN.pathConflict = true
		m.message(p.newPath, "CONFLICT (rename/delete): %s renamed to %s in %s, but deleted in %s.",
			p.oldPath, p.newPath, renameBranch, deleteBranch)
	} else {
		newN.stages[0] = withPath(oldN.stages[0], p.oldPath)
		switch {
		case typeChanged:
			// The source gained a differently-typed file from the
			// renaming side; it survives there, base stage cleared.
			oldN.stages[0] = version{}
		case sourceDeleted:
			newN.pathConflict = true
			m.message(p.newPath, "CONFLICT (rename/delete): %s renamed to %s in %s, but deleted in %s.",
				p.oldPath, p.newPath, renameBranch, deleteBranch)
		default:
			newN.stages[o] = withPath(oldN.stages[o], p.oldPath)
		}
	}

	if !typeChanged {
		m.resolveRemoval(oldN)
	}
	return nil
}

// resolveRemoval marks a rename source as resolved by removal.
func (m *merger) resolveRemoval(n *node) {
	n.resolved = true
	n.resV = version{}
}

func withPath(v version, path string) version {
	v.path = path
	return v
}

// detectRenames finds one side's renames as modify changes whose name
// changed, pairing only non-tree entries (git's rename detection pairs
// blobs, symlinks and gitlinks, never subtrees).
func (m *merger) detectRenames(base, side *object.Tree, sideIdx int) ([]renamePair, error) {
	changes, err := object.DiffTree(base, side)
	if err != nil {
		return nil, err
	}
	var filtered object.Changes
	for _, c := range changes {
		if changeIsFileIsh(c) {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	detected, err := object.DetectRenames(filtered, &object.DiffTreeOptions{
		DetectRenames:    true,
		RenameScore:      m.opts.RenameScore,
		RenameLimit:      m.opts.RenameLimit,
		OnlyExactRenames: m.opts.OnlyExactRenames,
	})
	if err != nil {
		return nil, err
	}

	var pairs []renamePair
	for _, c := range detected {
		action, err := c.Action()
		if err != nil {
			return nil, err
		}
		if action == merkletrie.Modify && c.From.Name != c.To.Name {
			pairs = append(pairs, renamePair{
				oldPath: c.From.Name,
				newPath: c.To.Name,
				side:    sideIdx,
			})
		}
	}
	return pairs, nil
}

// changeIsFileIsh reports whether both ends of a change are non-tree
// entries (an absent end qualifies).
func changeIsFileIsh(c *object.Change) bool {
	if c.From.Tree != nil && c.From.TreeEntry.Mode == filemode.Dir {
		return false
	}
	if c.To.Tree != nil && c.To.TreeEntry.Mode == filemode.Dir {
		return false
	}
	return true
}

// message records a path message (ort's path_msg).
func (m *merger) message(path, format string, args ...any) {
	m.messages = append(m.messages, Message{Path: path, Text: fmt.Sprintf(format, args...)})
}
