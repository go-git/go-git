package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v2/internal"
)

// Commit points to a single tree, marking it as what the project looked like
// at a certain point in time. It contains meta-information about that point
// in time, such as a timestamp, the author of the changes since the last
// commit, a pointer to the previous commit(s), etc.
// http://schacon.github.io/gitbook/1_the_git_object_model.html
type Commit struct {
	Hash      internal.Hash
	Author    Signature
	Committer Signature
	Message   string

	tree    internal.Hash
	parents []internal.Hash
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

// Decode transform an internal.Object into a Blob struct
func (c *Commit) Decode(o internal.Object) error {
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
				c.tree = internal.NewHash(string(split[1]))
			case "parent":
				c.parents = append(c.parents, internal.NewHash(string(split[1])))
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
		internal.CommitObject, c.Hash, c.Author.String(), c.Author.When,
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
	ch chan internal.Object
	r  *Repository
}

func newIter(r *Repository) iter {
	ch := make(chan internal.Object, 1)
	return iter{ch, r}
}

func (i *iter) Add(o internal.Object) {
	i.ch <- o
}

func (i *iter) Close() {
	close(i.ch)
}
