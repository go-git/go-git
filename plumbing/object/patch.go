package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/diff"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	fdiff "github.com/go-git/go-git/v6/plumbing/format/diff"
)

// ErrCanceled is returned when the operation is canceled.
var ErrCanceled = errors.New("operation canceled")

// PatchOptions selects the text backend. Nil Differ uses the go-diff backend.
// The backend must be safe for the caller's use of concurrent operations.
type PatchOptions struct {
	Differ diff.Differ
}

func getPatch(message string, changes ...*Change) (*Patch, error) {
	ctx := context.Background()
	return getPatchContext(ctx, message, changes...)
}

func getPatchContext(ctx context.Context, message string, changes ...*Change) (*Patch, error) {
	return getPatchWithOptions(ctx, message, nil, changes...)
}

func getPatchWithOptions(ctx context.Context, message string, options *PatchOptions, changes ...*Change) (*Patch, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrCanceled
	}
	backend := diff.NewDefault()
	if options != nil && options.Differ != nil {
		backend = options.Differ
	}
	if len(changes) == 0 {
		return &Patch{message: message}, nil
	}

	filePatches := make([]fdiff.FilePatch, 0, len(changes))
	for _, c := range changes {
		select {
		case <-ctx.Done():
			return nil, ErrCanceled
		default:
		}

		fp, err := filePatchWithDiffer(ctx, c, backend)
		if err != nil {
			return nil, err
		}

		filePatches = append(filePatches, fp)
	}

	return &Patch{message, filePatches}, nil
}

func filePatchWithContext(ctx context.Context, c *Change) (fdiff.FilePatch, error) {
	return filePatchWithDiffer(ctx, c, diff.NewDefault())
}

func filePatchWithDiffer(ctx context.Context, c *Change, backend diff.Differ) (fdiff.FilePatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrCanceled
	}
	// Submodules (gitlinks) are not blob objects, so their contents cannot be
	// read as files. Git represents them in a diff by a single
	// "Subproject commit <hash>" line, so build that content synthetically
	// instead of dropping the change from the patch.
	var fromContent, toContent []byte
	if isSubmodule(c.From) || isSubmodule(c.To) {
		fromContent = submoduleContent(c.From)
		toContent = submoduleContent(c.To)
	} else {
		from, to, err := c.Files()
		if err != nil {
			return nil, err
		}
		var fIsBinary, tIsBinary bool
		fromContent, fIsBinary, err = fileContent(from)
		if err != nil {
			return nil, err
		}
		toContent, tIsBinary, err = fileContent(to)
		if err != nil {
			return nil, err
		}
		if fIsBinary || tIsBinary {
			return &textFilePatch{from: c.From, to: c.To}, nil
		}
	}

	result, err := backend.Diff(ctx, fromContent, toContent)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrCanceled
		}
		return nil, err
	}
	if result == nil {
		return nil, errors.New("diff: backend returned a nil result")
	}

	return &textFilePatch{
		result: result,
		from:   c.From,
		to:     c.To,
	}, nil
}

func isSubmodule(e ChangeEntry) bool {
	return e != empty && e.TreeEntry.Mode == filemode.Submodule
}

// submoduleContent represents a gitlink as Git's synthetic text line.
func submoduleContent(e ChangeEntry) []byte {
	if !isSubmodule(e) {
		return nil
	}
	return fmt.Appendf(nil, "Subproject commit %s\n", e.TreeEntry.Hash)
}

func fileContent(f *File) (content []byte, isBinary bool, err error) {
	if f == nil {
		return content, isBinary, err
	}

	isBinary, err = f.IsBinary()
	if err != nil || isBinary {
		return content, isBinary, err
	}

	content, err = f.Bytes()

	return content, isBinary, err
}

// Patch is an implementation of fdiff.Patch interface
type Patch struct {
	message     string
	filePatches []fdiff.FilePatch
}

// FilePatches returns the file patches.
func (p *Patch) FilePatches() []fdiff.FilePatch {
	return p.filePatches
}

// Message returns the patch message.
func (p *Patch) Message() string {
	return p.message
}

// Encode encodes the patch to the given writer.
func (p *Patch) Encode(w io.Writer) error {
	ue := fdiff.NewUnifiedEncoder(w, fdiff.DefaultContextLines)

	return ue.Encode(p)
}

// Stats returns line counts from raw edits, independent of display filters.
func (p *Patch) Stats() FileStats {
	return getFileStatsFromFilePatches(p.FilePatches())
}

func (p *Patch) String() string {
	buf := bytes.NewBuffer(nil)
	err := p.Encode(buf)
	if err != nil {
		return fmt.Sprintf("malformed patch: %s", err.Error())
	}

	return buf.String()
}

// changeEntryWrapper is an implementation of fdiff.File interface
type changeEntryWrapper struct {
	ce ChangeEntry
}

func (f *changeEntryWrapper) Hash() plumbing.Hash {
	if f.Empty() {
		return plumbing.ZeroHash
	}

	return f.ce.TreeEntry.Hash
}

func (f *changeEntryWrapper) Mode() filemode.FileMode {
	return f.ce.TreeEntry.Mode
}

func (f *changeEntryWrapper) Path() string {
	if f.Empty() {
		return ""
	}

	return f.ce.Name
}

func (f *changeEntryWrapper) Empty() bool {
	// Submodules (gitlinks) are not files, but they still take part in a diff
	// and thus must not be treated as empty entries.
	return !f.ce.TreeEntry.Mode.IsFile() &&
		f.ce.TreeEntry.Mode != filemode.Submodule
}

// textFilePatch is an implementation of fdiff.FilePatch interface
type textFilePatch struct {
	result   diff.Result
	from, to ChangeEntry
}

func (tf *textFilePatch) Files() (from, to fdiff.File) {
	f := &changeEntryWrapper{tf.from}
	t := &changeEntryWrapper{tf.to}

	if !f.Empty() {
		from = f
	}

	if !t.Empty() {
		to = t
	}

	return from, to
}

func (tf *textFilePatch) IsBinary() bool {
	return tf.result == nil
}

func (tf *textFilePatch) Diff() diff.Result {
	return tf.result
}

// FileStat stores the status of changes in content of a file.
type FileStat struct {
	Name     string
	Addition int
	Deletion int
}

func (fs FileStat) String() string {
	return printStat([]FileStat{fs})
}

// FileStats is a collection of FileStat.
type FileStats []FileStat

func (fileStats FileStats) String() string {
	return printStat(fileStats)
}

// printStat prints the stats of changes in content of files.
// Original implementation: https://github.com/git/git/blob/1a87c842ece327d03d08096395969aca5e0a6996/diff.c#L2615
// Parts of the output:
// <pad><filename><pad>|<pad><changeNumber><pad><+++/---><newline>
// example: " main.go | 10 +++++++--- "
func printStat(fileStats []FileStat) string {
	maxGraphWidth := uint(53)
	maxNameLen := 0
	maxChangeLen := 0

	scaleLinear := func(it, width, maxVal uint) uint {
		if it == 0 || maxVal == 0 {
			return 0
		}

		return 1 + (it * (width - 1) / maxVal)
	}

	for _, fs := range fileStats {
		if len(fs.Name) > maxNameLen {
			maxNameLen = len(fs.Name)
		}

		changes := strconv.Itoa(fs.Addition + fs.Deletion)
		if len(changes) > maxChangeLen {
			maxChangeLen = len(changes)
		}
	}

	var result strings.Builder
	for _, fs := range fileStats {
		add := uint(fs.Addition)
		del := uint(fs.Deletion)
		np := maxNameLen - len(fs.Name)
		cp := maxChangeLen - len(strconv.Itoa(fs.Addition+fs.Deletion))

		total := add + del
		if total > maxGraphWidth {
			add = scaleLinear(add, maxGraphWidth, total)
			del = scaleLinear(del, maxGraphWidth, total)
		}

		adds := strings.Repeat("+", int(add))
		dels := strings.Repeat("-", int(del))
		namePad := strings.Repeat(" ", np)
		changePad := strings.Repeat(" ", cp)

		fmt.Fprintf(&result, " %s%s | %s%d %s%s\n", fs.Name, namePad, changePad, total, adds, dels)
	}
	return result.String()
}

func getFileStatsFromFilePatches(filePatches []fdiff.FilePatch) FileStats {
	fileStats := make(FileStats, 0, len(filePatches))

	for _, fp := range filePatches {
		if fp.IsBinary() {
			continue
		}

		cs := FileStat{}
		from, to := fp.Files()
		switch {
		case from == nil:
			// New File is created.
			cs.Name = to.Path()
		case to == nil:
			// File is deleted.
			cs.Name = from.Path()
		case from.Path() != to.Path():
			// File is renamed.
			cs.Name = fmt.Sprintf("%s => %s", from.Path(), to.Path())
		default:
			cs.Name = from.Path()
		}

		for edit := range fp.Diff().Edits() {
			cs.Addition += edit.After.End - edit.After.Start
			cs.Deletion += edit.Before.End - edit.Before.Start
		}

		fileStats = append(fileStats, cs)
	}

	return fileStats
}
