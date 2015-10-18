package packfile

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/src-d/crawler/clients/git/commons"
)

type Object interface {
	Type() string
	Hash() string
}

type Hash []byte

func (h Hash) String() string {
	return hex.EncodeToString(h)
}

type Commit struct {
	Tree      Hash
	Parents   []Hash
	Author    Signature
	Committer Signature
	Message   string
	hash      string
}

func NewCommit(b []byte) (*Commit, error) {
	o := &Commit{hash: commons.GitHash("commit", b)}

	lines := bytes.Split(b, []byte{'\n'})
	for i := range lines {
		if len(lines[i]) > 0 {
			var err error

			split := bytes.SplitN(lines[i], []byte{' '}, 2)
			switch string(split[0]) {
			case "tree":
				o.Tree = make([]byte, 20)
				_, err = hex.Decode(o.Tree, split[1])
			case "parent":
				h := make([]byte, 20)
				_, err = hex.Decode(h, split[1])
				if err == nil {
					o.Parents = append(o.Parents, h)
				}
			case "author":
				o.Author = NewSignature(split[1])
			case "committer":
				o.Committer = NewSignature(split[1])
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

func (o *Commit) Type() string {
	return "commit"
}

func (o *Commit) Hash() string {
	return o.hash
}

type Signature struct {
	Name  string
	Email string
	When  time.Time
}

func NewSignature(signature []byte) Signature {
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

type Tree struct {
	Entries []TreeEntry
	hash    string
}

type TreeEntry struct {
	Name string
	Hash string
}

func NewTree(body []byte) (*Tree, error) {
	o := &Tree{hash: commons.GitHash("tree", body)}

	if len(body) == 0 {
		return o, nil
	}

	for {
		split := bytes.SplitN(body, []byte{0}, 2)
		split1 := bytes.SplitN(split[0], []byte{' '}, 2)

		o.Entries = append(o.Entries, TreeEntry{
			Name: string(split1[1]),
			Hash: fmt.Sprintf("%x", split[1][0:20]),
		})

		body = split[1][20:]
		if len(split[1]) == 20 {
			break
		}
	}

	return o, nil
}

func (o *Tree) Type() string {
	return "tree"
}

func (o *Tree) Hash() string {
	return o.hash
}

type Blob struct {
	Len  int
	hash string
}

func NewBlob(b []byte) (*Blob, error) {
	return &Blob{Len: len(b), hash: commons.GitHash("blob", b)}, nil
}

func (o *Blob) Type() string {
	return "blob"
}

func (o *Blob) Hash() string {
	return o.hash
}

type ContentCallback func(hash string, content []byte)
