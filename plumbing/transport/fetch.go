package transport

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/compat"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

// FetchPack fetches a packfile from the remote into the given storage.
func FetchPack(
	ctx context.Context,
	st storage.Storer,
	caps *capability.List,
	packf io.ReadCloser,
	shallowInfo *packp.ShallowUpdate,
	req *FetchRequest,
) error {
	packf = ioutil.NewContextReadCloser(ctx, packf)

	var demuxer *sideband.Demuxer
	var reader io.Reader = packf
	if caps.Supports(capability.Sideband64k) {
		demuxer = sideband.NewDemuxer(sideband.Sideband64k, reader)
	} else if caps.Supports(capability.Sideband) {
		demuxer = sideband.NewDemuxer(sideband.Sideband, reader)
	}

	if demuxer != nil && req.Progress != nil {
		demuxer.Progress = req.Progress
		reader = demuxer
	}

	tr, remoteFormat, compatFetch := CompatFetchTranslator(conn.Capabilities(), st)
	if compatFetch {
		importer := compat.NewImportStorer(st, tr)
		p := packfile.NewParser(reader,
			packfile.WithStorage(importer),
			packfile.WithObjectFormat(remoteFormat))
		if _, err := p.Parse(); err != nil {
			return fmt.Errorf("parse compat pack: %w", err)
		}
		if err := importer.Finalize(); err != nil {
			return fmt.Errorf("finalize compat import: %w", err)
		}
		if err := compat.TranslateStoredObjects(st, tr); err != nil {
			return fmt.Errorf("reconcile compat mappings: %w", err)
		}
	} else {
		if err := packfile.UpdateObjectStorage(st, reader); err != nil {
			return err
		}

		// If the storage supports compatObjectFormat, translate all fetched
		// objects to populate the bidirectional hash mapping table.
		if tp, ok := st.(xstorage.CompatTranslatorProvider); ok {
			if t := tp.Translator(); t != nil {
				if err := compat.TranslateStoredObjects(st, t); err != nil {
					return err
				}
			}
		}
	}

	if err := packf.Close(); err != nil {
		return err
	}

	if shallowInfo != nil {
		if compatFetch {
			for i, h := range shallowInfo.Shallows {
				if native, err := tr.Mapping().CompatToNative(h); err == nil {
					shallowInfo.Shallows[i] = native
				}
			}
			for i, h := range shallowInfo.Unshallows {
				if native, err := tr.Mapping().CompatToNative(h); err == nil {
					shallowInfo.Unshallows[i] = native
				}
			}
		}
		if err := updateShallow(st, shallowInfo); err != nil {
			return err
		}
	}

	return nil
}

// CompatFetchTranslator returns the compat translator and remote object format
// when the remote advertises the repository's compat format.
func CompatFetchTranslator(
	caps *capability.List,
	st storage.Storer,
) (*compat.Translator, formatcfg.ObjectFormat, bool) {
	tp, ok := st.(xstorage.CompatTranslatorProvider)
	if !ok {
		return nil, formatcfg.UnsetObjectFormat, false
	}

	tr := tp.Translator()
	if tr == nil || !caps.Supports(capability.ObjectFormat) {
		return nil, formatcfg.UnsetObjectFormat, false
	}

	values := caps.Get(capability.ObjectFormat)
	if len(values) == 0 {
		return nil, formatcfg.UnsetObjectFormat, false
	}

	remoteFormat := formatcfg.ObjectFormat(values[0])
	if remoteFormat != tr.CompatObjectFormat() {
		return nil, remoteFormat, false
	}

	return tr, remoteFormat, true
}

func updateShallow(st storage.Storer, shallowInfo *packp.ShallowUpdate) error {
	shallows, err := st.Shallow()
	if err != nil {
		return err
	}

outer:
	for _, s := range shallowInfo.Shallows {
		for _, oldS := range shallows {
			if s == oldS {
				continue outer
			}
		}
		shallows = append(shallows, s)
	}

	for _, s := range shallowInfo.Unshallows {
		for i, oldS := range shallows {
			if s == oldS {
				shallows = append(shallows[:i], shallows[i+1:]...)
				break
			}
		}
	}

	return st.SetShallow(shallows)
}
