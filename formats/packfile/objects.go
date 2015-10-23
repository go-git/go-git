package packfile

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

type ObjectType string

const (
	CommitObject ObjectType = "commit"
	TreeObject   ObjectType = "tree"
	BlobObject   ObjectType = "blob"
)

// Object generic object interface
type Object interface {
	Type() ObjectType
	Hash() Hash
}

// Hash SHA1 hased content
type Hash [20]byte

// ComputeHash compute the hash for a given objType and content
func ComputeHash(t ObjectType, content []byte) Hash {
	h := []byte(t)
	h = append(h, ' ')
	h = strconv.AppendInt(h, int64(len(content)), 10)
	h = append(h, 0)
	h = append(h, content...)

	return Hash(sha1.Sum(h))
}

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// Commit points to a single tree, marking it as what the project looked like
// at a certain point in time. It contains meta-information about that point
// in time, such as a timestamp, the author of the changes since the last
// commit, a pointer to the previous commit(s), etc.
// http://schacon.github.io/gitbook/1_the_git_object_model.html
type Commit struct {
	Tree      Hash
	Parents   []Hash
	Author    Signature
	Committer Signature
	Message   string
	hash      Hash
}

// ParseCommit transform a byte slice into a Commit struct
func ParseCommit(b []byte) (*Commit, error) {
	o := &Commit{hash: ComputeHash(CommitObject, b)}

	lines := bytes.Split(b, []byte{'\n'})
	for i := range lines {
		if len(lines[i]) > 0 {
			var err error

			split := bytes.SplitN(lines[i], []byte{' '}, 2)
			switch string(split[0]) {
			case "tree":
				_, err = hex.Decode(o.Tree[:], split[1])
			case "parent":
				var h Hash
				_, err = hex.Decode(h[:], split[1])
				if err == nil {
					o.Parents = append(o.Parents, h)
				}
			case "author":
				o.Author = ParseSignature(split[1])
			case "committer":
				o.Committer = ParseSignature(split[1])
			}

			if err != nil {
				return nil, err
			}
		} else {
			o.Message = string(bytes.Join(append(lines[i+1:]), []byte{'\n'}))
			break
		}

	}

	return o, nil
}

// Type returns the object type
func (o *Commit) Type() ObjectType {
	return CommitObject
}

// Hash returns the computed hash of the commit
func (o *Commit) Hash() Hash {
	return o.hash
}

// Tree is basically like a directory - it references a bunch of other trees
// and/or blobs (i.e. files and sub-directories)
type Tree struct {
	Entries []TreeEntry
	hash    Hash
}

// TreeEntry represents a file
type TreeEntry struct {
	Name string
	Hash Hash
}

// ParseTree transform a byte slice into a Tree struct
func ParseTree(b []byte) (*Tree, error) {
	o := &Tree{hash: ComputeHash(TreeObject, b)}

	if len(b) == 0 {
		return o, nil
	}

	for {
		split := bytes.SplitN(b, []byte{0}, 2)
		split1 := bytes.SplitN(split[0], []byte{' '}, 2)

		entry := TreeEntry{}
		entry.Name = string(split1[1])
		copy(entry.Hash[:], split[1][0:20])

		o.Entries = append(o.Entries, entry)

		b = split[1][20:]
		if len(split[1]) == 20 {
			break
		}
	}

	return o, nil
}

// Type returns the object type
func (o *Tree) Type() ObjectType {
	return TreeObject
}

// Hash returns the computed hash of the tree
func (o *Tree) Hash() Hash {
	return o.hash
}

// Blob is used to store file data - it is generally a file.
type Blob struct {
	Len  int
	hash Hash
}

// ParseBlob transform a byte slice into a Blob struct
func ParseBlob(b []byte) (*Blob, error) {
	return &Blob{
		Len:  len(b),
		hash: ComputeHash(BlobObject, b),
	}, nil
}

// Type returns the object type
func (o *Blob) Type() ObjectType {
	return BlobObject
}

// Hash returns the computed hash of the blob
func (o *Blob) Hash() Hash {
	return o.hash
}

type ContentCallback func(hash Hash, content []byte)

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
