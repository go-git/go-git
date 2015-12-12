package git

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"gopkg.in/src-d/go-git.v2/core"
)

// New errors defined by this package.
var ErrFileNotFound = errors.New("file not found")

type Hash core.Hash

// Commit points to a single tree, marking it as what the project looked like
// at a certain point in time. It contains meta-information about that point
// in time, such as a timestamp, the author of the changes since the last
// commit, a pointer to the previous commit(s), etc.
// http://schacon.github.io/gitbook/1_the_git_object_model.html
type Commit struct {
	Hash      core.Hash
	Author    Signature
	Committer Signature
	Message   string

	tree    core.Hash
	parents []core.Hash
	r       *Repository
}

func (c *Commit) Tree() *Tree {
	tree, _ := c.r.Tree(c.tree)
	return tree
}

func (c *Commit) Parents() *CommitIter {
	i := NewCommitIter(c.r)
	go func() {
		defer i.Close()
		for _, hash := range c.parents {
			obj, _ := c.r.Storage.Get(hash)
			i.Add(obj)
		}
	}()

	return i
}

// NumParents returns the number of parents in a commit.
func (c *Commit) NumParents() int {
	return len(c.parents)
}

// File returns the file with the specified "path" in the commit and a
// nil error if the file exists. If the file does not exists, it returns
// a nil file and the ErrFileNotFound error.
func (c *Commit) File(path string) (file *File, err error) {
	for file := range c.Tree().Files() {
		if file.Name == path {
			return file, nil
		}
	}
	return nil, ErrFileNotFound
}

// Decode transform an core.Object into a Blob struct
func (c *Commit) Decode(o core.Object) error {
	c.Hash = o.Hash()
	r := bufio.NewReader(o.Reader())

	var message bool
	for {
		line, err := r.ReadSlice('\n')
		if err != nil && err != io.EOF {
			return err
		}

		line = bytes.TrimSpace(line)
		if !message {
			if len(line) == 0 {
				message = true
				continue
			}

			split := bytes.SplitN(line, []byte{' '}, 2)
			switch string(split[0]) {
			case "tree":
				c.tree = core.NewHash(string(split[1]))
			case "parent":
				c.parents = append(c.parents, core.NewHash(string(split[1])))
			case "author":
				c.Author.Decode(split[1])
			case "committer":
				c.Committer.Decode(split[1])
			}
		} else {
			c.Message += string(line) + "\n"
		}

		if err == io.EOF {
			return nil
		}
	}
}

func (c *Commit) String() string {
	return fmt.Sprintf(
		"%s %s\nAuthor: %s\nDate:   %s\n",
		core.CommitObject, c.Hash, c.Author.String(), c.Author.When,
	)
}

type CommitIter struct {
	iter
}

func NewCommitIter(r *Repository) *CommitIter {
	return &CommitIter{newIter(r)}
}

func (i *CommitIter) Next() (*Commit, error) {
	obj := <-i.ch
	if obj == nil {
		return nil, io.EOF
	}

	commit := &Commit{r: i.r}
	return commit, commit.Decode(obj)
}

type iter struct {
	ch chan core.Object
	r  *Repository

	IsClosed bool
}

func newIter(r *Repository) iter {
	ch := make(chan core.Object, 1)
	return iter{ch: ch, r: r}
}

func (i *iter) Add(o core.Object) {
	if i.IsClosed {
		return
	}

	i.ch <- o
}

func (i *iter) Close() {
	if i.IsClosed {
		return
	}

	defer func() { i.IsClosed = true }()
	close(i.ch)
}

type commitSorterer struct {
	l []*Commit
}

func (s commitSorterer) Len() int {
	return len(s.l)
}

func (s commitSorterer) Less(i, j int) bool {
	return s.l[i].Committer.When.Before(s.l[j].Committer.When)
}

func (s commitSorterer) Swap(i, j int) {
	s.l[i], s.l[j] = s.l[j], s.l[i]
}

// SortCommits sort a commit list by commit date, from older to newer.
func SortCommits(l []*Commit) {
	s := &commitSorterer{l}
	sort.Sort(s)
}
