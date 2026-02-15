package transport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// ErrUpdateReference is returned when a reference update fails.
var ErrUpdateReference = errors.New("failed to update ref")

// AdvertiseReferences is a server command that implements the reference
// discovery phase of the Git transfer protocol.
func AdvertiseReferences(
	_ context.Context,
	st storage.Storer,
	w io.Writer,
	service Service,
	smart bool,
) error {
	switch service {
	case UploadPackService, ReceivePackService:
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedService, service)
	}

	forPush := service == ReceivePackService
	ar := packp.NewAdvRefs()

	// Set server default capabilities
	_ = ar.Capabilities.Set(capability.Agent, capability.DefaultAgent())
	_ = ar.Capabilities.Set(capability.OFSDelta)
	_ = ar.Capabilities.Set(capability.Sideband64k)
	if forPush {
		// TODO: support thin-pack
		_ = ar.Capabilities.Set(capability.NoThin)
		// TODO: support atomic
		_ = ar.Capabilities.Set(capability.DeleteRefs)
		_ = ar.Capabilities.Set(capability.ReportStatus)
		_ = ar.Capabilities.Set(capability.PushOptions)
		_ = ar.Capabilities.Set(capability.Quiet)
	} else {
		// TODO: support include-tag
		// TODO: support deepen
		// TODO: support deepen-since
		_ = ar.Capabilities.Set(capability.MultiACK)
		_ = ar.Capabilities.Set(capability.MultiACKDetailed)
		_ = ar.Capabilities.Set(capability.Sideband)
		_ = ar.Capabilities.Set(capability.NoProgress)
		_ = ar.Capabilities.Set(capability.SymRef)
		_ = ar.Capabilities.Set(capability.Shallow)

		cfg, err := st.Config()
		var objectformat config.ObjectFormat
		if err == nil && cfg != nil {
			objectformat = cfg.Extensions.ObjectFormat
		}

		if objectformat == config.UnsetObjectFormat {
			objectformat = config.DefaultObjectFormat
		}
		_ = ar.Capabilities.Set(capability.ObjectFormat, objectformat.String())
	}

	// Set references
	if err := addReferences(st, ar, !forPush); err != nil {
		return err
	}

	if smart {
		smartReply := packp.SmartReply{
			Service: service.String(),
		}

		if err := smartReply.Encode(w); err != nil {
			return fmt.Errorf("failed to encode smart reply: %w", err)
		}
	}

	return ar.Encode(w)
}

func addReferences(st storage.Storer, ar *packp.AdvRefs, addHead bool) error {
	iter, err := st.IterReferences()
	if err != nil {
		return err
	}

	// Add references and their peeled values
	return iter.ForEach(func(r *plumbing.Reference) error {
		hash, name := r.Hash(), r.Name()
		if r.Type() == plumbing.SymbolicReference {
			ref, err := storer.ResolveReference(st, r.Target())
			if errors.Is(err, plumbing.ErrReferenceNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			hash = ref.Hash()
		}
		if name == plumbing.HEAD {
			if !addHead {
				return nil
			}
			// Add default branch HEAD symref
			_ = ar.Capabilities.Add(capability.SymRef, fmt.Sprintf("%s:%s", name, r.Target()))
			ar.Head = &hash
		}
		ar.References[name.String()] = hash
		if r.Name().IsTag() {
			if tag, err := object.GetTag(st, hash); err == nil {
				ar.Peeled[name.String()] = tag.Target
			}
		}
		return nil
	})
}
