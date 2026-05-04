package transport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// ErrUpdateReference is returned when a reference update fails.
var ErrUpdateReference = errors.New("failed to update ref")

// AdvertiseRefs is a server command that implements the reference
// discovery phase of the Git transfer protocol.
func AdvertiseRefs(
	_ context.Context,
	st storage.Storer,
	w io.Writer,
	service string,
	smart bool,
) error {
	switch service {
	case UploadPackService, ReceivePackService:
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedService, service)
	}

	forPush := service == ReceivePackService
	ar := &packp.AdvRefs{}

	// Set server default capabilities
	ar.Capabilities.Set(capability.Agent, capability.DefaultAgent())
	ar.Capabilities.Set(capability.OFSDelta)
	ar.Capabilities.Set(capability.Sideband64k)
	if forPush {
		// TODO: support thin-pack
		ar.Capabilities.Set(capability.NoThin)
		// TODO: support atomic
		ar.Capabilities.Set(capability.DeleteRefs)
		ar.Capabilities.Set(capability.ReportStatus)
		ar.Capabilities.Set(capability.PushOptions)
		ar.Capabilities.Set(capability.Quiet)
	} else {
		// TODO: support include-tag
		// TODO: support deepen
		// TODO: support deepen-since
		ar.Capabilities.Set(capability.MultiACK)
		ar.Capabilities.Set(capability.MultiACKDetailed)
		ar.Capabilities.Set(capability.Sideband)
		ar.Capabilities.Set(capability.NoProgress)
		ar.Capabilities.Set(capability.Shallow)

		cfg, err := st.Config()
		var objectformat config.ObjectFormat
		if err == nil && cfg != nil {
			objectformat = cfg.Extensions.ObjectFormat
		}

		if objectformat == config.UnsetObjectFormat {
			objectformat = config.DefaultObjectFormat
		}
		ar.Capabilities.Set(capability.ObjectFormat, objectformat.String())
	}

	// Set references
	if err := addReferences(st, ar, !forPush); err != nil {
		return err
	}

	// Validate capabilities before sending the response.
	if err := capability.Validate(&ar.Capabilities); err != nil {
		return fmt.Errorf("invalid capabilities: %w", err)
	}

	if smart {
		smartReply := packp.SmartReply{
			Service: service,
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
			// Only advertise a symref when HEAD is symbolic. A detached HEAD
			// (HashReference) has no branch target to advertise; emitting
			// "HEAD:" with an empty target corrupts the capability list and
			// causes the client to store an unresolvable HEAD symref.
			if r.Type() == plumbing.SymbolicReference {
				ar.Capabilities.Add(capability.SymRef, fmt.Sprintf("%s:%s", name, r.Target()))
			}
			ar.References = append([]*plumbing.Reference{plumbing.NewHashReference(name, hash)}, ar.References...)
			return nil
		}
		ar.References = append(ar.References, plumbing.NewHashReference(name, hash))
		if r.Name().IsTag() {
			if tag, err := object.GetTag(st, hash); err == nil {
				ar.References = append(ar.References, plumbing.NewHashReference(
					plumbing.ReferenceName(name.String()+"^{}"), tag.Target,
				))
			}
		}
		return nil
	})
}
