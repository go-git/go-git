package compat

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ImportStorer presents a compat-format view to packfile parsing while
// persisting translated native objects into the underlying storage.
type ImportStorer struct {
	base      storer.EncodedObjectStorer
	tr        *Translator
	pendingMu sync.RWMutex
	pending   map[plumbing.Hash]pendingImportObject
}

type pendingImportObject struct {
	typ     plumbing.ObjectType
	content []byte
}

// NewImportStorer returns a storage wrapper suitable for parser-based compat
// fetch. Incoming objects are expected to be in the translator's compat format.
func NewImportStorer(base storer.EncodedObjectStorer, tr *Translator) *ImportStorer {
	return &ImportStorer{
		base:    base,
		tr:      tr,
		pending: make(map[plumbing.Hash]pendingImportObject),
	}
}

func (s *ImportStorer) RawObjectWriter(typ plumbing.ObjectType, _ int64) (io.WriteCloser, error) {
	return &importObjectWriter{storer: s, typ: typ}, nil
}

func (s *ImportStorer) NewEncodedObject() plumbing.EncodedObject {
	return plumbing.NewMemoryObject(plumbing.FromObjectFormat(s.tr.CompatObjectFormat()))
}

func (s *ImportStorer) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	reader, err := obj.Reader()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return s.importCompatObject(obj.Type(), content)
}

func (s *ImportStorer) EncodedObject(objType plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if pending, ok := s.pendingObject(h); ok {
		if objType != plumbing.AnyObject && pending.typ != objType {
			return nil, plumbing.ErrObjectNotFound
		}
		return newCompatMemoryObject(s.tr.CompatObjectFormat(), pending.typ, pending.content)
	}

	nativeHash := h
	if _, err := s.base.EncodedObject(plumbing.AnyObject, h); err != nil {
		resolved, resolveErr := s.tr.Mapping().CompatToNative(h)
		if resolveErr != nil {
			return nil, err
		}
		nativeHash = resolved
	}

	obj, err := s.base.EncodedObject(plumbing.AnyObject, nativeHash)
	if err != nil {
		return nil, err
	}

	reader, err := obj.Reader()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	nativeContent, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	compatContent, err := s.tr.ReverseTranslateContent(obj.Type(), nativeContent)
	if err != nil {
		return nil, err
	}

	exported := plumbing.NewMemoryObject(plumbing.FromObjectFormat(s.tr.CompatObjectFormat()))
	exported.SetType(obj.Type())
	exported.SetSize(int64(len(compatContent)))
	w, err := exported.Writer()
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(compatContent); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	if objType != plumbing.AnyObject && exported.Type() != objType {
		return nil, plumbing.ErrObjectNotFound
	}

	return exported, nil
}

func (s *ImportStorer) IterEncodedObjects(plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	return nil, fmt.Errorf("compat import storage does not support iteration")
}

func (s *ImportStorer) HasEncodedObject(h plumbing.Hash) error {
	if _, ok := s.pendingObject(h); ok {
		return nil
	}

	if err := s.base.HasEncodedObject(h); err == nil {
		return nil
	}

	native, err := s.tr.Mapping().CompatToNative(h)
	if err != nil {
		return err
	}
	return s.base.HasEncodedObject(native)
}

func (s *ImportStorer) EncodedObjectSize(h plumbing.Hash) (int64, error) {
	obj, err := s.EncodedObject(plumbing.AnyObject, h)
	if err != nil {
		return 0, err
	}
	return obj.Size(), nil
}

func (s *ImportStorer) AddAlternate(remote string) error {
	return s.base.AddAlternate(remote)
}

// Finalize retries any compat-format objects that were deferred during pack
// parsing because their referenced objects had not been mapped yet.
func (s *ImportStorer) Finalize() error {
	for {
		progress := false
		skipped := 0

		for _, typ := range []plumbing.ObjectType{
			plumbing.BlobObject,
			plumbing.TreeObject,
			plumbing.CommitObject,
			plumbing.TagObject,
		} {
			hashes := s.pendingHashesOfType(typ)
			for _, compatHash := range hashes {
				pending, ok := s.pendingObject(compatHash)
				if !ok {
					continue
				}

				err := s.storeCompatObject(compatHash, pending.typ, pending.content)
				if err != nil {
					if errors.Is(err, plumbing.ErrObjectNotFound) {
						skipped++
						continue
					}
					return err
				}

				s.deletePendingObject(compatHash)
				progress = true
			}
		}

		if s.pendingCount() == 0 {
			return nil
		}
		if !progress {
			return fmt.Errorf("unable to finalize %d compat objects", s.pendingCount())
		}
	}
}

func (s *ImportStorer) importCompatObject(objType plumbing.ObjectType, compatContent []byte) (plumbing.Hash, error) {
	compatHash, err := s.tr.ComputeCompatHash(objType, compatContent)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	if err := s.storeCompatObject(compatHash, objType, compatContent); err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			s.setPendingObject(compatHash, pendingImportObject{
				typ:     objType,
				content: bytes.Clone(compatContent),
			})
			return compatHash, nil
		}
		return plumbing.ZeroHash, err
	}

	nativeHash, err := s.tr.Mapping().CompatToNative(compatHash)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return nativeHash, nil
}

func (s *ImportStorer) storeCompatObject(
	compatHash plumbing.Hash,
	objType plumbing.ObjectType,
	compatContent []byte,
) error {
	nativeContent, err := s.tr.TranslateCompatContent(objType, compatContent)
	if err != nil {
		return err
	}

	nativeObj := plumbing.NewMemoryObject(plumbing.FromObjectFormat(s.tr.NativeObjectFormat()))
	nativeObj.SetType(objType)
	nativeObj.SetSize(int64(len(nativeContent)))
	w, err := nativeObj.Writer()
	if err != nil {
		return err
	}
	if _, err := w.Write(nativeContent); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	nativeHash, err := s.base.SetEncodedObject(nativeObj)
	if err != nil {
		return err
	}

	if err := s.tr.Mapping().Add(nativeHash, compatHash); err != nil {
		return err
	}

	return nil
}

type importObjectWriter struct {
	storer *ImportStorer
	typ    plumbing.ObjectType
	buf    bytes.Buffer
}

func (w *importObjectWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *importObjectWriter) Close() error {
	_, err := w.storer.importCompatObject(w.typ, w.buf.Bytes())
	return err
}

func (s *ImportStorer) pendingHashesOfType(objType plumbing.ObjectType) []plumbing.Hash {
	s.pendingMu.RLock()
	hashes := make([]plumbing.Hash, 0, len(s.pending))
	for h, obj := range s.pending {
		if obj.typ == objType {
			hashes = append(hashes, h)
		}
	}
	s.pendingMu.RUnlock()

	slices.SortFunc(hashes, func(a, b plumbing.Hash) int {
		return bytes.Compare(a.Bytes(), b.Bytes())
	})

	return hashes
}

func (s *ImportStorer) pendingObject(h plumbing.Hash) (pendingImportObject, bool) {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()

	obj, ok := s.pending[h]
	return obj, ok
}

func (s *ImportStorer) setPendingObject(h plumbing.Hash, obj pendingImportObject) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	s.pending[h] = obj
}

func (s *ImportStorer) deletePendingObject(h plumbing.Hash) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	delete(s.pending, h)
}

func (s *ImportStorer) pendingCount() int {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()

	return len(s.pending)
}

func newCompatMemoryObject(
	objectFormat formatcfg.ObjectFormat,
	objType plumbing.ObjectType,
	content []byte,
) (plumbing.EncodedObject, error) {
	obj := plumbing.NewMemoryObject(plumbing.FromObjectFormat(objectFormat))
	obj.SetType(objType)
	obj.SetSize(int64(len(content)))

	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(content); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return obj, nil
}
