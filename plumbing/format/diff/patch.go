// Package diff implements diff operations and unified diff encoding.
package diff

import (
	"github.com/go-git/go-git/v6/plumbing"
	textdiff "github.com/go-git/go-git/v6/plumbing/diff"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

// Patch represents a collection of steps to transform several files.
type Patch interface {
	// FilePatches returns a slice of patches per file.
	FilePatches() []FilePatch
	// Message returns an optional message that can be at the top of the
	// Patch representation.
	Message() string
}

// FilePatch represents the necessary steps to transform one file into another.
type FilePatch interface {
	// IsBinary returns true if this patch is representing a binary file.
	IsBinary() bool
	// Files returns the from and to Files, with all the necessary metadata
	// about them. If the patch creates a new file, "from" will be nil.
	// If the patch deletes a file, "to" will be nil.
	Files() (from, to File)
	// Diff returns the text changes, or nil for a binary file.
	// Text files return a non-nil result even when empty or unchanged.
	// The result's input bytes must not be modified.
	Diff() textdiff.Result
}

// File contains all the file metadata necessary to print some patch formats.
type File interface {
	// Hash returns the File Hash.
	Hash() plumbing.Hash
	// Mode returns the FileMode.
	Mode() filemode.FileMode
	// Path returns the complete Path to the file, including the filename.
	Path() string
}
