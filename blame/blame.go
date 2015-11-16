// Package blame contains blaming functionality for files in the repo.
//
// Blaming a file is finding what commit was the last to modify each of
// the lines in the file, therefore the output of a blaming operation is
// usualy a slice of commits, one commit per line in the file.
//
// This package also provides a pretty print function to output the
// results of a blame in a similar format to the git-blame command.
package blame

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/src-d/go-git.v2"
	"gopkg.in/src-d/go-git.v2/core"
	"gopkg.in/src-d/go-git.v2/diff"
	"gopkg.in/src-d/go-git.v2/revlist"
)

// Blame returns the last commit that modified each line of a file in
// a repository.
//
// The file to blame is identified by the input arguments: repo, commit and path.
// The output is a slice of commits, one for each line in the file.
//
// Blaming a file is a two step process:
//
// 1. Create a linear history of the commits affecting a file. We use
//    revlist.New for that.
//
// 2. Then build a graph with a node for every line in every file in
//    the history of the file.
//
//    Each node (line) holds the commit where it was introduced or
//    last modified. To achieve that we use the FORWARD algorithm
//    described in Zimmermann, et al. "Mining Version Archives for
//    Co-changed Lines", in proceedings of the Mining Software
//    Repositories workshop, Shanghai, May 22-23, 2006.
//
//    Each node is asigned a commit: Start by the nodes in the first
//    commit. Assign that commit as the creator of all its lines.
//
//    Then jump to the nodes in the next commit, and calculate the diff
//    between the two files. Newly created lines get
//    assigned the new commit as its origin. Modified lines also get
//    this new commit. Untouched lines retain the old commit.
//
//    All this work is done in the assignOrigin function.
//
//    This function holds all the internal relevant data in a blame
//    struct, that is not exported.
//
//    TODO: ways to improve the efficiency of this function:
//
//    1. Improve revlist
//
//	  2. Improve how to traverse the history (example a backward
//	  traversal will be much more efficient)
//
//    TODO: ways to improve the functrion in general
//
//    1. Add memoization betweenn revlist and assign.
//
//    2. It is using much more memmory than needed, see the TODOs below.
func Blame(repo *git.Repository, commit *git.Commit, path string) ([]*git.Commit, error) {
	// init the internal blame struct
	b := new(blame)
	b.repo = repo
	b.fRev = commit
	b.path = path

	// calculte the history of the file and store it in the
	// internal blame struct.
	var err error
	b.revs, err = revlist.New(b.repo, b.fRev, b.path)
	if err != nil {
		return nil, err
	}
	sort.Sort(b.revs) // for forward blame, we need the history sorted by commit date

	// allocate space for the data in all the revisions of the file
	b.data = make([]string, len(b.revs))

	// init the graph
	b.graph = make([][]vertex, len(b.revs))

	// for all every revision of the file, starting with the first
	// one...
	var found bool
	for i, rev := range b.revs {
		// get the contents of the file
		b.data[i], found = git.Data(b.path, rev)
		if !found {
			continue
		}
		// count its lines
		nLines := git.CountLines(b.data[i])
		// create a node for each line
		b.graph[i] = make([]vertex, nLines)
		// assign a commit to each node
		// if this is the first revision, then the node is assigned to
		// this first commit.
		if i == 0 {
			for j := 0; j < nLines; j++ {
				b.graph[i][j] = vertex(b.revs[i])
			}
		} else {
			// if this is not the first commit, then assign to the old
			// commit or to the new one, depending on what the diff
			// says.
			b.assignOrigin(i, i-1)
		}
	}

	// fill in the output results: copy the nodes of the last revision
	// into the result.
	fVs := b.graph[len(b.graph)-1]
	result := make([]*git.Commit, 0, len(fVs))
	for _, v := range fVs {
		c := git.Commit(*v)
		result = append(result, &c)
	}
	return result, nil
}

// this struct is internally used by the blame function to hold its
// intputs, outputs and state.
type blame struct {
	repo  *git.Repository // the repo holding the history of the file to blame
	path  string          // the path of the file to blame
	fRev  *git.Commit     // the commit of the final revision of the file to blame
	revs  revlist.Revs    // the chain of revisions affecting the the file to blame
	data  []string        // the contents on the file in all the revisions TODO: not all data is needed, only the current rev and the prev
	graph [][]vertex      // the graph of the lines in the file across all the revisions TODO: not all vertexes are needed, only the current rev and the prev
}

type vertex *git.Commit // a vertex only needs to store the original commit it came from

// Assigns origin to vertexes in current (c) rev from data in its previous (p)
// revision
func (b *blame) assignOrigin(c, p int) {
	// assign origin based on diff info
	hunks := diff.Do(b.data[p], b.data[c])
	sl := -1 // source line
	dl := -1 // destination line
	for h := range hunks {
		hLines := git.CountLines(hunks[h].Text)
		for hl := 0; hl < hLines; hl++ {
			// fmt.Printf("file=%q, rev=%d, r=%d, h=%d, hunk=%v, hunkLine=%d\n", file, rev, r, h, hunks[h], hl)
			switch {
			case hunks[h].Type == 0:
				sl++
				dl++
				b.graph[c][dl] = b.graph[p][sl]
			case hunks[h].Type == 1:
				dl++
				b.graph[c][dl] = vertex(b.revs[c])
			case hunks[h].Type == -1:
				sl++
			default:
				panic("unreachable")
			}
		}
	}
}

// This will print the results of a Blame as in git-blame.
func (b *blame) PrettyPrint() string {
	var buf bytes.Buffer

	contents, found := git.Data(b.path, b.fRev)
	if !found {
		panic("PrettyPrint: internal error in repo.Data")
	}

	lines := strings.Split(contents, "\n")
	// max line number length
	mlnl := len(fmt.Sprintf("%s", strconv.Itoa(len(lines))))
	// max author length
	mal := b.maxAuthorLength()
	format := fmt.Sprintf("%%s (%%-%ds %%%dd) %%s\n",
		mal, mlnl)

	fVs := b.graph[len(b.graph)-1]
	for ln, v := range fVs {
		fmt.Fprintf(&buf, format, v.Hash.String()[:8],
			prettyPrintAuthor(fVs[ln]), ln+1, lines[ln])
	}
	return buf.String()
}

// utility function to pretty print the author.
func prettyPrintAuthor(c *git.Commit) string {
	return fmt.Sprintf("%s %s", c.Author.Name, c.Author.When.Format("2006-01-02"))
}

// utility function to calculate the number of runes needed
// to print the longest author name in the blame of a file.
func (b *blame) maxAuthorLength() int {
	memo := make(map[core.Hash]struct{}, len(b.graph)-1)
	fVs := b.graph[len(b.graph)-1]
	m := 0
	for ln := range fVs {
		if _, ok := memo[fVs[ln].Hash]; ok {
			continue
		}
		memo[fVs[ln].Hash] = struct{}{}
		m = max(m, utf8.RuneCountInString(prettyPrintAuthor(fVs[ln])))
	}
	return m
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
