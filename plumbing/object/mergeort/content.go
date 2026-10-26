package mergeort

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// This file ports the blob-level merge chain of git's merge-ort:
// handle_content_merge (mode merge, trivial oid merge, type dispatch,
// two-way downgrade when the base has another type) on top of ll_merge's
// guards (MAX_XDIFF_SIZE, binary → ll_binary_merge semantics including the
// virtual-ancestor take-base rule) and merge_3way's label derivation
// (branch names, or "branch:path" when a rename moved a side's origin).

// contentResult is what a content merge produced.
type contentResult struct {
	v     version // mode Empty means "no result" (never for present sides)
	clean bool
	// binary reports the merge short-circuited as binary (one side taken,
	// or the base at recursion depth).
	binary bool
}

// contentMerge performs the three-way content merge of the file versions
// o (base), a (ours) and b (theirs) found at path. pathnames carries where
// each stage's blob came from (renames rewrite it; empty means path).
// extraMarker grows the conflict markers for rename-driven merges, whose
// output can end up nested inside another conflict (merge-ort's
// extra_marker_size).
//
// The merged blob is written to the storer when a real merge happened.
func (m *merger) contentMerge(path string, o, a, b version, pathnames [3]string, extraMarker int) (contentResult, error) {
	// Merge modes first (handle_content_merge). a and b are both present
	// and of the same type on entry (process_entry guarantees it).
	var mode filemode.FileMode
	clean := true
	if a.mode == b.mode || a.mode == o.mode {
		mode = b.mode
	} else {
		// Must be the 100644/100755 case.
		mode = a.mode
		clean = b.mode == o.mode
	}

	// Trivial oid merge.
	if sameHash(a, b) || sameHash(a, o) {
		return contentResult{v: versionWithMode(b, mode), clean: clean}, nil
	}
	if sameHash(b, o) {
		return contentResult{v: versionWithMode(a, mode), clean: clean}, nil
	}

	switch {
	case isRegularFile(a.mode):
		return m.mergeBlobs(path, o, a, b, mode, pathnames, clean, extraMarker)
	case a.mode == filemode.Submodule:
		// Without the submodule repository there is no history search
		// (merge_submodule's fast paths are already covered by the
		// trivial oid merge above): report a conflict keeping ours.
		// At recursion depth the base version wins (call_depth rule).
		if m.callDepth > 0 {
			return contentResult{v: o, clean: false}, nil
		}
		return contentResult{v: a, clean: false}, nil
	case a.mode == filemode.Symlink:
		if m.callDepth > 0 {
			return contentResult{v: o, clean: false}, nil
		}
		switch m.favor() {
		case xmFavorOurs:
			return contentResult{v: versionWithMode(a, mode), clean: clean}, nil
		case xmFavorTheirs:
			return contentResult{v: versionWithMode(b, mode), clean: clean}, nil
		default:
			return contentResult{v: versionWithMode(a, mode), clean: false}, nil
		}
	}
	// Unreachable for tree-collected entries (modes come from real trees).
	return contentResult{}, errUnsupportedMode{path: path, mode: a.mode}
}

type errUnsupportedMode struct {
	path string
	mode filemode.FileMode
}

func (e errUnsupportedMode) Error() string {
	return "mergeort: unsupported file mode " + e.mode.String() + " for " + e.path
}

// mergeBlobs runs the regular-file merge: binary guard, then the xdl_merge
// port with conflict markers, writing the result blob to the storer
// (ll_xdl_merge + ll_binary_merge + merge_3way label handling).
func (m *merger) mergeBlobs(path string, o, a, b version, mode filemode.FileMode, pathnames [3]string, clean bool, extraMarker int) (contentResult, error) {
	// A base of a different type (or absent) downgrades to a two-way
	// merge with an empty base.
	twoWay := fileType(o.mode) != fileType(a.mode)
	baseData := []byte(nil)
	if !twoWay {
		data, err := m.blob(o.hash)
		if err != nil {
			return contentResult{}, err
		}
		baseData = data
	}
	oursData, err := m.blob(a.hash)
	if err != nil {
		return contentResult{}, err
	}
	theirsData, err := m.blob(b.hash)
	if err != nil {
		return contentResult{}, err
	}

	labels := xmpOptions{
		ancestor: m.threeWayLabel(pathnames, 0, m.ancestorLabel, path),
		file1:    m.threeWayLabel(pathnames, 1, m.branchLabels[0], path),
		file2:    m.threeWayLabel(pathnames, 2, m.branchLabels[1], path),
	}

	favor := m.favor()
	if int64(len(baseData)) > maxDiffSize || int64(len(oursData)) > maxDiffSize ||
		int64(len(theirsData)) > maxDiffSize || isBinary(baseData) ||
		isBinary(oursData) || isBinary(theirsData) {
		// ll_binary_merge: virtual ancestors take the base and stay
		// clean; favors take their side and resolve; plain merges
		// take ours and conflict.
		m.message(path, "warning: Cannot merge binary files: %s (%s vs. %s)",
			path, labels.file1, labels.file2)
		switch {
		case m.callDepth > 0:
			return contentResult{v: versionWithMode(o, mode), clean: true, binary: true}, nil
		case favor == xmFavorOurs:
			return contentResult{v: versionWithMode(a, mode), clean: clean, binary: true}, nil
		case favor == xmFavorTheirs:
			return contentResult{v: versionWithMode(b, mode), clean: clean, binary: true}, nil
		default:
			return contentResult{v: versionWithMode(a, mode), clean: false, binary: true}, nil
		}
	}

	m.message(path, "Auto-merging %s", path)

	merged, xClean := xdlMerge(baseData, oursData, theirsData, xmpOptions{
		level:      xmZealous,
		favor:      favor,
		styleDiff3: m.opts.Style == StyleDiff3,
		markerSize: m.markerSize() + extraMarker,
		ancestor:   labels.ancestor,
		file1:      labels.file1,
		file2:      labels.file2,
	})

	h, err := m.putBlob(merged)
	if err != nil {
		return contentResult{}, err
	}
	return contentResult{v: version{mode: mode, hash: h, path: ""}, clean: clean && xClean}, nil
}

// threeWayLabel derives the conflict-marker label for one side
// (merge_3way): the plain branch label when all stages came from the same
// path, otherwise "label:path" so renames stay visible in the markers.
func (m *merger) threeWayLabel(pathnames [3]string, side int, label, path string) string {
	pn := [3]string{}
	for i := range pn {
		if pathnames[i] == "" {
			pn[i] = path
		} else {
			pn[i] = pathnames[i]
		}
	}
	if pn[0] == pn[1] && pn[1] == pn[2] {
		return label
	}
	return label + ":" + pn[side]
}

// favor resolves the effective favor: recursion depth always merges
// content plainly (merge_3way zeroes the variant for virtual ancestors).
func (m *merger) favor() int {
	if m.callDepth > 0 {
		return xmFavorNone
	}
	switch m.opts.Favor {
	case FavorOurs:
		return xmFavorOurs
	case FavorTheirs:
		return xmFavorTheirs
	case FavorUnion:
		return xmFavorUnion
	default:
		return xmFavorNone
	}
}

// markerSize grows with recursion depth so nested conflict markers from
// virtual-base merges stay distinguishable (extra_marker_size).
func (m *merger) markerSize() int {
	size := m.opts.MarkerSize
	if size <= 0 {
		size = defaultMarkerSize
	}
	return size + 2*m.callDepth
}

// blob reads a blob's full content.
func (m *merger) blob(h plumbing.Hash) ([]byte, error) {
	blob, err := object.GetBlob(m.s, h)
	if err != nil {
		return nil, err
	}
	r, err := blob.Reader()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// putBlob writes data as a blob and returns its hash.
func (m *merger) putBlob(data []byte) (plumbing.Hash, error) {
	obj := m.s.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	obj.SetSize(int64(len(data)))
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return m.s.SetEncodedObject(obj)
}

// putTree writes a tree with the given entries and returns its hash.
// Entries are sorted into git tree order first.
func (m *merger) putTree(entries []object.TreeEntry) (plumbing.Hash, error) {
	sortTreeEntries(entries)
	t := &object.Tree{Entries: entries}
	obj := m.s.NewEncodedObject()
	if err := t.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return m.s.SetEncodedObject(obj)
}

// version is one side's staged version of a path: mode Empty means the
// side does not have the path. path records where the blob came from when
// a rename moved it (for "branch:path" marker labels); empty means the
// node's own path.
type version struct {
	mode filemode.FileMode
	hash plumbing.Hash
	path string
}

func sameHash(a, b version) bool {
	return !a.hash.IsZero() && a.hash == b.hash
}

func versionWithMode(v version, mode filemode.FileMode) version {
	return version{mode: mode, hash: v.hash, path: v.path}
}

// isRegularFile matches S_ISREG: regular, executable or the deprecated
// mode.
func isRegularFile(mode filemode.FileMode) bool {
	return mode == filemode.Regular || mode == filemode.Deprecated || mode == filemode.Executable
}

// fileType collapses modes into the S_IFMT classes that merge differently
// (regular / symlink / gitlink).
func fileType(mode filemode.FileMode) int {
	switch {
	case isRegularFile(mode):
		return 0
	case mode == filemode.Symlink:
		return 1
	case mode == filemode.Submodule:
		return 2
	default:
		return -1
	}
}
