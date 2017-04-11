package git

import "fmt"

// Status current status of a Worktree
type Status map[string]*FileStatus

func (s Status) File(filename string) *FileStatus {
	if _, ok := (s)[filename]; !ok {
		s[filename] = &FileStatus{}
	}

	return s[filename]

}

func (s Status) IsClean() bool {
	for _, status := range s {
		if status.Worktree != Unmodified || status.Staging != Unmodified {
			return false
		}
	}

	return true
}

func (s Status) String() string {
	var names []string
	for name := range s {
		names = append(names, name)
	}

	var output string
	for _, name := range names {
		status := s[name]
		if status.Staging == 0 && status.Worktree == 0 {
			continue
		}

		if status.Staging == Renamed {
			name = fmt.Sprintf("%s -> %s", name, status.Extra)
		}

		output += fmt.Sprintf("%s%s %s\n", status.Staging, status.Worktree, name)
	}

	return output
}

// FileStatus status of a file in the Worktree
type FileStatus struct {
	Staging  StatusCode
	Worktree StatusCode
	Extra    string
}

// StatusCode status code of a file in the Worktree
type StatusCode int8

const (
	Unmodified StatusCode = iota
	Untracked
	Modified
	Added
	Deleted
	Renamed
	Copied
	UpdatedButUnmerged
)

func (c StatusCode) String() string {
	switch c {
	case Unmodified:
		return " "
	case Modified:
		return "M"
	case Added:
		return "A"
	case Deleted:
		return "D"
	case Renamed:
		return "R"
	case Copied:
		return "C"
	case UpdatedButUnmerged:
		return "U"
	case Untracked:
		return "?"
	default:
		return "-"
	}
}
