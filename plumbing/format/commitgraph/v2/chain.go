package v2

import (
	"bufio"
	"io"
	"path"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// OpenChainFile reads a commit chain file and returns a slice of the hashes within it
//
// Commit-Graph chains are described at https://git-scm.com/docs/commit-graph
// and are new line separated list of graph file hashes, oldest to newest.
//
// This function simply reads the file and returns the hashes as a slice.
func OpenChainFile(r io.Reader) ([]string, error) {
	if r == nil {
		return nil, io.ErrUnexpectedEOF
	}
	bufRd := bufio.NewReader(r)
	chain := make([]string, 0, 8)
	for {
		line, err := bufRd.ReadSlice('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		hashStr := string(line[:len(line)-1])
		if !plumbing.IsHash(hashStr) {
			return nil, ErrMalformedCommitGraphFile
		}
		chain = append(chain, hashStr)
	}
	return chain, nil
}

// OpenChainOrFileIndex expects a billy.Filesystem representing a .git directory.
// It will first attempt to read a commit-graph index file, before trying to read a
// commit-graph chain file and its index files. If neither are present, an error is returned.
// Otherwise an Index will be returned.
//
// See: https://git-scm.com/docs/commit-graph
func OpenChainOrFileIndex(fs billy.Filesystem) (Index, error) {
	file, err := fs.Open(path.Join("objects", "info", "commit-graph"))
	if err != nil {
		// try to open a chain file
		return OpenChainIndex(fs)
	}

	index, err := OpenFileIndex(file)
	if err != nil {
		// Ignore any file closing errors and return the error from OpenFileIndex instead
		_ = file.Close()
		return nil, err
	}
	return index, nil
}

// OpenChainIndex expects a billy.Filesystem representing a .git directory.
// It will read a commit-graph chain file and return a coalesced index.
// If the chain file or a graph in that chain is not present, an error is returned.
//
// See: https://git-scm.com/docs/commit-graph
func OpenChainIndex(fs billy.Filesystem) (Index, error) {
	chainFile, err := fs.Open(path.Join("objects", "info", "commit-graphs", "commit-graph-chain"))
	if err != nil {
		return nil, err
	}

	chain, err := OpenChainFile(chainFile)
	_ = chainFile.Close()
	if err != nil {
		return nil, err
	}

	var index Index
	for _, hash := range chain {

		file, err := fs.Open(path.Join("objects", "info", "commit-graphs", "graph-"+hash+".graph"))
		if err != nil {
			// Ignore all other file closing errors and return the error from opening the last file in the graph
			_ = index.Close()
			return nil, err
		}

		index, err = OpenFileIndexWithParent(file, index)
		if err != nil {
			// Ignore file closing errors and return the error from OpenFileIndex instead
			_ = index.Close()
			return nil, err
		}
	}

	return index, nil
}
