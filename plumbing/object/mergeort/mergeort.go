// Package mergeort implements an in-core three-way merge following the
// structure of git's merge-ort strategy ("Ostensibly Recursive's Twin"):
//
//	collect   — a single traversal of the base, ours and theirs trees
//	            registering per-path stages
//	renames   — rename detection on both sides (exact and similarity based,
//	            reusing object.DetectRenames) applied to the stage map
//	process   — per-path 3-way decisions, recursive content merges and
//	            conflict recording
//
// Like merge_incore_nonrecursive / git merge-tree --write-tree, the merge
// never touches a working tree or index: it produces an in-memory result
// tree (written to the object storer), a cleanliness flag, the conflicted
// stages and git-compatible conflict messages. Blob-level merges follow
// xdiff/xmerge.c structure (see xmerge.go).
//
// Differences from git's merge-ort, by design:
//   - directory rename detection is not implemented (equivalent to
//     git's -Xno-renames for directories only; file renames are detected);
//   - rename detection uses go-git's own detector (jgit-derived scoring),
//     so pairing ties can resolve differently than git's diffcore;
//   - there is no merge driver / gitattributes .gitattributes, renormalize
//     or submodule history consultation.
package mergeort

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ConflictStyle selects the conflict marker layout.
type ConflictStyle int

const (
	// StyleMerge is the default "merge" style: <<<<<<< ours, =======,
	// >>>>>>> theirs.
	StyleMerge ConflictStyle = iota
	// StyleDiff3 adds the base section between ours and the divider
	// (diff3 -m style).
	StyleDiff3
)

// Favor auto-resolves content conflicts the way git's -X options do.
type Favor int

const (
	// FavorNone reports content conflicts (default).
	FavorNone Favor = iota
	// FavorOurs resolves conflicted hunks with our version.
	FavorOurs
	// FavorTheirs resolves conflicted hunks with their version.
	FavorTheirs
	// FavorUnion concatenates both versions of conflicted hunks.
	FavorUnion
)

// Options configures a merge. The zero value is usable and equivalent to
// DefaultOptions minus the labels.
type Options struct {
	// AncestorLabel labels the base version in diff3-style conflict
	// markers and "ancestor:path" renames. Default: derived like git's
	// ("empty tree", the abbreviated base hash, or "merged common
	// ancestors" when several bases were folded).
	AncestorLabel string
	// OurLabel and TheirLabel label the conflict markers. Defaults:
	// "ours" and "theirs".
	OurLabel, TheirLabel string
	// MarkerSize is the conflict marker length (default 7).
	MarkerSize int
	// Style selects the conflict marker layout (default StyleMerge).
	Style ConflictStyle
	// Favor auto-resolves content conflicts (default FavorNone).
	Favor Favor
	// DisableRenames turns off rename detection, like git's
	// -Xno-renames. The zero value detects renames (git's default).
	DisableRenames bool
	// RenameScore is the similarity threshold for rename detection,
	// 0-100 (default 60, go-git's DefaultDiffTreeOptions).
	RenameScore uint
	// RenameLimit caps the delete×add comparisons during rename
	// detection; 0 means no limit.
	RenameLimit uint
	// OnlyExactRenames skips similarity-based rename detection.
	OnlyExactRenames bool
}

// DefaultOptions returns the recommended options.
func DefaultOptions() *Options {
	return &Options{
		OurLabel:    "ours",
		TheirLabel:  "theirs",
		RenameScore: 60,
	}
}

func (o *Options) normalize() *Options {
	n := *o
	if n.OurLabel == "" && n.TheirLabel == "" && n.AncestorLabel == "" {
		// Zero value: keep documented defaults for labels too.
		n.OurLabel = "ours"
		n.TheirLabel = "theirs"
	} else {
		if n.OurLabel == "" {
			n.OurLabel = "ours"
		}
		if n.TheirLabel == "" {
			n.TheirLabel = "theirs"
		}
	}
	if n.MarkerSize <= 0 {
		n.MarkerSize = defaultMarkerSize
	}
	if n.RenameScore == 0 {
		n.RenameScore = 60
	}
	return &n
}

// Stage is one side's version of a conflicted path.
type Stage struct {
	Mode filemode.FileMode
	Hash plumbing.Hash
	// Path is where this stage's blob came from when a rename supplied
	// it; it equals the conflict's Path otherwise.
	Path string
}

// Conflict describes one conflicted path: the three stages (nil where a
// side has no entry) and the machine-readable reason.
type Conflict struct {
	Path   string
	Reason string
	Stages [3]*Stage
}

// Message is an informational or conflict message, ort's path_messages
// equivalent (e.g. "CONFLICT (content): Merge conflict in file").
type Message struct {
	Path string
	Text string
}

// Result is the outcome of an in-core merge.
type Result struct {
	// Tree is the merged tree, written to the storer. On conflict it
	// holds the auto-merged working result: conflicted files carry
	// conflict markers (or one side's content when markers do not
	// apply), exactly like git merge-tree --write-tree.
	Tree *object.Tree
	// Clean reports whether the merge completed without conflicts.
	Clean bool
	// Conflicts lists the conflicted paths with their stages.
	Conflicts []Conflict
	// Messages carries the conflict and informational messages.
	Messages []Message
}

// merger holds the state of one in-core merge.
type merger struct {
	s             storer.EncodedObjectStorer
	opts          *Options
	branchLabels  [2]string // ours, theirs (Temporary merge branch N at depth)
	ancestorLabel string
	callDepth     int

	// dirs maps every directory path that needs merging to its entries;
	// the root is "". Directories fully resolved during collect (all
	// three sides equal) are absent.
	dirs map[string]*dirNode
	// byPath indexes every collected node by full path for the rename
	// phase.
	byPath map[string]*node
	// resultPaths records paths present in the result tree so far, for
	// unique-path allocation.
	resultPaths map[string]bool

	conflicts []Conflict
	messages  []Message
	clean     bool
}

type dirNode struct {
	path  string
	nodes map[string]*node
}

type node struct {
	name string
	// stages are the file versions per side; mode Empty = absent.
	stages [3]version
	// dirs/dirsHash record which sides hold a directory here.
	dirs     [3]bool
	dirsHash [3]plumbing.Hash
	// matchMask is the collect-time match_mask (bit pairs of equal,
	// present file sides). Deliberately stale after rename rewrites,
	// like merge-ort: process_entry consults it as collected.
	matchMask int
	// resolved marks nodes settled during collect or by the rename
	// phase; resV is the outcome (mode Empty = removed).
	resolved bool
	resV     version
	// pathConflict marks rename-induced conflicts that must stay
	// unclean even when the per-path decision is otherwise clean.
	pathConflict bool
}

// MergeTrees performs an in-core three-way merge of the trees ours and
// theirs against base, the merge-tree --write-tree primitive. base may be
// nil, meaning an empty tree (unrelated histories).
func MergeTrees(s storer.EncodedObjectStorer, base, ours, theirs *object.Tree, opts *Options) (*Result, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	m := &merger{
		s:             s,
		opts:          opts.normalize(),
		branchLabels:  [2]string{opts.OurLabel, opts.TheirLabel},
		ancestorLabel: opts.AncestorLabel,
		dirs:          map[string]*dirNode{},
		byPath:        map[string]*node{},
		resultPaths:   map[string]bool{},
		clean:         true,
	}
	if m.ancestorLabel == "" {
		m.ancestorLabel = "empty tree"
		if base != nil && !base.Hash.IsZero() {
			m.ancestorLabel = abbrevHash(base.Hash)
		}
	}
	return m.run(base, ours, theirs)
}

// MergeCommits performs an in-core three-way merge of two commits. The
// merge base is computed with Commit.MergeBase; multiple bases are folded
// pairwise into virtual bases whose conflicts are embedded as content
// (merge_ort_recursive), so the merge itself always runs three-way.
func MergeCommits(s storer.EncodedObjectStorer, ours, theirs *object.Commit, opts *Options) (*Result, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	m := &merger{
		s:             s,
		opts:          opts.normalize(),
		branchLabels:  [2]string{opts.OurLabel, opts.TheirLabel},
		ancestorLabel: opts.AncestorLabel,
		dirs:          map[string]*dirNode{},
		byPath:        map[string]*node{},
		resultPaths:   map[string]bool{},
		clean:         true,
	}

	bases, err := ours.MergeBase(theirs)
	if err != nil {
		return nil, err
	}

	switch len(bases) {
	case 0:
		if m.ancestorLabel == "" {
			m.ancestorLabel = "empty tree"
		}
	case 1:
		if m.ancestorLabel == "" {
			m.ancestorLabel = abbrevHash(bases[0].Hash)
		}
	default:
		if m.ancestorLabel == "" {
			m.ancestorLabel = "merged common ancestors"
		}
	}

	oursT, err := ours.Tree()
	if err != nil {
		return nil, err
	}
	theirsT, err := theirs.Tree()
	if err != nil {
		return nil, err
	}

	var baseT *object.Tree
	if len(bases) > 0 {
		baseT, err = m.foldBases(bases)
		if err != nil {
			return nil, err
		}
	}
	return m.run(baseT, oursT, theirsT)
}

// foldBases folds multiple merge bases into one virtual base tree
// (merge_ort_internal's folding loop): bases are merged pairwise, each
// pair against its own merge base recursively, and conflicts in the
// intermediate merges are embedded as content — the committed
// "conflicts" are already resolved.
func (m *merger) foldBases(bases []*object.Commit) (*object.Tree, error) {
	first := bases[0]
	for _, next := range bases[1:] {
		r, err := m.mergeCommitsDepth(first, next)
		if err != nil {
			return nil, err
		}
		first, err = m.virtualCommit(r.Tree, first, next)
		if err != nil {
			return nil, err
		}
	}
	return first.Tree()
}

// mergeCommitsDepth merges two commits at recursion depth with the
// "Temporary merge branch" labels, computing their merge base the same
// way MergeCommits does.
func (m *merger) mergeCommitsDepth(a, b *object.Commit) (*Result, error) {
	savedBranches, savedAncestor := m.branchLabels, m.ancestorLabel
	m.branchLabels = [2]string{"Temporary merge branch 1", "Temporary merge branch 2"}
	m.ancestorLabel = "merged common ancestors"
	m.callDepth++

	savedState := m.snapshotState()
	m.resetState()

	r, err := m.mergeCommitsCore(a, b)

	m.callDepth--
	m.branchLabels, m.ancestorLabel = savedBranches, savedAncestor
	m.restoreState(savedState)
	return r, err
}

func (m *merger) mergeCommitsCore(a, b *object.Commit) (*Result, error) {
	bases, err := a.MergeBase(b)
	if err != nil {
		return nil, err
	}
	at, err := a.Tree()
	if err != nil {
		return nil, err
	}
	bt, err := b.Tree()
	if err != nil {
		return nil, err
	}
	var baseT *object.Tree
	if len(bases) > 0 {
		baseT, err = m.foldBases(bases)
		if err != nil {
			return nil, err
		}
	}
	return m.run(baseT, at, bt)
}

// mergeState snapshots the per-merge working maps so a depth recursion can
// run a sub-merge in isolation.
type mergeState struct {
	dirs        map[string]*dirNode
	byPath      map[string]*node
	resultPaths map[string]bool
	conflicts   []Conflict
	messages    []Message
	clean       bool
}

func (m *merger) snapshotState() mergeState {
	return mergeState{
		dirs: m.dirs, byPath: m.byPath, resultPaths: m.resultPaths,
		conflicts: m.conflicts, messages: m.messages, clean: m.clean,
	}
}

func (m *merger) resetState() {
	m.dirs = map[string]*dirNode{}
	m.byPath = map[string]*node{}
	m.resultPaths = map[string]bool{}
	m.conflicts, m.messages, m.clean = nil, nil, true
}

func (m *merger) restoreState(s mergeState) {
	m.dirs, m.byPath, m.resultPaths = s.dirs, s.byPath, s.resultPaths
	m.conflicts, m.messages, m.clean = s.conflicts, s.messages, s.clean
}

// virtualCommit writes a throwaway commit wrapping tree with the given
// parents (make_virtual_commit) so a folded base can take part in later
// merge-base computations.
func (m *merger) virtualCommit(tree *object.Tree, parents ...*object.Commit) (*object.Commit, error) {
	c := &object.Commit{
		Message:  "virtual merge base (mergeort)",
		TreeHash: tree.Hash,
	}
	for _, p := range parents {
		c.ParentHashes = append(c.ParentHashes, p.Hash)
	}
	obj := m.s.NewEncodedObject()
	if err := c.Encode(obj); err != nil {
		return nil, err
	}
	h, err := m.s.SetEncodedObject(obj)
	if err != nil {
		return nil, err
	}
	return object.GetCommit(m.s, h)
}

// run executes the three phases over fresh state.
func (m *merger) run(base, ours, theirs *object.Tree) (*Result, error) {
	if err := m.collect("", base, ours, theirs); err != nil {
		return nil, err
	}
	if !m.opts.DisableRenames && base != nil && !base.Hash.IsZero() {
		if err := m.processRenames(base, ours, theirs); err != nil {
			return nil, err
		}
	}
	rootHash, _, err := m.processDirectory("")
	if err != nil {
		return nil, err
	}
	tree, err := object.GetTree(m.s, rootHash)
	if err != nil {
		return nil, err
	}
	return &Result{
		Tree:      tree,
		Clean:     m.clean,
		Conflicts: m.conflicts,
		Messages:  m.messages,
	}, nil
}

func abbrevHash(h plumbing.Hash) string {
	s := h.String()
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// treeEntryVersion converts a tree entry into a staged version.
func treeEntryVersion(e object.TreeEntry) version {
	return version{mode: e.Mode, hash: e.Hash}
}
