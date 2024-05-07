package server

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
)

var ErrUpdateReference = errors.New("failed to update ref")

// AdvertiseReferences is a server command that implements the reference
// discovery phase of the Git transfer protocol.
func AdvertiseReferences(ctx context.Context, st storage.Storer, w io.Writer, forPush bool) error {
	ar := packp.NewAdvRefs()

	// Set server default capabilities
	ar.Capabilities.Set(capability.Agent, capability.DefaultAgent()) // nolint: errcheck
	ar.Capabilities.Set(capability.OFSDelta)                         // nolint: errcheck
	ar.Capabilities.Set(capability.Sideband64k)                      // nolint: errcheck
	if forPush {
		ar.Capabilities.Set(capability.DeleteRefs)   // nolint: errcheck
		ar.Capabilities.Set(capability.ReportStatus) // nolint: errcheck
		ar.Capabilities.Set(capability.PushOptions)  // nolint: errcheck
	} else {
		ar.Capabilities.Set(capability.Sideband)   // nolint: errcheck
		ar.Capabilities.Set(capability.NoProgress) // nolint: errcheck
	}

	// Set references
	if err := addReferences(st, ar, !forPush); err != nil {
		return err
	}

	return ar.Encode(w)
}

func addReferences(st storage.Storer, ar *packp.AdvRefs, addHead bool) error {
	iter, err := st.IterReferences()
	if err != nil {
		return err
	}

	// Add references and their peeled values
	if err := iter.ForEach(func(r *plumbing.Reference) (err error) {
		ref := r
		switch ref.Type() {
		case plumbing.SymbolicReference:
			ref, err = storer.ResolveReference(st, r.Target())
			if err != nil {
				return err
			}
		}
		if ref.Name() == plumbing.HEAD {
			if !addHead {
				return nil
			}
			hash := ref.Hash()
			ar.Head = &hash
		}
		ar.References = append(ar.References, ref)
		if ref.Name().IsTag() {
			if tag, err := object.GetTag(st, ref.Hash()); err == nil {
				tagRef := plumbing.NewReferenceFromStrings(ref.Name().String()+"^{}", tag.Target.String())
				ar.References = append(ar.References, tagRef)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

// DiscoverProtocolVersion takes a git protocol string and returns the
// corresponding protocol version.
func DiscoverProtocolVersion(p string) protocol.Version {
	var ver protocol.Version
	for _, param := range strings.Split(p, ":") {
		if strings.HasPrefix(param, "version=") {
			if v, _ := strconv.Atoi(param[8:]); v > int(ver) {
				ver = protocol.Version(v)
			}
		}
	}
	return ver
}
