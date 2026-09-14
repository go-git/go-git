package http

import (
	"bufio"
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func (s *dumbPackSession) fetchDumb(ctx context.Context, st storage.Storer, req *transport.FetchRequest) error {
	if req.Depth != 0 {
		return errors.New("dumb http protocol does not support shallow capabilities")
	}

	fsi, ok := st.(interface {
		Filesystem() billy.Filesystem
	})
	if !ok {
		return errors.New("dumb http protocol requires a filesystem")
	}

	repoFs := fsi.Filesystem()
	r := newFetchWalker(ctx, s, st, repoFs)
	if err := r.process(req.Wants); err != nil {
		return err
	}

	if err := r.fetch(); err != nil {
		return fmt.Errorf("error fetching objects: %w", err)
	}

	return nil
}

type fetchWalker struct {
	ctx        context.Context
	client     *http.Client
	baseURL    *url.URL
	authorizer Authorizer
	st         storage.Storer
	refs       *packp.AdvRefs
	fs         billy.Filesystem
	queue      []plumbing.Hash
	packIdx    map[plumbing.Hash]string
	dropped    *redirectRecord
}

func newFetchWalker(ctx context.Context, s *dumbPackSession, st storage.Storer, fs billy.Filesystem) *fetchWalker {
	return &fetchWalker{
		ctx:        ctx,
		client:     s.client,
		baseURL:    s.baseURL,
		authorizer: s.authorizer,
		st:         st,
		refs:       s.refs,
		fs:         fs,
		queue:      make([]plumbing.Hash, 0),
		packIdx:    make(map[plumbing.Hash]string),
		dropped:    s.dropped,
	}
}

func (r *fetchWalker) httpGet(urlPath string) (*http.Response, error) {
	u, err := url.JoinPath(r.baseURL.String(), urlPath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if err := applyAuth(req, r.authorizer); err != nil {
		return nil, err
	}
	resp, err := doRequest(r.client, req)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, wrapDropped(r.dropped, err)
	}
	return resp, nil
}

func (r *fetchWalker) getInfoPacks() ([]string, error) {
	res, err := r.httpGet("objects/info/packs")
	if errors.Is(err, transport.ErrRepositoryNotFound) {
		// A repository whose objects are all loose has no objects/info/packs
		// to serve, and the dumb protocol treats its absence as "no packs".
		// The 404 is ordinary control flow here, the same way fetchObject
		// reads it for a loose object, so a repository that is otherwise
		// fetchable must not be refused as missing outright.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	var packs []string
	s := bufio.NewScanner(res.Body)
	for s.Scan() {
		line := s.Text()
		h := strings.TrimPrefix(line, "P pack-")
		h = strings.TrimSuffix(h, ".pack")
		packs = append(packs, h)
	}
	return packs, s.Err()
}

func (r *fetchWalker) downloadFile(fp string) (rErr error) {
	res, err := r.httpGet(fp)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	f, err := r.fs.TempFile(filepath.Dir(fp), filepath.Base(fp)+".temp")
	if err != nil {
		// The body is an object or pack, so there is nothing to gain from
		// draining a file this walk has given up on writing.
		_ = res.Body.Close()
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			rErr = err
		}
	}()

	if _, err := ioutil.CopyBufferPool(f, res.Body); err != nil {
		// As above: an object or a pack, so a walk that has given up on
		// writing it gains nothing from draining what is left.
		_ = res.Body.Close()
		return err
	}
	if err := res.Body.Close(); err != nil {
		return err
	}

	return r.fs.Rename(f.Name(), fp)
}

func (r *fetchWalker) getHead() (ref *plumbing.Reference, err error) {
	res, err := r.httpGet("HEAD")
	if err != nil {
		return nil, err
	}
	defer func() {
		if res.Body != nil {
			bodyErr := res.Body.Close()
			if err == nil {
				err = bodyErr
			}
		}
	}()

	s := bufio.NewScanner(res.Body)
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return nil, err
		}
		return nil, transport.ErrRepositoryNotFound
	}

	line := s.Text()
	if target, found := strings.CutPrefix(line, "ref: "); found {
		return plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.ReferenceName(target)), nil
	}

	return plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(line)), nil
}

func (r *fetchWalker) process(wants []plumbing.Hash) error {
	var head plumbing.Hash
	if headRef, err := r.refs.Head(); err != nil {
		h, err := r.getHead()
		if err != nil {
			return err
		}

		switch h.Type() {
		case plumbing.HashReference:
			head = h.Hash()
			r.refs.References = append([]*plumbing.Reference{h}, r.refs.References...)
		case plumbing.SymbolicReference:
			for _, ref := range r.refs.References {
				if ref.Name().String() == h.Target().String() {
					head = ref.Hash()
					break
				}
			}
		}
	} else {
		head = headRef.Hash()
	}

	if head.IsZero() {
		return transport.ErrRepositoryNotFound
	}

	infoPacks, err := r.getInfoPacks()
	if err != nil {
		return err
	}

	for _, h := range infoPacks {
		ph := plumbing.NewHash(h)
		if ph.IsZero() {
			continue
		}

		packIdx := path.Join("objects", "pack", fmt.Sprintf("pack-%s.idx", h))
		// Stat returns nil when the index is already on disk; the walk only has
		// to fetch the ones it does not hold.
		if _, err := r.fs.Stat(packIdx); err == nil {
			r.packIdx[ph] = packIdx
		} else {
			if err := r.downloadFile(packIdx); err != nil {
				return err
			}
			r.packIdx[ph] = packIdx
		}
	}

	// Only enqueue head if the walk does not already hold it, the same way the
	// references below are treated. head used to be appended unconditionally -
	// and twice - so a fetch whose head was already present walked it anyway
	// and aborted on the zero-value object that came back.
	if r.st.HasEncodedObject(head) != nil {
		r.queue = append(r.queue, head)
	}

	// The caller's wants reach the walk too. The dumb protocol has no
	// capability negotiation, so a want that no advertised reference points at
	// would otherwise be ignored, and a fetch for an object the server cannot
	// serve would report success having fetched nothing.
	for _, want := range wants {
		if r.st.HasEncodedObject(want) != nil {
			r.queue = append(r.queue, want)
		}
	}

	for _, ref := range r.refs.References {
		if r.st.HasEncodedObject(ref.Hash()) != nil {
			r.queue = append(r.queue, ref.Hash())
		}
	}

	return nil
}

// errObjectAlreadyPresent reports that the walk already holds the object, so
// there is nothing to download and no decoded object to hand back. It is
// control flow rather than a failure: fetch marks the object processed and
// moves on.
var errObjectAlreadyPresent = errors.New("object already present")

func (r *fetchWalker) fetchObject(objHash plumbing.Hash, obj plumbing.EncodedObject) (err error) {
	if r.st.HasEncodedObject(objHash) == nil {
		return errObjectAlreadyPresent
	}

	h := objHash.String()
	res, err := r.httpGet(path.Join("objects", h[:2], h[2:]))
	if errors.Is(err, transport.ErrRepositoryNotFound) {
		return io.EOF
	}
	if err != nil {
		return err
	}

	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return io.EOF
	default:
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	rd, err := objfile.NewReader(res.Body, objectFormatFromHash(objHash))
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

func objectFormatFromHash(h plumbing.Hash) formatcfg.ObjectFormat {
	if h.HexSize() == formatcfg.SHA256HexSize {
		return formatcfg.SHA256
	}
	return formatcfg.SHA1
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
		if errors.Is(err, errObjectAlreadyPresent) {
			// Nothing to download, and no decoded object to descend through:
			// a fetch never walks into objects it already holds, exactly as it
			// never enqueues the references that point at them.
			processed[objHash.String()] = struct{}{}
			continue
		}
		if errors.Is(err, io.EOF) {
			for packHash, packIdxPath := range r.packIdx {
				idxFile, err := r.fs.Open(packIdxPath)
				if err != nil {
					return fmt.Errorf("error opening index file: %w", err)
				}

				var hasher hash.Hash
				if packHash.Size() == crypto.SHA256.Size() {
					hasher = hash.New(crypto.SHA256)
				} else {
					hasher = hash.New(crypto.SHA1)
				}

				idx := idxfile.NewMemoryIndex(packHash.Size())
				d := idxfile.NewDecoder(idxFile, hasher)
				err = d.Decode(idx)
				_ = idxFile.Close()
				if err != nil {
					return fmt.Errorf("error decoding index file: %w", err)
				}

				indicies = append(indicies, idx)
				packPath := path.Join("objects", "pack", fmt.Sprintf("pack-%s.pack", packHash.String()))
				if ok, err := idx.Contains(objHash); err == nil && ok {
					processed[objHash.String()] = struct{}{}
					if _, ok := packs[packPath]; ok {
						continue LOOP
					}

					if _, err := r.fs.Stat(packPath); err == nil {
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
