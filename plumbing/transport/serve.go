package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
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

// AdvertiseCapabilitiesV2 writes the V2 capability advertisement to w.
// This replaces AdvertiseReferences for V2 connections.
func AdvertiseCapabilitiesV2(
	_ context.Context,
	st storage.Storer,
	w io.Writer,
	service Service,
	smart bool,
) error {
	if smart {
		smartReply := packp.SmartReply{
			Service: service.String(),
		}

		if err := smartReply.Encode(w); err != nil {
			return fmt.Errorf("failed to encode smart reply: %w", err)
		}
	}

	// Write the version line.
	if _, err := pktline.Writeln(w, "version 2"); err != nil {
		return err
	}

	v2caps := packp.NewV2ServerCapabilities()

	// Global capabilities.
	_ = v2caps.Global.Add(capability.Agent, capability.DefaultAgent())

	cfg, err := st.Config()
	var objectformat config.ObjectFormat
	if err == nil && cfg != nil {
		objectformat = cfg.Extensions.ObjectFormat
	}
	if objectformat == config.UnsetObjectFormat {
		objectformat = config.DefaultObjectFormat
	}
	_ = v2caps.Global.Add(capability.ObjectFormat, objectformat.String())

	// ls-refs command with sub-capabilities.
	lsRefsCaps := capability.NewList()
	_ = lsRefsCaps.Add("peel")
	_ = lsRefsCaps.Add("symrefs")
	v2caps.Commands["ls-refs"] = lsRefsCaps

	if service == UploadPackService {
		// fetch command with sub-capabilities.
		fetchCaps := capability.NewList()
		_ = fetchCaps.Add(capability.Shallow)
		_ = fetchCaps.Add(capability.Filter)
		_ = fetchCaps.Add(capability.IncludeTag)
		_ = fetchCaps.Add(capability.OFSDelta)
		_ = fetchCaps.Add(capability.WaitForDone)
		_ = fetchCaps.Add("ref-in-want")
		v2caps.Commands["fetch"] = fetchCaps
	} else {
		// receive-pack: advertise push capabilities in global so that
		// V0-style push negotiation works. V2 push still uses V0 wire
		// format for the actual update-request + packfile exchange.
		_ = v2caps.Global.Add(capability.ReportStatus)
		_ = v2caps.Global.Add(capability.DeleteRefs)
		_ = v2caps.Global.Add(capability.Quiet)
		_ = v2caps.Global.Add(capability.OFSDelta)
		_ = v2caps.Global.Add(capability.Sideband64k)
	}

	return v2caps.Encode(w)
}

// HandleLsRefs handles the V2 ls-refs command server-side.
func HandleLsRefs(
	_ context.Context,
	st storage.Storer,
	r io.Reader,
	w io.Writer,
) error {
	// Decode the ls-refs request arguments (after delimiter).
	req, err := decodeLsRefsArgs(r)
	if err != nil {
		return fmt.Errorf("decoding ls-refs args: %w", err)
	}

	resp := packp.NewLsRefsResponse()

	// Upstream Git sends HEAD first (via send_possibly_unborn_head),
	// then iterates other refs. We mirror this by handling HEAD
	// separately before the main iteration.
	addLsRef := func(ref *plumbing.Reference, name plumbing.ReferenceName, hash plumbing.Hash) {
		if ref.Type() == plumbing.SymbolicReference && req.IncludeSymRefs {
			resp.References = append(resp.References, plumbing.NewSymbolicReference(name, ref.Target()))
			resp.SymRefTargets[name.String()] = hash
		} else {
			resp.References = append(resp.References, plumbing.NewHashReference(name, hash))
		}
		if req.IncludePeeled && name.IsTag() {
			if tag, err := object.GetTag(st, hash); err == nil {
				resp.Peeled[name.String()] = tag.Target
			}
		}
	}

	matchesPrefix := func(name string) bool {
		if len(req.RefPrefixes) == 0 {
			return true
		}
		for _, prefix := range req.RefPrefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}

	// Send HEAD first.
	if matchesPrefix("HEAD") {
		headRef, err := st.Reference(plumbing.HEAD)
		if err == nil {
			hash := headRef.Hash()
			if headRef.Type() == plumbing.SymbolicReference {
				resolved, resolveErr := storer.ResolveReference(st, headRef.Target())
				if resolveErr == nil {
					hash = resolved.Hash()
				} else if !errors.Is(resolveErr, plumbing.ErrReferenceNotFound) {
					return resolveErr
				} else if !req.IncludeUnborn {
					// Unborn HEAD and unborn not requested — skip.
					hash = plumbing.ZeroHash
				}
			}
			if !hash.IsZero() || req.IncludeUnborn {
				addLsRef(headRef, plumbing.HEAD, hash)
			}
		}
	}

	// Then iterate all other refs.
	iter, err := st.IterReferences()
	if err != nil {
		return err
	}

	err = iter.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name()
		if name == plumbing.HEAD {
			return nil // already sent above
		}

		if !matchesPrefix(name.String()) {
			return nil
		}

		hash := ref.Hash()
		if ref.Type() == plumbing.SymbolicReference {
			resolved, err := storer.ResolveReference(st, ref.Target())
			if errors.Is(err, plumbing.ErrReferenceNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			hash = resolved.Hash()
		}

		addLsRef(ref, name, hash)
		return nil
	})
	if err != nil {
		return err
	}

	return resp.Encode(w)
}

// decodeLsRefsArgs reads the ls-refs arguments after the delimiter packet.
// tooManyPrefixes matches upstream Git's guard against excessive ref-prefix
// lines. If the client sends this many or more, all prefixes are discarded
// and all refs are returned (preventing DoS via prefix filtering).
const tooManyPrefixes = 65536

func decodeLsRefsArgs(r io.Reader) (*packp.LsRefsRequest, error) {
	req := &packp.LsRefsRequest{}

	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return nil, err
		}

		if l == pktline.Flush {
			// Upstream: if too many prefixes, clear them to return all refs.
			if len(req.RefPrefixes) >= tooManyPrefixes {
				req.RefPrefixes = nil
			}
			return req, nil
		}
		if l == pktline.Delim {
			continue
		}

		text := string(bytes.TrimSuffix(line, []byte("\n")))

		switch {
		case text == "peel":
			req.IncludePeeled = true
		case text == "symrefs":
			req.IncludeSymRefs = true
		case text == "unborn":
			req.IncludeUnborn = true
		case strings.HasPrefix(text, "ref-prefix "):
			if len(req.RefPrefixes) < tooManyPrefixes {
				req.RefPrefixes = append(req.RefPrefixes, strings.TrimPrefix(text, "ref-prefix "))
			}
		}
	}
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
				_ = ar.Capabilities.Add(capability.SymRef, fmt.Sprintf("%s:%s", name, r.Target()))
			}
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
