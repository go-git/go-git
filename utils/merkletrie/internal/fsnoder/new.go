package fsnoder

import (
	"bytes"
	"fmt"
	"io"

	"srcd.works/go-git.v4/utils/merkletrie/noder"
)

// New function creates a full merkle trie from the string description of
// a filesystem tree.  See examples of the string format in the package
// description.
func New(s string) (noder.Noder, error) {
	return decodeDir([]byte(s), root)
}

const (
	root    = true
	nonRoot = false
)

// Expected data: a fsnoder description, for example: A(foo bar qux ...).
// When isRoot is true, unnamed dirs are supported, for example: (foo
// bar qux ...)
func decodeDir(data []byte, isRoot bool) (*dir, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, io.EOF
	}

	// get the name of the dir (a single letter) and remove it from the
	// data.  In case the there is no name and isRoot is true, just use
	// "" as the name.
	var name string
	if data[0] == dirStartMark {
		if isRoot {
			name = ""
		} else {
			return nil, fmt.Errorf("inner unnamed dirs not allowed: %s", data)
		}
	} else {
		name = string(data[0])
		data = data[1:]
	}

	// check that data is enclosed in parents and it is big enough and
	// remove them.
	if len(data) < 2 {
		return nil, fmt.Errorf("malformed data: too short")
	}
	if data[0] != dirStartMark {
		return nil, fmt.Errorf("malformed data: first %q not found",
			dirStartMark)
	}
	if data[len(data)-1] != dirEndMark {
		return nil, fmt.Errorf("malformed data: last %q not found",
			dirEndMark)
	}
	data = data[1 : len(data)-1] // remove initial '(' and last ')'

	children, err := decodeChildren(data)
	if err != nil {
		return nil, err
	}

	return newDir(name, children)
}

func isNumber(b byte) bool {
	return '0' <= b && b <= '9'
}

func isLetter(b byte) bool {
	return ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

func decodeChildren(data []byte) ([]noder.Noder, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	chunks := split(data)
	ret := make([]noder.Noder, len(chunks))
	var err error
	for i, c := range chunks {
		ret[i], err = decodeChild(c)
		if err != nil {
			return nil, fmt.Errorf("malformed element %d (%s): %s", i, c, err)
		}
	}

	return ret, nil
}

// returns the description of the elements of a dir.  It is just looking
// for spaces if they are not part of inner dirs.
func split(data []byte) [][]byte {
	chunks := [][]byte{}

	start := 0
	dirDepth := 0
	for i, b := range data {
		switch b {
		case dirStartMark:
			dirDepth++
		case dirEndMark:
			dirDepth--
		case dirElementSep:
			if dirDepth == 0 {
				chunks = append(chunks, data[start:i+1])
				start = i + 1
			}
		}
	}
	chunks = append(chunks, data[start:])

	return chunks
}

// A child can be a file or a dir.
func decodeChild(data []byte) (noder.Noder, error) {
	clean := bytes.TrimSpace(data)
	if len(data) < 3 {
		return nil, fmt.Errorf("element too short: %s", clean)
	}

	switch clean[1] {
	case fileStartMark:
		return decodeFile(clean)
	case dirStartMark:
		return decodeDir(clean, nonRoot)
	default:
		if clean[0] == dirStartMark {
			return nil, fmt.Errorf("non-root unnamed dir are not allowed: %s",
				clean)
		}
		return nil, fmt.Errorf("malformed dir element: %s", clean)
	}
}

func decodeFile(data []byte) (noder.Noder, error) {
	if len(data) == 3 {
		return decodeEmptyFile(data)
	}

	if len(data) != 4 {
		return nil, fmt.Errorf("length is not 4")
	}
	if !isLetter(data[0]) {
		return nil, fmt.Errorf("name must be a letter")
	}
	if data[1] != '<' {
		return nil, fmt.Errorf("wrong file start character")
	}
	if !isNumber(data[2]) {
		return nil, fmt.Errorf("contents must be a number")
	}
	if data[3] != '>' {
		return nil, fmt.Errorf("wrong file end character")
	}

	name := string(data[0])
	contents := string(data[2])

	return newFile(name, contents)
}

func decodeEmptyFile(data []byte) (noder.Noder, error) {
	if len(data) != 3 {
		return nil, fmt.Errorf("length is not 3: %s", data)
	}
	if !isLetter(data[0]) {
		return nil, fmt.Errorf("name must be a letter: %s", data)
	}
	if data[1] != '<' {
		return nil, fmt.Errorf("wrong file start character: %s", data)
	}
	if data[2] != '>' {
		return nil, fmt.Errorf("wrong file end character: %s", data)
	}

	name := string(data[0])

	return newFile(name, "")
}

// HashEqual returns if a and b have the same hash.
func HashEqual(a, b noder.Hasher) bool {
	return bytes.Equal(a.Hash(), b.Hash())
}
