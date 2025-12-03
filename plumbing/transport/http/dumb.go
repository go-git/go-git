package http

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func (s *HTTPSession) fetchDumb(ctx context.Context, req *transport.FetchRequest) error {
	if req.Depth != 0 {
		return errors.New("dumb http protocol does not support shallow capabilities")
	}

	fsi, ok := s.st.(interface {
		Filesystem() billy.Filesystem
	})
	if !ok {
		return errors.New("dumb http protocol requires a filesystem")
	}

	repoFs := fsi.Filesystem()
	r := newFetchWalker(s, ctx, repoFs)
	if err := r.process(); err != nil {
		return err
	}

	if err := r.fetch(); err != nil {
		return fmt.Errorf("error fetching objects: %w", err)
	}

	return nil
}

// fetchWalker implements the Dumb protocol for fetching objects.
type fetchWalker struct {
	*HTTPSession
	ctx     context.Context
	fs      billy.Filesystem
	queue   []plumbing.Hash
	packIdx map[plumbing.Hash]string
}

func newFetchWalker(s *HTTPSession, ctx context.Context, fs billy.Filesystem) *fetchWalker {
	walker := new(fetchWalker)
	walker.HTTPSession = s
	walker.ctx = ctx
	walker.fs = fs
	walker.queue = make([]plumbing.Hash, 0)
	walker.packIdx = make(map[plumbing.Hash]string)
	return walker
}

func (r *fetchWalker) getInfoPacks() ([]string, error) {
	url, err := url.JoinPath(r.ep.String(), "objects", "info", "packs")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	applyHeaders(req, "", r.ep, r.auth, "", false)
	res, err := doRequest(r.client, req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusNotFound:
		return nil, transport.ErrRepositoryNotFound
	default:
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	var packs []string
	s := bufio.NewScanner(res.Body)
	for s.Scan() {
		line := s.Text()
		hash := strings.TrimPrefix(line, "P pack-")
		hash = strings.TrimSuffix(hash, ".pack")
		packs = append(packs, hash)
	}

	return packs, s.Err()
}

// downloadFile downloads a file from the server and saves it to the filesystem.
func (r *fetchWalker) downloadFile(fp string) (rErr error) {
	url, err := url.JoinPath(r.ep.String(), fp)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	applyHeaders(req, "", r.ep, r.auth, "", false)
	res, err := doRequest(r.client, req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	copyFn := func(w io.Writer) error {
		if _, err := ioutil.CopyBufferPool(w, res.Body); err != nil {
			return err
		}

		if err := res.Body.Close(); err != nil {
			return err
		}

		if closer, ok := w.(io.Closer); ok {
			return closer.Close()
		}

		return nil
	}

	f, err := r.fs.TempFile(filepath.Dir(fp), filepath.Base(fp)+".temp")
	if err != nil {
		copyFn(io.Discard)
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			rErr = err
		}
	}()

	if err := copyFn(f); err != nil {
		return err
	}

	// TODO: support hardlinks and "core.createobject" configuration
	return r.fs.Rename(f.Name(), fp)
}

// getHead returns the HEAD reference from the server.
func (r *fetchWalker) getHead() (ref *plumbing.Reference, err error) {
	url, err := url.JoinPath(r.ep.String(), "HEAD")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	applyHeaders(req, "", r.ep, r.auth, "", false)
	res, err := doRequest(r.client, req)
	if err != nil {
		return nil, err
	}

	defer func() {
		if res.Body == nil {
			return
		}
		bodyErr := res.Body.Close()
		if err == nil {
			err = bodyErr
		}
	}()
	switch res.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusNotFound:
		return nil, transport.ErrRepositoryNotFound
	default:
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	s := bufio.NewScanner(res.Body)
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return nil, err
		}
		// EOF, no data
		return nil, transport.ErrRepositoryNotFound
	}

	line := s.Text()
	if target, found := strings.CutPrefix(line, "ref: "); found {
		return plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.ReferenceName(target)), nil
	}

	return plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(line)), nil
}

// process calculates the objects to fetch and downloads them.
func (r *fetchWalker) process() error {
	var head plumbing.Hash
	if r.refs.Head == nil {
		hash, err := r.getHead()
		if err != nil {
			return err
		}

		switch hash.Type() {
		case plumbing.HashReference:
			head = hash.Hash()
			r.refs.Head = &head
		case plumbing.SymbolicReference:
			for name, h := range r.refs.References {
				if name == hash.Target().String() {
					head = h
					break
				}
			}
		}
	} else {
		head = *r.refs.Head
	}

	if head.IsZero() {
		// TODO: better error message?
		return transport.ErrRepositoryNotFound
	}

	infoPacks, err := r.getInfoPacks()
	if err != nil {
		return err
	}

	for _, hash := range infoPacks {
		h := plumbing.NewHash(hash)
		if h.IsZero() {
			continue
		}

		// XXX: we need to check if the index file exists. Currently, there is
		// no way to do so using the storer interfaces except using
		// HasEncodedObject which might be an expensive operation.
		packIdx := path.Join("objects", "pack", fmt.Sprintf("pack-%s.idx", hash))
		if _, err := r.fs.Stat(packIdx); errors.Is(err, fs.ErrExist) {
			r.packIdx[h] = packIdx
		} else {
			if err := r.downloadFile(packIdx); err != nil {
				return err
			}

			// TODO: parse and checksum the index file
			r.packIdx[h] = packIdx
		}
	}

	r.queue = append(r.queue, head)
	for name, hash := range r.refs.References {
		peeled, hasPeeled := r.refs.Peeled[name]
		if r.st.HasEncodedObject(hash) != nil {
			r.queue = append(r.queue, hash)
		}
		if hasPeeled && r.st.HasEncodedObject(peeled) != nil {
			r.queue = append(r.queue, peeled)
		}
	}

	r.queue = append(r.queue, head)

	return nil
}

func (r *fetchWalker) fetchObject(hash plumbing.Hash, obj plumbing.EncodedObject) (err error) {
	if r.st.HasEncodedObject(hash) == nil {
		return nil
	}

	h := hash.String()
	url, err := url.JoinPath(r.ep.String(), "objects", h[:2], h[2:])
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	applyHeaders(req, "", r.ep, r.auth, "", false)
	res, err := doRequest(r.client, req)
	if errors.Is(err, transport.ErrRepositoryNotFound) {
		// TODO: better error handling
		return io.EOF
	}
	if err != nil {
		return err
	}

	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return io.EOF
	default:
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	rd, err := objfile.NewReader(res.Body)
	if err != nil {
		return err
	}

	ioutil.CheckClose(rd, &err)

	t, size, err := rd.Header()
	if err != nil {
		return err
	}

	obj.SetType(t)
	obj.SetSize(size)

	w, err := obj.Writer()
	if err != nil {
		return err
	}

	ioutil.CheckClose(w, &err)

	if _, err := ioutil.CopyBufferPool(w, rd); err != nil {
		return err
	}

	return nil
}

func (r *fetchWalker) fetch() error {
	packs := map[string]struct{}{}
	processed := map[string]struct{}{}
	indicies := []*idxfile.MemoryIndex{}

LOOP:
	for len(r.queue) > 0 {
		objHash := r.queue[0]
		r.queue = r.queue[1:]
		if _, ok := processed[objHash.String()]; ok {
			continue
		}

		for _, idx := range indicies {
			if ok, err := idx.Contains(objHash); err == nil && ok {
				continue LOOP
			}
		}

		obj := r.st.NewEncodedObject()
		err := r.fetchObject(objHash, obj)
		if errors.Is(err, io.EOF) {
			// TODO: support http-alternates
			for packHash, packIdxPath := range r.packIdx {
				idxFile, err := r.fs.Open(packIdxPath)
				if err != nil {
					return fmt.Errorf("error opening index file: %w", err)
				}

				idx := idxfile.NewMemoryIndex(packHash.Size())
				d := idxfile.NewDecoder(idxFile)
				if err := d.Decode(idx); err != nil {
					_ = idxFile.Close()
					return fmt.Errorf("error decoding index file: %w", err)
				}

				indicies = append(indicies, idx)
				packPath := path.Join("objects", "pack", fmt.Sprintf("pack-%s.pack", packHash.String()))
				if ok, err := idx.Contains(objHash); err == nil && ok {
					processed[objHash.String()] = struct{}{}
					if _, ok := packs[packPath]; ok {
						continue LOOP
					}

					if _, err := r.fs.Stat(packPath); errors.Is(err, fs.ErrExist) {
						packs[packPath] = struct{}{}
						continue LOOP
					}

					if err := r.downloadFile(packPath); err != nil {
						return fmt.Errorf("error downloading pack file: %w", err)
					}

					packs[packPath] = struct{}{}
					continue LOOP
				}
			}
		} else if err != nil {
			return err
		}

		switch obj.Type() {
		case plumbing.CommitObject:
			commit, err := object.DecodeCommit(r.st, obj)
			if err != nil {
				return err
			}

			r.queue = append(r.queue, commit.ParentHashes...)
			r.queue = append(r.queue, commit.TreeHash)
		case plumbing.TreeObject:
			tree, err := object.DecodeTree(r.st, obj)
			if err != nil {
				return err
			}

			r.queue = append(r.queue, tree.Hash)
			for _, e := range tree.Entries {
				r.queue = append(r.queue, e.Hash)
			}
		case plumbing.TagObject:
			tag, err := object.DecodeTag(r.st, obj)
			if err != nil {
				return err
			}

			r.queue = append(r.queue, tag.Hash)
			r.queue = append(r.queue, tag.Target)
		case plumbing.BlobObject:
			blob, err := object.DecodeBlob(obj)
			if err != nil {
				return err
			}

			r.queue = append(r.queue, blob.Hash)
		default:
			return plumbing.ErrInvalidType
		}

		if _, err := r.st.SetEncodedObject(obj); err != nil {
			return err
		}
		processed[objHash.String()] = struct{}{}
	}

	for packPath := range packs {
		f, err := r.fs.Open(packPath)
		if err != nil {
			return err
		}

		if err := packfile.UpdateObjectStorage(r.st, f); err != nil {
			_ = f.Close()
			return err
		}

		if err := f.Close(); err != nil {
			return err
		}
	}

	return nil
}
