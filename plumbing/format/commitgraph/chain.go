package commitgraph

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/utils/trace"
)

// OpenChainFile reads a commit chain file and returns a slice of the hashes within it
//
// Commit-Graph chains are described at https://git-scm.com/docs/commit-graph
// and are new line separated list of graph file hashes, oldest to newest.
//
// This function simply reads the file and returns the hashes as a slice.
// A hash of the other object format's width is rejected with
// ErrObjectFormatMismatch.
func OpenChainFile(r io.Reader, opts ...Option) ([]string, error) {
	o, err := readOptions(opts)
	if err != nil {
		return nil, err
	}
	chain, err := readChain(r, o, -1)
	if err != nil {
		return nil, err
	}
	return chain, nil
}

// readChain returns the graph hashes r lists, up to the first line that
// fails to read or is not a hash of the configured object format. The
// error describes that line; the hashes before it are returned with it.
// If maxLines is not negative, at most that many lines are read and the
// rest of r is ignored.
func readChain(r io.Reader, o options, maxLines int) ([]string, error) {
	if r == nil {
		return nil, io.ErrUnexpectedEOF
	}
	bufRd := bufio.NewReader(r)
	chain := make([]string, 0, 8)
	for maxLines < 0 || len(chain) < maxLines {
		line, err := bufRd.ReadSlice('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return chain, err
		}

		hashStr := string(line[:len(line)-1])
		if !plumbing.IsHash(hashStr) {
			return chain, ErrMalformedCommitGraphFile
		}
		if len(hashStr) != o.objectFormat.HexSize() {
			return chain, fmt.Errorf("%w: chain names %d-character graph hash, want %s", ErrObjectFormatMismatch, len(hashStr), o.objectFormat)
		}
		chain = append(chain, hashStr)
	}
	return chain, nil
}

// OpenChainOrFileIndex expects a billy.Filesystem representing a .git directory.
// It will first attempt to read a commit-graph index file, before trying to read a
// commit-graph chain file and its index files. Like Git, it falls back to the
// chain whenever the index file cannot be opened or read. If the index file
// exists but is unusable and there is no chain, the index file's error is
// returned; if the chain is there but cannot be loaded, both errors are.
// Such a joined error can then also match os.ErrNotExist, from a missing
// layer of the chain. Otherwise an Index will be returned.
//
// See: https://git-scm.com/docs/commit-graph
func OpenChainOrFileIndex(fs billy.Filesystem, opts ...Option) (Index, error) {
	if _, err := readOptions(opts); err != nil {
		return nil, err
	}
	index, fileErr := openFileIndex(fs, opts...)
	if fileErr == nil {
		return index, nil
	}

	// Only a chain file that is not there means there is no chain; a
	// not-exist error from loading the chain's layers, such as the one
	// ErrIncompleteChain wraps for a missing layer, is reported as is.
	chainFile, err := openChainFile(fs)
	if err != nil {
		switch {
		case errors.Is(fileErr, os.ErrNotExist):
			return nil, err
		case errors.Is(err, os.ErrNotExist):
			return nil, fileErr
		}
		return nil, errors.Join(fileErr, err)
	}
	index, err = openChain(fs, chainFile, opts...)
	if err != nil && !errors.Is(fileErr, os.ErrNotExist) {
		return nil, errors.Join(fileErr, err)
	}
	return index, err
}

func openChainFile(fs billy.Filesystem) (billy.File, error) {
	return fs.Open(path.Join("objects", "info", "commit-graphs", "commit-graph-chain"))
}

func openFileIndex(fs billy.Filesystem, opts ...Option) (Index, error) {
	file, err := fs.Open(path.Join("objects", "info", "commit-graph"))
	if err != nil {
		return nil, err
	}

	index, err := OpenFileIndex(file, opts...)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return index, nil
}

// OpenChainIndex expects a billy.Filesystem representing a .git directory.
// It will read a commit-graph chain file and return a coalesced index.
//
// Like Git, it keeps the layers that load and stops at the first chain
// entry that is not a valid hash, or whose graph is missing, invalid or
// does not match the layers below it; the commits in the layers above are
// then simply not in the index, and a warning is traced. With
// WithStrictChain it returns ErrIncompleteChain instead. If the chain file
// is not present, or its first layer cannot be loaded, an error is
// returned.
//
// As in Git, only the chain file's first size/(hash length+1) lines are
// read, so a remainder too short to hold another entry, such as a trailing
// blank line, is ignored.
//
// See: https://git-scm.com/docs/commit-graph
func OpenChainIndex(fs billy.Filesystem, opts ...Option) (Index, error) {
	if _, err := readOptions(opts); err != nil {
		return nil, err
	}
	chainFile, err := openChainFile(fs)
	if err != nil {
		return nil, err
	}
	return openChain(fs, chainFile, opts...)
}

// openChain loads the chain chainFile lists, and closes chainFile.
func openChain(fs billy.Filesystem, chainFile billy.File, opts ...Option) (Index, error) {
	o, err := readOptions(opts)
	if err != nil {
		_ = chainFile.Close()
		return nil, err
	}

	// Like Git's load_commit_graph_chain_fd_st, read only as many lines as
	// the file has room for whole entries, a hash and its newline; any
	// shorter remainder, such as a trailing blank line, is ignored.
	fi, err := chainFile.Stat()
	if err != nil {
		_ = chainFile.Close()
		return nil, err
	}
	maxLines := int(fi.Size() / int64(o.objectFormat.HexSize()+1))
	chain, readErr := readChain(chainFile, o, maxLines)
	_ = chainFile.Close()
	if len(chain) == 0 {
		if readErr == nil {
			readErr = ErrMalformedCommitGraphFile
		}
		return nil, readErr
	}

	// incomplete reports that the chain stops below layer n (from 1), and
	// why: an error with WithStrictChain, and a warning otherwise, as Git
	// gives one.
	incomplete := func(index Index, n int, cause error) (Index, error) {
		err := fmt.Errorf("%w: layer %d: %w", ErrIncompleteChain, n, cause)
		if o.strictChain {
			_ = index.Close()
			return nil, err
		}
		trace.General.Printf("commitgraph: %v", err)
		return index, nil
	}

	var index Index
	for i, hash := range chain {
		next, err := openChainLayer(fs, hash, index, opts...)
		if err == nil && i > 0 {
			// As in Git's add_graph_to_chain, a layer is added only while
			// the chain names the layers below it after their trailers.
			// The top layer's own name is never checked.
			err = checkLayerName(chain[i-1], index)
			if err != nil {
				_ = closeTopLayer(next)
			}
		}
		if err != nil {
			if index == nil {
				return nil, err
			}
			return incomplete(index, i+1, err)
		}
		index = next
	}
	if readErr != nil {
		return incomplete(index, len(chain)+1, readErr)
	}

	return index, nil
}

// closeTopLayer closes the file of layer, the top of a chain, leaving the
// layers below it open: closing layer itself would close them too.
func closeTopLayer(layer Index) error {
	return layer.(*fileIndex).reader.Close()
}

// checkLayerName reports whether the chain names layer, the top of the
// chain loaded so far, after the checksum in its trailer. The trailer is
// read, not recomputed, so this catches a stale or mismatched layer, not a
// forged one.
func checkLayerName(name string, layer Index) error {
	if want, _ := plumbing.FromHex(name); layer.(*fileIndex).graphOID != want {
		return ErrMalformedCommitGraphFile
	}
	return nil
}

// openChainLayer opens the graph the chain names hash on top of parent. On
// success the returned Index owns parent; on failure the caller keeps it.
func openChainLayer(fs billy.Filesystem, hash string, parent Index, opts ...Option) (Index, error) {
	file, err := fs.Open(path.Join("objects", "info", "commit-graphs", "graph-"+hash+".graph"))
	if err != nil {
		return nil, err
	}

	next, err := OpenFileIndexWithParent(file, parent, opts...)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return next, nil
}
