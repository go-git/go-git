package transport

import (
	"context"
	"errors"
	"fmt"
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
func AdvertiseReferences(ctx context.Context, st storage.Storer, w io.Writer, service string, stateless bool) error {
	forPush := service == ReceivePackServiceName
	ar := packp.NewAdvRefs()

	// Set server default capabilities
	ar.Capabilities.Set(capability.Agent, capability.DefaultAgent()) //nolint:errcheck
	ar.Capabilities.Set(capability.OFSDelta)                         //nolint:errcheck
	ar.Capabilities.Set(capability.Sideband64k)                      //nolint:errcheck
	if forPush {
		// TODO: support thin-pack
		// TODO: support atomic
		ar.Capabilities.Set(capability.DeleteRefs)   //nolint:errcheck
		ar.Capabilities.Set(capability.ReportStatus) //nolint:errcheck
		ar.Capabilities.Set(capability.PushOptions)  //nolint:errcheck
	} else {
		// TODO: support multi_ack and multi_ack_detailed caps
		// TODO: support include-tag
		// TODO: support shallow
		// TODO: support deepen
		// TODO: support deepen-since
		ar.Capabilities.Set(capability.Sideband)   //nolint:errcheck
		ar.Capabilities.Set(capability.NoProgress) //nolint:errcheck
		ar.Capabilities.Set(capability.SymRef)     //nolint:errcheck
		ar.Capabilities.Set(capability.Shallow)    //nolint:errcheck
	}

	// Set references
	if err := addReferences(st, ar, !forPush); err != nil {
		return err
	}

	if stateless {
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
	if err := iter.ForEach(func(r *plumbing.Reference) error {
		hash, name := r.Hash(), r.Name()
		switch r.Type() {
		case plumbing.SymbolicReference:
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
			ar.Capabilities.Add(capability.SymRef, fmt.Sprintf("%s:%s", name, r.Target())) //nolint:errcheck
			ar.Head = &hash
		}
		ar.References[name.String()] = hash
		if r.Name().IsTag() {
			if tag, err := object.GetTag(st, hash); err == nil {
				ar.Peeled[name.String()] = tag.Target
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
