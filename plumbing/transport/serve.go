package transport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/trace"
)

// ErrUpdateReference is returned when a reference update fails.
var ErrUpdateReference = errors.New("failed to update ref")

// ErrFunnyRefname is returned when a push names a reference the server refuses
// to touch: one that is not under refs/, or one whose name is malformed.
var ErrFunnyRefname = errors.New("funny refname")

// ErrDuplicateRefname is reported for every command of a push whose command
// list updates one reference more than once.
var ErrDuplicateRefname = errors.New("multiple updates for ref not allowed")

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

// advertisable reports whether a reference name may be put on the wire.
//
// The reference store reports what is on disk, malformed names included,
// because a caller that cannot see a name cannot repair it and because
// anything that prunes or repacks from that listing has to see every name that
// is really there. The advertisement excludes malformed wire names. A valid
// Git name is retained even if the filesystem storer applies stricter path
// safety rules, such as rejecting an HFS+ component that folds to a dot.
//
// Git's upload-pack.c send_ref does not consult REF_BAD_NAME: malformed names
// that reach it can be advertised with a zero object id, leaving the dropping
// to fetch-pack.c's filter_refs. The files backend excludes some entries,
// including loose .lock files, before send_ref. The ref-filter.c pass that
// warns and skips is the porcelain layer behind for-each-ref and git branch,
// not upload-pack's path.
//
// Dropping rather than zeroing is the choice here, because a zero id is a
// thing every client has to be taught to read, while a name a peer cannot
// store is one it has no use for. The cost is that git ls-remote against a
// go-git server does not show such a name at all, where Git may show it with
// a zero object id.
//
// HEAD is the one name outside refs/ that belongs on the wire, and Validate
// carves it out already.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/upload-pack.c#L1196-L1242
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refs/files-backend.c#L345-L350
func advertisable(name plumbing.ReferenceName) bool {
	if name == plumbing.HEAD {
		return true
	}

	return name.IsUnderRefs() && name.Validate() == nil
}

func addReferences(st storage.Storer, ar *packp.AdvRefs, addHead bool) error {
	iter, err := st.IterReferences()
	if err != nil {
		return err
	}

	// Add references and their peeled values
	return iter.ForEach(func(r *plumbing.Reference) error {
		hash, name := r.Hash(), r.Name()
		var target plumbing.ReferenceName

		if !advertisable(name) {
			// One malformed name must not prevent advertising the usable refs.
			trace.General.Printf("ignoring ref with broken name %q", string(name))
			return nil
		}
		if r.Type() == plumbing.SymbolicReference {
			ref, err := storer.ResolveReference(st, r.Target())
			// Missing, rejected and cyclic referents cost this one entry.
			// Other errors still propagate: an unavailable store must not
			// turn into a successful but incomplete advertisement.
			if reference.IsUnresolvableForAdvertisement(err) {
				trace.General.Printf("ignoring ref %q with unresolvable target %q",
					string(name), r.Target().String())
				return nil
			}
			if err != nil {
				return err
			}
			hash = ref.Hash()
			// Git advertises the terminal name, including for symbolic chains.
			// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/upload-pack.c#L1245-L1259.
			target = ref.Name()
		}
		if name == plumbing.HEAD {
			if !addHead {
				return nil
			}
			// Only advertise a symref when HEAD is symbolic. A detached HEAD
			// (HashReference) has no branch target to advertise; emitting
			// "HEAD:" with an empty target corrupts the capability list and
			// causes the client to store an unresolvable HEAD symref.
			//
			// The target is checked too. It is a name leaving the process
			// like any other, and naming a ref that was just withheld would
			// produce an advertisement contradicting itself: the client is
			// told HEAD points somewhere it will never be told about, and
			// go-git's own client fails such a clone outright. HEAD keeps its
			// object id, so the peer still has a starting point.
			if r.Type() == plumbing.SymbolicReference && advertisable(target) {
				ar.Capabilities.Add(capability.SymRef, fmt.Sprintf("%s:%s", name, target))
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
