package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"gopkg.in/src-d/go-git.v2/internal"
)

// Commit points to a single tree, marking it as what the project looked like
// at a certain point in time. It contains meta-information about that point
// in time, such as a timestamp, the author of the changes since the last
// commit, a pointer to the previous commit(s), etc.
// http://schacon.github.io/gitbook/1_the_git_object_model.html
type Commit struct {
	Hash      internal.Hash
	Tree      internal.Hash
	Parents   []internal.Hash
	Author    Signature
	Committer Signature
	Message   string
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
				c.Tree = internal.NewHash(string(split[1]))
			case "parent":
				c.Parents = append(c.Parents, internal.NewHash(string(split[1])))
			case "author":
				c.Author = ParseSignature(split[1])
			case "committer":
				c.Committer = ParseSignature(split[1])
			}
		} else {
			c.Message += string(line) + "\n"
		}

		if err == io.EOF {
			return nil
		}
	}
}

// Tree is basically like a directory - it references a bunch of other trees
// and/or blobs (i.e. files and sub-directories)
type Tree struct {
	Entries []TreeEntry
	Hash    internal.Hash
}

// TreeEntry represents a file
type TreeEntry struct {
	Name string
	Mode os.FileMode
	Hash internal.Hash
}

// Decode transform an internal.Object into a Tree struct
func (t *Tree) Decode(o internal.Object) error {
	t.Hash = o.Hash()
	if o.Size() == 0 {
		return nil
	}

	r := bufio.NewReader(o.Reader())
	for {
		mode, err := r.ReadString(' ')
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		fm, err := strconv.ParseInt(mode[:len(mode)-1], 8, 32)
		if err != nil && err != io.EOF {
			return err
		}

		name, err := r.ReadString(0)
		if err != nil && err != io.EOF {
			return err
		}

		var hash internal.Hash
		_, err = r.Read(hash[:])
		if err != nil && err != io.EOF {
			return err
		}

		t.Entries = append(t.Entries, TreeEntry{
			Hash: hash,
			Mode: os.FileMode(fm),
			Name: name[:len(name)-1],
		})
	}

	return nil
}

// Blob is used to store file data - it is generally a file.
type Blob struct {
	Hash internal.Hash
	Size int64
	obj  internal.Object
}

// Decode transform an internal.Object into a Blob struct
func (b *Blob) Decode(o internal.Object) error {
	b.Hash = o.Hash()
	b.Size = o.Size()
	b.obj = o

	return nil
}

// Reader returns a reader allow the access to the content of the blob
func (b *Blob) Reader() io.Reader {
	return b.obj.Reader()
}

// Signature represents an action signed by a person
type Signature struct {
	Name  string
	Email string
	When  time.Time
}

// ParseSignature parse a byte slice returning a new action signature.
func ParseSignature(signature []byte) Signature {
	ret := Signature{}
	if len(signature) == 0 {
		return ret
	}

	from := 0
	state := 'n' // n: name, e: email, t: timestamp, z: timezone
	for i := 0; ; i++ {
		var c byte
		var end bool
		if i < len(signature) {
			c = signature[i]
		} else {
			end = true
		}

		switch state {
		case 'n':
			if c == '<' || end {
				if i == 0 {
					break
				}
				ret.Name = string(signature[from : i-1])
				state = 'e'
				from = i + 1
			}
		case 'e':
			if c == '>' || end {
				ret.Email = string(signature[from:i])
				i++
				state = 't'
				from = i + 1
			}
		case 't':
			if c == ' ' || end {
				t, err := strconv.ParseInt(string(signature[from:i]), 10, 64)
				if err == nil {
					ret.When = time.Unix(t, 0)
				}
				end = true
			}
		}

		if end {
			break
		}
	}

	return ret
}

func (s *Signature) String() string {
	return fmt.Sprintf("%q <%s> @ %s", s.Name, s.Email, s.When)
}
