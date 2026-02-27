package git

import (
	"bytes"
	"fmt"
	"iter"

	"github.com/go-git/go-git/v6/plumbing/object"
)

// StatusCode status code of a file in the Worktree
type StatusCode byte

// File status codes.
const (
	Unmodified         StatusCode = ' '
	Untracked          StatusCode = '?'
	Modified           StatusCode = 'M'
	Added              StatusCode = 'A'
	Deleted            StatusCode = 'D'
	Renamed            StatusCode = 'R'
	Copied             StatusCode = 'C'
	UpdatedButUnmerged StatusCode = 'U'
)

// FileStatus contains the status of a file in the worktree
type FileStatus struct {
	// Staging is the status of a file in the staging area
	Staging StatusCode
	// Worktree is the status of a file in the worktree
	Worktree StatusCode
	// Extra contains extra information, such as the previous name in a rename
	Extra string
}

// Status represents the current status of a Worktree.
type Status struct {
	// head is needed to figure out whether the file is unmodified or untracked.
	head *object.Tree
	// m consists of statuses of files that are changed.
	m map[string]FileStatus
}

// File returns the FileStatus for a given path.
func (s Status) File(path string) FileStatus {
	if _, ok := s.m[path]; !ok {
		// The file hasn't changed or cannot be seen by git.

		if s.head != nil {
			if _, err := s.head.FindEntry(path); err == nil {
				return FileStatus{Staging: Unmodified, Worktree: Unmodified}
			}
		}

		return FileStatus{Staging: Untracked, Worktree: Untracked}
	}

	return s.m[path]
}

// IsUntracked checks if file for given path is 'Untracked'
func (s Status) IsUntracked(path string) bool {
	stat := s.File(path)
	return stat.Worktree == Untracked
}

// IsClean returns true if all the files are in Unmodified status.
func (s Status) IsClean() bool {
	return len(s.m) == 0
}

// Len returns the total count of the file statuses.
func (s Status) Len() int {
	return len(s.m)
}

// Iter returns the iterator of file statuses.
func (s Status) Iter() iter.Seq2[string, FileStatus] {
	return func(yield func(path string, file FileStatus) bool) {
		for path, file := range s.m {
			if !yield(path, file) {
				return
			}
		}
	}
}

func (s Status) String() string {
	buf := bytes.NewBuffer(nil)
	for path, status := range s.m {
		if status.Staging == Renamed {
			path = fmt.Sprintf("%s -> %s", path, status.Extra)
		}

		fmt.Fprintf(buf, "%c%c %s\n", status.Staging, status.Worktree, path)
	}

	return buf.String()
}
