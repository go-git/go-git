package git

import (
	"bytes"
	"strings"
)

// CountLines returns the number of lines in a string.
// The newline character is assumed to be '\n'.
// The empty string contains 0 lines.
// If the last line of the string doesn't end with a newline, it will
// still be considered a line.
func CountLines(s string) int {
	if s == "" {
		return 0
	}
	nEol := strings.Count(s, "\n")
	if strings.HasSuffix(s, "\n") {
		return nEol
	}
	return nEol + 1
}

// FindFile searches for a path in a commit. Returns the file and true if found.
// Returns nil and false if not found.
// TODO: should this be a method of git.Commit instead?
func FindFile(path string, commit *Commit) (file *File, found bool) {
	tree := commit.Tree()
	for file := range tree.Files() {
		if file.Name == path {
			return file, true
		}
	}
	return nil, false
}

// Data returns the contents of a file in a commit and true if found.
// Returns an empty string and false if the file is not found in the
// commit.
// TODO: should this be a method of git.Commit instead?
func Data(path string, commit *Commit) (contents string, found bool) {
	file, found := FindFile(path, commit)
	if !found {
		return "", found
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(file)
	return buf.String(), found
}
