package compat

import (
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// PushExportStorer projects native repository objects into the compat object
// format for pack generation during push.
//
// It is read-only and intended to be used only as an export view over an
// existing native repository storage. It only handles the repository's native
// <-> compat hash pair; it is not a general-purpose object-format gateway.
type PushExportStorer struct {
	base storer.EncodedObjectStorer
	cfg  *config.Config
	tr   *Translator
}

// NewPushExportStorer returns a read-only storage view that exposes objects in
// the translator's compat format while reading from the native base storage.
func NewPushExportStorer(base storer.EncodedObjectStorer, cfg *config.Config, tr *Translator) *PushExportStorer {
	exportCfg := config.NewConfig()
	if cfg != nil {
		exportCfg.Pack = cfg.Pack
	}
	exportCfg.Core.RepositoryFormatVersion = formatcfg.Version1
	exportCfg.Extensions.ObjectFormat = tr.CompatObjectFormat()
	exportCfg.Extensions.CompatObjectFormat = formatcfg.UnsetObjectFormat

	return &PushExportStorer{
		base: base,
		cfg:  exportCfg,
		tr:   tr,
	}
}

func (s *PushExportStorer) Config() (*config.Config, error) {
	return s.cfg, nil
}

func (s *PushExportStorer) SetConfig(*config.Config) error {
	return fmt.Errorf("compat push export storage is read-only")
}

func (s *PushExportStorer) RawObjectWriter(plumbing.ObjectType, int64) (io.WriteCloser, error) {
	return nil, fmt.Errorf("compat push export storage is read-only")
}

func (s *PushExportStorer) NewEncodedObject() plumbing.EncodedObject {
	return plumbing.NewMemoryObject(plumbing.FromObjectFormat(s.tr.CompatObjectFormat()))
}

func (s *PushExportStorer) SetEncodedObject(plumbing.EncodedObject) (plumbing.Hash, error) {
	return plumbing.ZeroHash, fmt.Errorf("compat push export storage is read-only")
}

func (s *PushExportStorer) EncodedObject(objType plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	nativeHash := h

	if _, err := s.base.EncodedObject(plumbing.AnyObject, h); err != nil {
		if !errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, err
		}

		if _, err := s.tr.Mapping().NativeToCompat(h); err != nil {
			if !errors.Is(err, plumbing.ErrObjectNotFound) {
				return nil, err
			}
			var resolveErr error
			nativeHash, resolveErr = s.tr.Mapping().CompatToNative(h)
			if resolveErr != nil {
				return nil, resolveErr
			}
		}
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

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	compatContent, err := s.tr.ReverseTranslateContent(obj.Type(), content)
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

func (s *PushExportStorer) IterEncodedObjects(objType plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	return nil, fmt.Errorf("compat push export storage does not support iteration")
}

func (s *PushExportStorer) HasEncodedObject(h plumbing.Hash) error {
	if err := s.base.HasEncodedObject(h); err == nil {
		return nil
	}

	native, err := s.tr.Mapping().CompatToNative(h)
	if err != nil {
		return err
	}
	return s.base.HasEncodedObject(native)
}

func (s *PushExportStorer) EncodedObjectSize(h plumbing.Hash) (int64, error) {
	obj, err := s.EncodedObject(plumbing.AnyObject, h)
	if err != nil {
		return 0, err
	}
	return obj.Size(), nil
}

func (s *PushExportStorer) AddAlternate(string) error {
	return fmt.Errorf("compat push export storage is read-only")
}
