package transport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// ErrUpdateReference is returned when a reference update fails.
var ErrUpdateReference = errors.New("failed to update ref")

// AdvertiseRefs is a server command that implements the reference
// discovery phase of the v0/v1 Git transfer protocol. Protocol v2 advertises
// capabilities only, via [AdvertiseCapabilities]; the sole reason this function
// accepts protocol.V2 is the receive-pack fallback: v2 has no push, so when a
// client requests v2 for receive-pack git ignores it and serves a classic
// advertisement (builtin/receive-pack.c), while http-backend still suppresses
// the "# service=..." smart-reply line for the v2 request (http-backend.c
// get_info_refs). Both behaviours are reproduced below.
func AdvertiseRefs(
	_ context.Context,
	st storage.Storer,
	w io.Writer,
	service string,
	smart bool,
	version protocol.Version,
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
		ar.Capabilities.Set(capability.ObjectFormat, objectFormat(st).String())
	}

	// Set references
	if err := addReferences(st, ar, !forPush); err != nil {
		return err
	}

	// Validate capabilities before sending the response.
	if err := capability.Validate(&ar.Capabilities); err != nil {
		return fmt.Errorf("invalid capabilities: %w", err)
	}

	// git's http-backend omits the "# service=..." smart reply whenever the
	// requested protocol is v2, even for receive-pack which then falls back to
	// a v0 advertisement (http-backend.c get_info_refs).
	if smart && version != protocol.V2 {
		smartReply := packp.SmartReply{
			Service: service,
		}

		if err := smartReply.Encode(w); err != nil {
			return fmt.Errorf("failed to encode smart reply: %w", err)
		}
	}

	// V1 prefixes the advertisement with an explicit version packet (V0 emits
	// none). AdvRefs.Encode writes it from ar.Version, so set the field rather
	// than writing the line by hand — a single source for the encoded version.
	// A v2 request with no v2 service (e.g. receive-pack) falls back to a v0
	// advertisement, so only V1 sets the field here; V2 stays at the V0 default.
	if version == protocol.V1 {
		ar.Version = protocol.V1
	}

	return ar.Encode(w)
}

// AdvertiseCapabilities implements the Protocol v2 capability advertisement for
// the upload-pack service. Unlike the v0/v1 [AdvertiseRefs], it does not list
// references (clients retrieve them with the ls-refs command) and it does not
// emit the smart-HTTP "# service=..." prefix: git omits that line for v2
// (http-backend.c get_info_refs), the response starts directly with the version
// packet.
func AdvertiseCapabilities(_ context.Context, st storage.Storer, w io.Writer, service string) error {
	if service != UploadPackService {
		return fmt.Errorf("%w: %s", ErrUnsupportedService, service)
	}

	adv := &packp.CapabilityAdv{
		Version:      protocol.V2,
		Capabilities: serverV2Capabilities(st),
	}
	return adv.Encode(w)
}

// serverV2Capabilities builds the v2 capabilities this server implements. Only
// commands and features that are actually handled are advertised: advertising a
// feature that isn't handled makes clients request it and then mis-handle the
// reply.
//
// The fetch "shallow" feature covers the whole deepen family (deepen <n>,
// deepen-since, deepen-not and deepen-relative), all of which are handled, so
// it is advertised as the single token upstream uses.
//
// TODO: advertise these once implemented:
//   - ls-refs=unborn       report an unborn HEAD on an empty repository
//   - fetch=filter         partial-clone object filters
//   - fetch=ref-in-want    want-ref negotiation
//   - fetch=sideband-all   sideband for the entire response, not just the packfile
//   - fetch=packfile-uris  offload pack data to out-of-band URIs
//   - fetch=wait-for-done  negotiate-only fetch (git fetch --negotiate-only)
//   - server-option        process client "server-option" lines
//   - object-info          object size/type queries without a fetch
func serverV2Capabilities(st storage.Storer) capability.List {
	var caps capability.List
	caps.Set(capability.Agent, capability.DefaultAgent())
	caps.Set(capability.LsRefs)
	caps.Set(capability.FetchCmd, "shallow")
	caps.Set(capability.ObjectFormat, objectFormat(st).String())
	return caps
}

// objectFormat returns the repository's configured object format, defaulting to
// the package default when the config is missing or unset.
func objectFormat(st storage.Storer) config.ObjectFormat {
	cfg, err := st.Config()
	if err != nil || cfg == nil {
		return config.DefaultObjectFormat
	}
	if cfg.Extensions.ObjectFormat == config.UnsetObjectFormat {
		return config.DefaultObjectFormat
	}
	return cfg.Extensions.ObjectFormat
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
