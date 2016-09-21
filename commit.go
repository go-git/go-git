package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
)

// Hash hash of an object
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

// Tree returns the Tree from the commit
func (c *Commit) Tree() (*Tree, error) {
	return c.r.Tree(c.tree)
}

// Parents return a CommitIter to the parent Commits
func (c *Commit) Parents() *CommitIter {
	return NewCommitIter(c.r, core.NewObjectLookupIter(
		c.r.s.ObjectStorage(),
		core.CommitObject,
		c.parents,
	))
}

// NumParents returns the number of parents in a commit.
func (c *Commit) NumParents() int {
	return len(c.parents)
}

// File returns the file with the specified "path" in the commit and a
// nil error if the file exists. If the file does not exist, it returns
// a nil file and the ErrFileNotFound error.
func (c *Commit) File(path string) (*File, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	return tree.File(path)
}

// Files returns a FileIter allowing to iterate over the Tree
func (c *Commit) Files() (*FileIter, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	return tree.Files(), nil
}

// ID returns the object ID of the commit. The returned value will always match
// the current value of Commit.Hash.
//
// ID is present to fulfill the Object interface.
func (c *Commit) ID() core.Hash {
	return c.Hash
}

// Type returns the type of object. It always returns core.CommitObject.
//
// Type is present to fulfill the Object interface.
func (c *Commit) Type() core.ObjectType {
	return core.CommitObject
}

// Decode transforms a core.Object into a Commit struct.
func (c *Commit) Decode(o core.Object) (err error) {
	if o.Type() != core.CommitObject {
		return ErrUnsupportedObject
	}

	c.Hash = o.Hash()

	reader, err := o.Reader()
	if err != nil {
		return err
	}
	defer checkClose(reader, &err)

	r := bufio.NewReader(reader)

	var message bool
	for {
		line, err := r.ReadSlice('\n')
		if err != nil && err != io.EOF {
			return err
		}

		if !message {
			line = bytes.TrimSpace(line)
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
			c.Message += string(line)
		}

		if err == io.EOF {
			return nil
		}
	}
}

// History return a slice with the previous commits in the history of this commit
func (c *Commit) History() ([]*Commit, error) {
	var commits []*Commit
	err := WalkCommitHistory(c, func(commit *Commit) error {
		commits = append(commits, commit)
		return nil
	})

	ReverseSortCommits(commits)
	return commits, err
}

// Encode transforms a Commit into a core.Object.
func (b *Commit) Encode(o core.Object) error {
	o.SetType(core.CommitObject)
	w, err := o.Writer()
	if err != nil {
		return err
	}
	defer checkClose(w, &err)
	if _, err = fmt.Fprintf(w, "tree %s\n", b.tree.String()); err != nil {
		return err
	}
	for _, parent := range b.parents {
		if _, err = fmt.Fprintf(w, "parent %s\n", parent.String()); err != nil {
			return err
		}
	}
	if _, err = fmt.Fprint(w, "author "); err != nil {
		return err
	}
	if err = b.Author.Encode(w); err != nil {
		return err
	}
	if _, err = fmt.Fprint(w, "\ncommitter "); err != nil {
		return err
	}
	if err = b.Committer.Encode(w); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "\n\n%s", b.Message); err != nil {
		return err
	}
	return err
}

func (c *Commit) String() string {
	return fmt.Sprintf(
		"%s %s\nAuthor: %s\nDate:   %s\n\n%s\n",
		core.CommitObject, c.Hash, c.Author.String(),
		c.Author.When.Format(DateFormat), indent(c.Message),
	)
}

func indent(t string) string {
	var output []string
	for _, line := range strings.Split(t, "\n") {
		if len(line) != 0 {
			line = "    " + line
		}

		output = append(output, line)
	}

	return strings.Join(output, "\n")
}

// CommitIter provides an iterator for a set of commits.
type CommitIter struct {
	core.ObjectIter
	r *Repository
}

// NewCommitIter returns a CommitIter for the given repository and underlying
// object iterator.
//
// The returned CommitIter will automatically skip over non-commit objects.
func NewCommitIter(r *Repository, iter core.ObjectIter) *CommitIter {
	return &CommitIter{iter, r}
}

// Next moves the iterator to the next commit and returns a pointer to it. If it
// has reached the end of the set it will return io.EOF.
func (iter *CommitIter) Next() (*Commit, error) {
	obj, err := iter.ObjectIter.Next()
	if err != nil {
		return nil, err
	}

	commit := &Commit{r: iter.r}
	return commit, commit.Decode(obj)
}

// ForEach call the cb function for each commit contained on this iter until
// an error happends or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *CommitIter) ForEach(cb func(*Commit) error) error {
	return iter.ObjectIter.ForEach(func(obj core.Object) error {
		commit := &Commit{r: iter.r}
		if err := commit.Decode(obj); err != nil {
			return err
		}

		return cb(commit)
	})
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

// ReverseSortCommits sort a commit list by commit date, from newer to older.
func ReverseSortCommits(l []*Commit) {
	s := &commitSorterer{l}
	sort.Sort(sort.Reverse(s))
}
