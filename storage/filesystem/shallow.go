package filesystem

import (
	"bufio"
	"bytes"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ShallowStorage where the shallow commits are stored, an internal to
// manipulate the shallow file
type ShallowStorage struct {
	dir          *dotgit.DotGit
	objectFormat formatcfg.ObjectFormat
}

// SetShallow replaces the shallow file with commits, one hex object ID per
// line. Without commits it removes the file, since Git takes any shallow file,
// even an empty one, to mean the repository is shallow. See
// [dotgit.DotGit.SetShallow] for how the file is replaced.
func (s *ShallowStorage) SetShallow(commits []plumbing.Hash) error {
	var buf bytes.Buffer
	buf.Grow(len(commits) * (s.objectFormat.HexSize() + 1))
	for _, h := range commits {
		buf.WriteString(h.String())
		buf.WriteByte('\n')
	}

	return s.dir.SetShallow(buf.Bytes())
}

// Shallow returns the shallow commits reading from shallow file from .git.
//
// Each line must start with a full hash in the repository's object format.
// Anything after the hash is ignored, so CRLF line endings are accepted. A
// line that does not start with a full hash is an error, as in upstream's
// is_repository_shallow.
func (s *ShallowStorage) Shallow() ([]plumbing.Hash, error) {
	f, err := s.dir.Shallow()
	if f == nil || err != nil {
		return nil, err
	}

	defer ioutil.CheckClose(f, &err)

	var hash []plumbing.Hash

	hexSize := s.objectFormat.HexSize()
	scn := bufio.NewScanner(f)
	for scn.Scan() {
		line := scn.Text()
		if len(line) < hexSize {
			return nil, fmt.Errorf("bad shallow line: %q", line)
		}
		h, ok := plumbing.FromHex(line[:hexSize])
		if !ok {
			return nil, fmt.Errorf("bad shallow line: %q", line)
		}
		hash = append(hash, h)
	}

	return hash, scn.Err()
}
