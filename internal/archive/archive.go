// Package archive provides archive generation functionality for git-upload-archive.
// It supports tar and zip formats with proper security controls.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// PAXGlobalHeader is the name used for PAX global extended headers in
// git-generated tar archives. This matches the name used by canonical git.
const PAXGlobalHeader = "pax_global_header"

// DefaultUmask is the default tar umask (002).
const DefaultUmask = 0o002

// Exported errors for archive operations.
var (
	// ErrOnlyRefNames is returned when raw hashes or relative expressions are used
	// but allowUnreachable is not set.
	ErrOnlyRefNames = errors.New("only ref names are allowed")

	// ErrRelativeExpressions is returned when relative ref expressions are used
	// but allowUnreachable is not set.
	ErrRelativeExpressions = errors.New("relative expressions are not allowed")

	// ErrObjectNotFound is returned when the specified object cannot be found.
	ErrObjectNotFound = errors.New("object not found")

	// ErrUnsupportedObjectType is returned when the object type is not supported for archiving.
	ErrUnsupportedObjectType = errors.New("unsupported object type for archive")

	// ErrPathNotFound is returned when a sub-path is not found in the tree.
	ErrPathNotFound = errors.New("path not found in tree")

	// ErrPathNotDirectory is returned when a sub-path is not a directory.
	ErrPathNotDirectory = errors.New("path is not a directory")

	// ErrPathspecNoMatch is returned when pathspec filters don't match any files.
	ErrPathspecNoMatch = errors.New("pathspec did not match any files")

	// ErrSymlinkTargetTooLarge is returned when a symlink blob exceeds the
	// maximum size accepted by the tar archive writer.
	ErrSymlinkTargetTooLarge = errors.New("symlink target too large")

	// ErrInvalidPrefix is returned when the requested archive prefix contains
	// path traversal sequences.
	ErrInvalidPrefix = errors.New("invalid archive prefix")

	// ErrUnsupportedFormat is returned when the requested archive format is
	// not supported.
	ErrUnsupportedFormat = errors.New("unsupported archive format")
)

const maxTarSymlinkTargetSize = 64 * 1024

// SupportedFormats returns the list of supported archive formats.
func SupportedFormats() []string {
	return []string{"tar", "tar.gz", "tgz", "zip"}
}

// ApplyUmask applies umask to the given mode for regular files.
// Returns mode with all permission bits set, then applies umask.
func ApplyUmask(mode int64, isExecutable bool) int64 {
	if isExecutable {
		return (mode | 0o777) &^ DefaultUmask
	}
	return (mode | 0o666) &^ DefaultUmask
}

// ApplyUmaskDir applies umask to directories.
// Directories always get full permissions minus umask.
func ApplyUmaskDir(mode int64) int64 {
	return (mode | 0o777) &^ DefaultUmask
}

// ResolveTreeish resolves a tree-ish expression to a tree object.
//
// Security: By default, only direct ref names (v1.0, main) and ref:path
// sub-tree syntax (v1.0:Documentation) are allowed. Raw SHA-1 hashes and
// relative expressions (main^, HEAD~2) are rejected unless
// allowUnreachable is true. See https://git-scm.com/docs/git-upload-archive
//
// Returns the tree, commit hash (if applicable), commit time, and any error.
func ResolveTreeish(st storage.Storer, treeish string, allowUnreachable bool) (*object.Tree, *plumbing.Hash, time.Time, error) {
	var subPath string
	if idx := strings.IndexByte(treeish, ':'); idx >= 0 {
		subPath = treeish[idx+1:]
		treeish = treeish[:idx]
	}

	if !allowUnreachable {
		if plumbing.IsHash(treeish) {
			return nil, nil, time.Time{}, fmt.Errorf("%w (got %s)", ErrOnlyRefNames, treeish)
		}
		if strings.ContainsAny(treeish, "^~@{}") {
			return nil, nil, time.Time{}, fmt.Errorf("%w (got %s)", ErrRelativeExpressions, treeish)
		}
	}

	h, err := ResolveRef(st, treeish, allowUnreachable)
	if err != nil {
		return nil, nil, time.Time{}, err
	}

	obj, err := object.GetObject(st, h)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("%w: %s", ErrObjectNotFound, treeish)
	}

	for {
		tag, ok := obj.(*object.Tag)
		if !ok {
			break
		}

		obj, err = object.GetObject(st, tag.Target)
		if err != nil {
			return nil, nil, time.Time{}, fmt.Errorf("resolve annotated tag: %w", err)
		}
	}

	var commitHash *plumbing.Hash
	var commitTime time.Time
	var tree *object.Tree

	switch o := obj.(type) {
	case *object.Commit:
		commitHash = &o.Hash
		commitTime = o.Committer.When
		tree, err = o.Tree()
		if err != nil {
			return nil, nil, time.Time{}, err
		}
	case *object.Tree:
		tree = o

		// git archive behaves differently when given a tree ID versus when
		// given a commit ID or tag ID. In the first case the current time is
		// used as the modification time of each file in the archive. In the
		// latter case the commit time as recorded in the referenced commit
		// object is used instead.
		//
		// See https://git-scm.com/docs/git-archive
		commitTime = time.Now()
	default:
		return nil, nil, time.Time{}, fmt.Errorf("unsupported object type for archive: %T", obj)
	}

	if subPath != "" {
		entry, err := tree.FindEntry(subPath)
		if err != nil {
			return nil, nil, time.Time{}, fmt.Errorf("path not found in tree: %s", subPath)
		}
		if entry.Mode != filemode.Dir {
			return nil, nil, time.Time{}, fmt.Errorf("path is not a directory: %s", subPath)
		}
		tree, err = object.GetTree(st, entry.Hash)
		if err != nil {
			return nil, nil, time.Time{}, err
		}
	}

	return tree, commitHash, commitTime, nil
}

// ResolveRef resolves a ref name to a hash.
func ResolveRef(st storage.Storer, name string, allowHash bool) (plumbing.Hash, error) {
	if allowHash && plumbing.IsHash(name) {
		return plumbing.NewHash(name), nil
	}

	for _, candidate := range []plumbing.ReferenceName{
		plumbing.ReferenceName(name),
		plumbing.ReferenceName("refs/heads/" + name),
		plumbing.ReferenceName("refs/tags/" + name),
	} {
		ref, err := storer.ResolveReference(st, candidate)
		if err == nil {
			return ref.Hash(), nil
		}
	}

	return plumbing.ZeroHash, fmt.Errorf("cannot resolve %q", name)
}

// WriteTarArchive writes a tar archive from a tree.
func WriteTarArchive(st storage.Storer, w io.Writer, tree *object.Tree, commitHash *plumbing.Hash, prefix string, pathFilter []string, modTime time.Time) error {
	tw := tar.NewWriter(w)

	// Write PAX global extended header with commit ID if available.
	// This matches the behavior of git archive and allows extraction
	// via git get-tar-commit-id.
	if commitHash != nil {
		err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeXGlobalHeader,
			Name:     PAXGlobalHeader,
			PAXRecords: map[string]string{
				"comment": commitHash.String(),
			},
		})
		if err != nil {
			return fmt.Errorf("writing global PAX header: %w", err)
		}
	}

	if prefix != "" && strings.HasSuffix(prefix, "/") {
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     prefix,
			Mode:     ApplyUmaskDir(0),
			ModTime:  modTime,
		}); err != nil {
			return err
		}
	}

	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	var matchedAny bool
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if len(pathFilter) > 0 && !MatchesPathFilter(name, pathFilter) {
			continue
		}
		matchedAny = true

		fullName := prefix + name

		// Extract Unix permission bits from git mode.
		unixMode := int64(entry.Mode) & 0o777

		if entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule {
			if err := tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     fullName + "/",
				Mode:     ApplyUmaskDir(unixMode),
				ModTime:  modTime,
			}); err != nil {
				return err
			}
			continue
		}

		blob, err := object.GetBlob(st, entry.Hash)
		if err != nil {
			return err
		}

		hdr := &tar.Header{
			Name:    fullName,
			Size:    blob.Size,
			Mode:    unixMode,
			ModTime: modTime,
		}

		if entry.Mode == filemode.Symlink {
			rc, err := blob.Reader()
			if err != nil {
				return err
			}
			target, err := io.ReadAll(io.LimitReader(rc, maxTarSymlinkTargetSize+1))
			closeErr := rc.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if len(target) > maxTarSymlinkTargetSize {
				return fmt.Errorf("%w: %s (%d bytes)", ErrSymlinkTargetTooLarge, fullName, len(target))
			}
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = string(target)
			hdr.Size = 0
			// Symlinks always get 0777 per canonical git.
			hdr.Mode = 0o777
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			continue
		}

		isExec := entry.Mode == filemode.Executable
		hdr.Mode = ApplyUmask(unixMode, isExec)

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		rc, err := blob.Reader()
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, rc)
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}

	if len(pathFilter) > 0 && !matchedAny {
		return fmt.Errorf("%w: '%s'", ErrPathspecNoMatch, strings.Join(pathFilter, " "))
	}

	return tw.Close()
}

// WriteZipArchive writes a zip archive from a tree.
func WriteZipArchive(st storage.Storer, w io.Writer, tree *object.Tree, commitHash *plumbing.Hash, prefix string, pathFilter []string, modTime time.Time) error {
	zw := zip.NewWriter(w)

	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	var matchedAny bool
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if len(pathFilter) > 0 && !MatchesPathFilter(name, pathFilter) {
			continue
		}
		matchedAny = true

		if entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule {
			continue
		}

		fullName := prefix + name
		blob, err := object.GetBlob(st, entry.Hash)
		if err != nil {
			return err
		}

		// Extract Unix permission bits from git mode and apply default umask.
		unixMode := int64(entry.Mode) & 0o777

		fh := &zip.FileHeader{
			Name:     fullName,
			Method:   zip.Deflate,
			Modified: modTime,
		}
		switch entry.Mode {
		case filemode.Executable:
			fh.SetMode(fs.FileMode(ApplyUmask(unixMode, true)))
		case filemode.Symlink:
			// Zip stores symlinks with mode 0o120000 + permissions.
			fh.SetMode(fs.FileMode(0o120000 | (ApplyUmask(unixMode, true) & 0o777)))
		default:
			fh.SetMode(fs.FileMode(ApplyUmask(unixMode, false)))
		}

		fw, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}

		rc, err := blob.Reader()
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, rc)
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}

	if len(pathFilter) > 0 && !matchedAny {
		return fmt.Errorf("pathspec '%s' did not match any files", strings.Join(pathFilter, " "))
	}

	// Store commit ID as ZIP file comment if available.
	// This matches the behavior of git archive.
	if commitHash != nil {
		if err := zw.SetComment(commitHash.String()); err != nil {
			return err
		}
	}

	return zw.Close()
}

// MatchesPathFilter checks if a name matches any of the path filters.
//
// Note: This function assumes paths use forward slashes (/), which is the
// format used by Git internally. The TreeWalker produces paths with forward
// slashes regardless of the operating system, so this function is
// platform-independent.
//
// Supported patterns:
//   - Exact match: "README.md"
//   - Prefix match: "docs/" matches "docs/guide.md"
//   - Glob patterns: "*.go" matches "main.go"
//   - Parent-child: "docs/guide.md" matches parent "docs"
func MatchesPathFilter(name string, filters []string) bool {
	for _, f := range filters {
		if name == f || strings.HasPrefix(name, f+"/") || strings.HasPrefix(f, name+"/") {
			return true
		}
		matched, _ := path.Match(f, name)
		if matched {
			return true
		}
	}
	return false
}

// GetTarCommitID extracts the commit ID from a git-generated tar archive.
// It reads the PAX global extended header from the beginning of the archive
// and returns the value of the "comment" field, which contains the commit hash.
// If no global header is found or it doesn't contain a comment, it returns
// an error.
func GetTarCommitID(r io.Reader) (*plumbing.Hash, error) {
	tr := tar.NewReader(r)
	hdr, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("reading tar header: %w", err)
	}
	// Check for PAX global extended header (typeflag 'g')
	if hdr.Typeflag != tar.TypeXGlobalHeader {
		return nil, fmt.Errorf("expected global PAX header, got typeflag %c", hdr.Typeflag)
	}
	// The PAX records should contain the commit ID in the "comment" field
	comment, ok := hdr.PAXRecords["comment"]
	if !ok {
		return nil, fmt.Errorf("global header missing comment field")
	}
	hash := plumbing.NewHash(comment)
	return &hash, nil
}

// WriteArchive generates an archive from a resolved tree and writes it to w.
//
// Supported formats: tar, zip, tar.gz, tgz.
// The prefix is prepended to all file paths in the archive.
// The paths slice can be used to filter which files are included.
func WriteArchive(st storage.Storer, w io.Writer, tree *object.Tree, commitHash *plumbing.Hash, commitTime time.Time, format, prefix string, paths []string) error {
	if HasInvalidPrefix(prefix) {
		return fmt.Errorf("%w: %s", ErrInvalidPrefix, prefix)
	}

	switch format {
	case "tar":
		return WriteTarArchive(st, w, tree, commitHash, prefix, paths, commitTime)
	case "tar.gz", "tgz":
		gw := gzip.NewWriter(w)
		if err := WriteTarArchive(st, gw, tree, commitHash, prefix, paths, commitTime); err != nil {
			return err
		}
		return gw.Close()
	case "zip":
		return WriteZipArchive(st, w, tree, commitHash, prefix, paths, commitTime)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
}

// HasInvalidPrefix reports whether prefix contains path traversal
// sequences ("..") or starts with an absolute path separator ("/" or "\").
// Both local and remote archive paths should call this before accepting a
// user-supplied prefix.
func HasInvalidPrefix(prefix string) bool {
	if strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "\\") {
		return true
	}
	return slices.Contains(strings.FieldsFunc(prefix, func(r rune) bool {
		return r == '/' || r == '\\'
	}), "..")
}
