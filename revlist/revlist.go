// Package revlist allows to create the revision history of a file, this
// is, the list of commits in the past that affect the file.
//
// The general idea is to traverse the git commit graph backward,
// flattening the graph into a linear history, and skipping commits that
// are irrelevant for the particular file.
//
// There is no single answer for this operation. The git command
// "git-revlist" returns different histories depending on its arguments
// and some internal heuristics.
//
// The current implementation tries to get something similar to what you
// whould get using git-revlist. See the failing tests for some
// insight about how the current implementation and git-revlist differs.
package revlist

import (
	"bytes"
	"errors"
	"io"
	"sort"

	"github.com/sergi/go-diff/diffmatchpatch"

	"gopkg.in/src-d/go-git.v2"
	"gopkg.in/src-d/go-git.v2/core"
	"gopkg.in/src-d/go-git.v2/diff"
)

// New errors defined by the package.
var ErrFileNotFound = errors.New("file not found")

// A Revs is a list of revisions for a file (basically a list of commits).
// It implements sort.Interface.
type Revs []*git.Commit

func (l Revs) Len() int {
	return len(l)
}

// sorts from older to newer commit.
func (l Revs) Less(i, j int) bool {
	return l[i].Committer.When.Before(l[j].Committer.When)
}

func (l Revs) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

// for debugging
func (l Revs) String() string {
	var buf bytes.Buffer
	for _, c := range l {
		buf.WriteString(c.Hash.String()[:8])
		buf.WriteString("\n")
	}
	return buf.String()
}

// New returns a Revs pointer for the
// file at "path", from commit "commit" backwards in time.
// The commits are stored in arbitrary order.
// It stops searching a branch for a file upon reaching the commit
// were it was created.
// Moves and copies are not currently supported.
// Cherry-picks are not detected and therefore are added to the list
// (see git path-id for hints on how to fix this).
// This function implements is equivalent to running go-rev-Revs.
func New(repo *git.Repository, commit *git.Commit, path string) (Revs, error) {
	result := make(Revs, 0)
	seen := make(map[core.Hash]struct{}, 0)
	err := walkGraph(&result, &seen, repo, commit, path)
	if err != nil {
		return nil, err
	}
	sort.Sort(result)
	result = removeComp(path, result, equivalent) // for merges of identical cherry-picks
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Recursive traversal of the commit graph, generating a linear history
// of the path.
func walkGraph(result *Revs, seen *map[core.Hash]struct{}, repo *git.Repository, current *git.Commit, path string) error {
	// check and update seen
	if _, ok := (*seen)[current.Hash]; ok {
		return nil
	}
	(*seen)[current.Hash] = struct{}{}

	// if the path is not in the current commit, stop searching.
	if _, found := git.FindFile(path, current); !found {
		return nil
	}

	parents := parentsContainingPath(path, current)

	switch len(parents) {
	// if the path is not found in any of its parents, the path was
	// created by this commit; we must add it to the revisions list and
	// stop searching. This includes the case when current is the
	// initial commit.
	case 0:
		//fmt.Println(current.Hash.String(), ": case 0")
		*result = append(*result, current)
		return nil
	case 1: // only one parent contains the path
		// if the file contents has change, add the current commit
		different, err := differentContents(path, current, parents)
		if err != nil {
			return err
		}
		if len(different) == 1 {
			//fmt.Println(current.Hash.String(), ": case 1")
			*result = append(*result, current)
		}
		// in any case, walk the parent
		return walkGraph(result, seen, repo, parents[0], path)
	default: // more than one parent contains the path
		// TODO: detect merges that had a conflict, because they must be
		// included in the result here.
		for _, p := range parents {
			err := walkGraph(result, seen, repo, p, path)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// TODO: benchmark this making git.Commit.parent public instead of using
// an iterator
func parentsContainingPath(path string, c *git.Commit) []*git.Commit {
	var result []*git.Commit
	iter := c.Parents()
	for {
		parent, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				return result
			}
			panic("unreachable")
		}
		if _, found := git.FindFile(path, parent); found {
			result = append(result, parent)
		}
	}
}

// Returns an slice of the commits in "cs" that has the file "path", but with different
// contents than what can be found in "c".
func differentContents(path string, c *git.Commit, cs []*git.Commit) ([]*git.Commit, error) {
	result := make([]*git.Commit, 0, len(cs))
	h, found := blobHash(path, c)
	if !found {
		return nil, ErrFileNotFound
	}
	for _, cx := range cs {
		if hx, found := blobHash(path, cx); found && h != hx {
			result = append(result, cx)
		}
	}
	return result, nil
}

// blobHash returns the hash of a path in a commit
func blobHash(path string, commit *git.Commit) (hash core.Hash, found bool) {
	file, found := git.FindFile(path, commit)
	if !found {
		var empty core.Hash
		return empty, found
	}
	return file.Hash, true
}

// Returns a new slice of commits, with duplicates removed.  Expects a
// sorted commit list.  Duplication is defined according to "comp".  It
// will always keep the first commit of a series of duplicated commits.
func removeComp(path string, cs []*git.Commit, comp func(string, *git.Commit, *git.Commit) bool) []*git.Commit {
	result := make([]*git.Commit, 0, len(cs))
	if len(cs) == 0 {
		return result
	}
	result = append(result, cs[0])
	for i := 1; i < len(cs); i++ {
		if !comp(path, cs[i], cs[i-1]) {
			result = append(result, cs[i])
		}
	}
	return result
}

// Equivalent commits are commits whos patch is the same.
func equivalent(path string, a, b *git.Commit) bool {
	numParentsA := a.NumParents()
	numParentsB := b.NumParents()

	// the first commit is not equivalent to anyone
	// and "I think" merges can not be equivalent to anything
	if numParentsA != 1 || numParentsB != 1 {
		return false
	}

	iterA := a.Parents()
	parentA, _ := iterA.Next()
	iterB := b.Parents()
	parentB, _ := iterB.Next()

	dataA, _ := git.Data(path, a)
	dataParentA, _ := git.Data(path, parentA)
	dataB, _ := git.Data(path, b)
	dataParentB, _ := git.Data(path, parentB)

	diffsA := diff.Do(dataParentA, dataA)
	diffsB := diff.Do(dataParentB, dataB)

	if sameDiffs(diffsA, diffsB) {
		return true
	}
	return false
}

func sameDiffs(a, b []diffmatchpatch.Diff) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameDiff(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameDiff(a, b diffmatchpatch.Diff) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case 0:
		return git.CountLines(a.Text) == git.CountLines(b.Text)
	case 1, -1:
		return a.Text == b.Text
	default:
		panic("unreachable")
	}
}
