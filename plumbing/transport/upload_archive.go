package transport

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/internal/archive"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
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

	// Unreachable Git objects (i.e. objects not referenced by any refs or unreachable
	// from the commit graph) are intentionally not supported due to security concerns.
	//
	// Support for handling such objects may be reconsidered in the future if a safe
	// and performant approach is established.
	allowUnreachable := false

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
				return muxError(mux, w, fmt.Errorf("%s requires an argument", arg))
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
				if treeish == "" {
					treeish = arg
				}
			} else {
				if feature := unsupportedArchiveFeature(arg); feature != "" {
					return muxError(mux, w, fmt.Errorf("unsupported feature: %s", feature))
				}
				return muxError(mux, w, fmt.Errorf("unknown option: %s", arg))
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
		// List-only mode: just write the list of supported formats.
		for _, f := range archive.SupportedFormats() {
			if _, err := fmt.Fprintf(mux, "%s\n", f); err != nil {
				return err
			}
		}
		if err := pktline.WriteFlush(w); err != nil {
			return fmt.Errorf("upload-archive: writing flush: %w", err)
		}
		return nil
	}

	if treeish == "" {
		return muxError(mux, w, fmt.Errorf("no tree-ish specified"))
	}

	tree, commitHash, commitTime, err := archive.ResolveTreeish(st, treeish, allowUnreachable)
	if err != nil {
		return muxError(mux, w, err)
	}

	if err = archive.WriteArchive(st, mux, tree, commitHash, commitTime, format, prefix, paths); err != nil {
		return muxError(mux, w, err)
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

// muxError writes an error to the sideband error channel and flushes.
// Returns the original error for convenience.
func muxError(mux *sideband.Muxer, w io.Writer, err error) error {
	errMsg := fmt.Sprintf("upload-archive: %s", err.Error())
	_, _ = mux.WriteChannel(sideband.ErrorMessage, []byte(errMsg))
	_ = pktline.WriteFlush(w)
	return err
}

func unsupportedArchiveFeature(arg string) string {
	switch {
	case arg == "--worktree-attributes":
		return "export-ignore / export-subst"
	case arg == "--add-file" || strings.HasPrefix(arg, "--add-file="):
		return "--add-file"
	case arg == "--add-virtual-file" || strings.HasPrefix(arg, "--add-virtual-file="):
		return "--add-virtual-file"
	case arg == "--mtime" || strings.HasPrefix(arg, "--mtime="):
		return "--mtime"
	case len(arg) == 2 && arg[0] == '-' && arg[1] >= '0' && arg[1] <= '9':
		return "archive backend compression options"
	}

	return ""
}
