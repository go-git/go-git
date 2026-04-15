package transport

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// UploadArchiveRequest configures the server-side upload-archive service.
type UploadArchiveRequest struct{}

// UploadArchive is a server command that serves the git-upload-archive service.
//
// It reads argument pkt-lines from r, sends ACK + flush, then generates the
// archive and streams it to w using sideband multiplexing.
//
// Wire protocol:
//
//	Client → Server: "argument <arg>\n" pkt-lines + flush
//	Server → Client: "ACK\n" pkt-line + flush
//	Server → Client: sideband packets (band 1 = archive data, band 2 = progress)
func UploadArchive(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	_ *UploadArchiveRequest,
) error {
	w = ioutil.NewContextWriteCloser(ctx, w)

	// TODO: Derive allowUnreachable from global/system config as well, not
	// just repository config.
	var allowUnreachable bool
	if v, ok := readAllowUnreachable(st); ok {
		allowUnreachable = v
	}

	args, err := readArchiveArgs(r)
	if err != nil {
		writeNACK(w, err.Error())
		return err
	}

	if _, err := pktline.WriteString(w, "ACK\n"); err != nil {
		return fmt.Errorf("upload-archive: writing ACK: %w", err)
	}
	if err := pktline.WriteFlush(w); err != nil {
		return fmt.Errorf("upload-archive: writing flush: %w", err)
	}

	mux := sideband.NewMuxer(sideband.Sideband64k, w)

	if err := writeArchive(ctx, st, mux, args, allowUnreachable); err != nil {
		errMsg := fmt.Sprintf("upload-archive: %s", err.Error())
		_, _ = mux.WriteChannel(sideband.ErrorMessage, []byte(errMsg))
		_ = pktline.WriteFlush(w)
		return err
	}

	return pktline.WriteFlush(w)
}

const maxArchiveArgs = 64

// readArchiveArgs reads "argument <arg>\n" pkt-lines until flush.
func readArchiveArgs(r io.Reader) ([]string, error) {
	var args []string
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return nil, fmt.Errorf("upload-archive: reading argument: %w", err)
		}
		if l == pktline.Flush {
			break
		}
		if len(args) >= maxArchiveArgs {
			return nil, fmt.Errorf("upload-archive: too many arguments (>%d)", maxArchiveArgs)
		}

		s := strings.TrimSuffix(string(line), "\n")
		if !strings.HasPrefix(s, "argument ") {
			return nil, fmt.Errorf("upload-archive: expected 'argument' token, got: %s", s)
		}
		args = append(args, s[len("argument "):])
	}
	return args, nil
}

func writeNACK(w io.Writer, reason string) {
	_, _ = pktline.WriteString(w, fmt.Sprintf("NACK %s\n", reason))
	_ = pktline.WriteFlush(w)
}

// readAllowUnreachable reads the uploadArchive.allowUnreachable config
// from the repository. It returns the value and whether the key was
// explicitly set. When the key is absent or the config cannot be read,
// ok is false and the caller should fall back to its own default.
func readAllowUnreachable(st storage.Storer) (value, ok bool) {
	cfg, err := st.Config()
	if err != nil {
		return false, false
	}
	if cfg.UploadArchive.AllowUnreachable.IsSet() {
		return cfg.UploadArchive.AllowUnreachable.IsTrue(), true
	}
	return false, false
}

// Supported archive formats.
var supportedArchiveFormats = []string{"tar", "tar.gz", "tgz", "zip"}

// writeArchive generates an archive from the repository and writes it to
// the sideband muxer's PackData channel.
//
// Args follow the same format as git-archive: [options...] <tree-ish> [paths...]
// Supported options: --format=tar|zip|tar.gz|tgz, --prefix=<prefix>, --list/-l
func writeArchive(_ context.Context, st storage.Storer, mux *sideband.Muxer, args []string, allowUnreachable bool) error {
	format := "tar"
	prefix := ""
	var treeish string
	var paths []string
	list := false

	// Normalize arguments: convert "--format zip" to "--format=zip" etc.
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--format", "--prefix":
			i++
			if i >= len(args) {
				return fmt.Errorf("%s requires an argument", arg)
			}
			normalized = append(normalized, arg+"="+args[i])
		default:
			normalized = append(normalized, arg)
		}
	}

	for _, arg := range normalized {
		switch {
		case arg == "--list" || arg == "-l":
			list = true
		case strings.HasPrefix(arg, "--format="):
			format = arg[len("--format="):]
		case strings.HasPrefix(arg, "--prefix="):
			prefix = arg[len("--prefix="):]
		case arg == "--":
			// paths are handled below
		default:
			if !strings.HasPrefix(arg, "-") {
				treeish = arg
			} else {
				return fmt.Errorf("unknown option: %s", arg)
			}
		}
	}

	// Extract paths after treeish
	for i, arg := range normalized {
		if arg == treeish {
			if i+1 < len(normalized) {
				if normalized[i+1] == "--" {
					paths = normalized[i+2:]
				} else {
					paths = normalized[i+1:]
				}
			}
			break
		}
	}

	if list {
		for _, f := range supportedArchiveFormats {
			if _, err := fmt.Fprintf(mux, "%s\n", f); err != nil {
				return err
			}
		}
		return nil
	}

	if treeish == "" {
		return fmt.Errorf("no tree-ish specified")
	}

	tree, commitTime, err := resolveTreeish(st, treeish, allowUnreachable)
	if err != nil {
		return err
	}

	switch format {
	case "tar":
		return writeTarArchive(st, mux, tree, prefix, paths, commitTime)
	case "tar.gz", "tgz":
		gw := gzip.NewWriter(mux)
		if err := writeTarArchive(st, gw, tree, prefix, paths, commitTime); err != nil {
			return err
		}
		return gw.Close()
	case "zip":
		return writeZipArchive(st, mux, tree, prefix, paths, commitTime)
	default:
		return fmt.Errorf("unsupported archive format: %s", format)
	}
}

// resolveTreeish resolves a tree-ish expression to a tree object.
//
// Security: By default, only direct ref names (v1.0, main) and ref:path
// sub-tree syntax (v1.0:Documentation) are allowed. Raw SHA-1 hashes and
// relative expressions (main^, HEAD~2) are rejected unless
// allowUnreachable is true. See https://git-scm.com/docs/git-upload-archive
func resolveTreeish(st storage.Storer, treeish string, allowUnreachable bool) (*object.Tree, time.Time, error) {
	var subPath string
	if idx := strings.IndexByte(treeish, ':'); idx >= 0 {
		subPath = treeish[idx+1:]
		treeish = treeish[:idx]
	}

	if !allowUnreachable {
		if plumbing.IsHash(treeish) {
			return nil, time.Time{}, fmt.Errorf("upload-archive: only ref names are allowed (got %s)", treeish)
		}
		if strings.ContainsAny(treeish, "^~@{}") {
			return nil, time.Time{}, fmt.Errorf("upload-archive: relative expressions are not allowed (got %s)", treeish)
		}
	}

	h, err := resolveRef(st, treeish, allowUnreachable)
	if err != nil {
		return nil, time.Time{}, err
	}

	obj, err := object.GetObject(st, h)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("object not found: %s", treeish)
	}

	var commitTime time.Time
	var tree *object.Tree

	switch o := obj.(type) {
	case *object.Commit:
		commitTime = o.Committer.When
		tree, err = o.Tree()
		if err != nil {
			return nil, time.Time{}, err
		}
	case *object.Tag:
		commit, err := object.GetCommit(st, o.Target)
		if err != nil {
			return nil, time.Time{}, err
		}
		commitTime = commit.Committer.When
		tree, err = commit.Tree()
		if err != nil {
			return nil, time.Time{}, err
		}
	case *object.Tree:
		tree = o
		commitTime = time.Now()
	default:
		return nil, time.Time{}, fmt.Errorf("unsupported object type for archive: %T", obj)
	}

	if subPath != "" {
		entry, err := tree.FindEntry(subPath)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("path not found in tree: %s", subPath)
		}
		if entry.Mode != filemode.Dir {
			return nil, time.Time{}, fmt.Errorf("path is not a directory: %s", subPath)
		}
		tree, err = object.GetTree(st, entry.Hash)
		if err != nil {
			return nil, time.Time{}, err
		}
	}

	return tree, commitTime, nil
}

func resolveRef(st storage.Storer, name string, allowHash bool) (plumbing.Hash, error) {
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

// defaultUmask is the default tar umask (002).
const defaultUmask = 0o002

// applyUmask applies umask to the given mode for regular files.
// Returns mode with all permission bits set, then applies umask.
func applyUmask(mode int64, isExecutable bool) int64 {
	if isExecutable {
		return (mode | 0o777) &^ defaultUmask
	}
	return (mode | 0o666) &^ defaultUmask
}

// applyUmaskDir applies umask to directories.
// Directories always get full permissions minus umask.
func applyUmaskDir(mode int64) int64 {
	return (mode | 0o777) &^ defaultUmask
}

func writeTarArchive(st storage.Storer, w io.Writer, tree *object.Tree, prefix string, pathFilter []string, modTime time.Time) error {
	tw := tar.NewWriter(w)

	if prefix != "" && strings.HasSuffix(prefix, "/") {
		_ = tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     prefix,
			Mode:     applyUmaskDir(0),
			ModTime:  modTime,
		})
	}

	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if len(pathFilter) > 0 && !matchesPathFilter(name, pathFilter) {
			continue
		}

		fullName := prefix + name

		// Extract Unix permission bits from git mode.
		unixMode := int64(entry.Mode) & 0o777

		if entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule {
			_ = tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     fullName + "/",
				Mode:     applyUmaskDir(unixMode),
				ModTime:  modTime,
			})
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
			target, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return err
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
		hdr.Mode = applyUmask(unixMode, isExec)

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		rc, err := blob.Reader()
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}

	return tw.Close()
}

func writeZipArchive(st storage.Storer, w io.Writer, tree *object.Tree, prefix string, pathFilter []string, modTime time.Time) error {
	zw := zip.NewWriter(w)

	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if len(pathFilter) > 0 && !matchesPathFilter(name, pathFilter) {
			continue
		}

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
			fh.SetMode(fs.FileMode(applyUmask(unixMode, true)))
		case filemode.Symlink:
			// Zip stores symlinks with mode 0o120000 + permissions.
			fh.SetMode(fs.FileMode(0o120000 | (applyUmask(unixMode, true) & 0o777)))
		default:
			fh.SetMode(fs.FileMode(applyUmask(unixMode, false)))
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
		_ = rc.Close()
		if err != nil {
			return err
		}
	}

	return zw.Close()
}

func matchesPathFilter(name string, filters []string) bool {
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
