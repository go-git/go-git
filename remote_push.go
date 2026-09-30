package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// Push performs a push to the remote. Returns NoErrAlreadyUpToDate if the
// remote was already up-to-date.
// Malformed wildcard source patterns are rejected before contacting the remote.
func (r *Remote) Push(o *PushOptions) error {
	return r.PushContext(context.Background(), o)
}

// PushContext performs a push to the remote. Returns NoErrAlreadyUpToDate if
// the remote was already up-to-date.
// Malformed wildcard source patterns are rejected before contacting the remote.
//
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned. The context only affects the
// transport operations.
func (r *Remote) PushContext(ctx context.Context, o *PushOptions) (err error) {
	if trace.Performance.Enabled() {
		start := time.Now()
		defer func() {
			trace.Performance.Printf("performance: %.9f s: git command: git push", time.Since(start).Seconds())
		}()
	}

	if err := o.Validate(); err != nil {
		return err
	}

	for _, spec := range o.RefSpecs {
		if !spec.IsWildcard() {
			continue
		}
		// A source glob must satisfy Git's refname rules with one wildcard
		// and one-level names allowed. A fixed invalid prefix or suffix must
		// fail before pruning can mistake filtered sources for missing refs.
		// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/refspec.c#L119-L134.
		name := strings.ReplaceAll(spec.Src(), "*", "x")
		if !strings.Contains(name, "/") {
			name = plumbing.RefPrefix + name
		}
		if err := plumbing.ReferenceName(name).Validate(); err != nil {
			return fmt.Errorf("%w: invalid source pattern %q", plumbing.ErrInvalidReferenceName, spec.Src())
		}
	}

	if o.RemoteName != r.c.Name {
		return fmt.Errorf("remote names don't match: %s != %s", o.RemoteName, r.c.Name)
	}

	if o.RemoteURL == "" && len(r.c.URLs) > 0 {
		o.RemoteURL = r.c.URLs[len(r.c.URLs)-1]
	}

	cl, req, err := newClient(o.RemoteURL, o.ClientOptions)
	if err != nil {
		return err
	}

	req.Command = transport.ReceivePackService
	sess, err := cl.Handshake(ctx, req)
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(sess, &err)

	rRefs, err := sess.GetRemoteRefs(ctx, nil)
	if err != nil {
		return err
	}

	remoteRefs := referenceStorageFromRefs(rRefs.References, true)
	if err := r.checkRequireRemoteRefs(o.RequireRemoteRefs, remoteRefs); err != nil {
		return err
	}

	return r.sendPack(ctx, sess, remoteRefs, o)
}

func (r *Remote) sendPack(ctx context.Context, sess transport.Session, remoteRefs storer.ReferenceStorer, o *PushOptions) error {
	isDelete := false
	allDelete := true
	for _, rs := range o.RefSpecs {
		if rs.IsDelete() {
			isDelete = true
		} else {
			allDelete = false
		}
		if isDelete && !allDelete {
			break
		}
	}

	// TODO: support delete-refs
	caps := sess.Capabilities() // server capabilities
	if isDelete && !caps.Supports(capability.DeleteRefs) {
		return ErrDeleteRefNotSupported
	}

	if o.Force {
		for i := 0; i < len(o.RefSpecs); i++ {
			rs := &o.RefSpecs[i]
			if !rs.IsForceUpdate() && !rs.IsDelete() {
				o.RefSpecs[i] = config.RefSpec("+" + rs.String())
			}
		}
	}

	localRefs, err := reference.References(r.s)
	if err != nil {
		return err
	}
	// Storage enumeration is also used by repository maintenance and must
	// remain complete. Only push candidates exclude malformed ordinary refs,
	// including stale lock files that Git's files ref iterator skips.
	pushRefs := localRefs[:0]
	for _, ref := range localRefs {
		if ref.Name().IsUnderRefs() {
			if err := ref.Name().Validate(); err != nil {
				// An explicit source must fail rather than disappear: prune
				// would interpret its absence as a request to delete its
				// mapped destination. Wildcards still skip malformed entries.
				for _, spec := range o.RefSpecs {
					if !spec.IsWildcard() && spec.Match(ref.Name()) {
						return err
					}
				}
				continue
			}
		}
		pushRefs = append(pushRefs, ref)
	}
	localRefs = pushRefs

	cmds := make([]*packp.Command, 0)
	if err := r.addReferencesToUpdate(o.RefSpecs, localRefs, remoteRefs, &cmds, o.Prune, o.ForceWithLease); err != nil {
		return err
	}

	if o.FollowTags {
		if err := r.addReachableTags(localRefs, remoteRefs, &cmds); err != nil {
			return err
		}
	}
	// Refspecs can turn a valid source into an invalid destination. Validate
	// the complete command list before any update is sent to the remote.
	for _, cmd := range cmds {
		if err := cmd.Name.Validate(); err != nil {
			return err
		}
	}

	if len(cmds) == 0 {
		return NoErrAlreadyUpToDate
	}

	objects := objectsToPush(cmds)
	haves, err := referencesToHashes(remoteRefs)
	if err != nil {
		return err
	}

	stop, err := r.s.Shallow()
	if err != nil {
		return err
	}

	// if we have shallow we should include this as part of the objects that
	// we are aware.
	haves = append(haves, stop...)

	var hashesToPush []plumbing.Hash
	// Avoid the expensive revlist operation if we're only doing deletes.
	if !allDelete {
		hashesToPush, err = revlist.Objects(r.s, objects, haves)
		if err != nil {
			return err
		}
	}

	if len(hashesToPush) == 0 {
		allDelete = true
		for _, command := range cmds {
			if command.Action() != packp.Delete {
				allDelete = false
				break
			}
		}
	}

	if err := pushHashes(ctx, sess, r.s, cmds, hashesToPush, allDelete, o); err != nil {
		return err
	}

	return r.updateRemoteReferenceStorage(cmds)
}

func (r *Remote) useRefDeltas(ar *packp.AdvRefs) bool {
	return !ar.Capabilities.Supports(capability.OFSDelta)
}

func (r *Remote) addReachableTags(localRefs []*plumbing.Reference, remoteRefs storer.ReferenceStorer, cmds *[]*packp.Command) error {
	tags := make(map[plumbing.Reference]struct{})
	// get a list of all tags locally
	for _, ref := range localRefs {
		if strings.HasPrefix(string(ref.Name()), "refs/tags") {
			tags[*ref] = struct{}{}
		}
	}

	remoteRefIter, err := remoteRefs.IterReferences()
	if err != nil {
		return err
	}

	// remove any that are already on the remote
	if err := remoteRefIter.ForEach(func(reference *plumbing.Reference) error {
		delete(tags, *reference)
		return nil
	}); err != nil {
		return err
	}

	for tag := range tags {
		tagObject, err := object.GetObject(r.s, tag.Hash())
		var tagCommit *object.Commit
		if err != nil {
			return fmt.Errorf("get tag object: %w", err)
		}

		if tagObject.Type() != plumbing.TagObject {
			continue
		}

		annotatedTag, ok := tagObject.(*object.Tag)
		if !ok {
			return errors.New("could not get annotated tag object")
		}

		tagCommit, err = object.GetCommit(r.s, annotatedTag.Target)
		if err != nil {
			return fmt.Errorf("get annotated tag commit: %w", err)
		}

		// only include tags that are reachable from one of the refs
		// already being pushed
		for _, cmd := range *cmds {
			if tag.Name() == cmd.Name {
				continue
			}

			if strings.HasPrefix(cmd.Name.String(), "refs/tags") {
				continue
			}

			c, err := object.GetCommit(r.s, cmd.New)
			if err != nil {
				return fmt.Errorf("get commit %v: %w", cmd.Name, err)
			}

			if isAncestor, err := tagCommit.IsAncestor(c); err == nil && isAncestor {
				*cmds = append(*cmds, &packp.Command{Name: tag.Name(), New: tag.Hash()})
			}
		}
	}

	return nil
}

func (r *Remote) updateRemoteReferenceStorage(
	cmds []*packp.Command,
) error {
	for _, spec := range r.c.Fetch {
		for _, c := range cmds {
			if !spec.Match(c.Name) {
				continue
			}

			local := spec.Dst(c.Name)
			ref := plumbing.NewHashReference(local, c.New)
			switch c.Action() {
			case packp.Create, packp.Update:
				if err := r.s.SetReference(ref); err != nil {
					return err
				}
			case packp.Delete:
				if err := r.s.RemoveReference(local); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *Remote) addReferencesToUpdate(
	refspecs []config.RefSpec,
	localRefs []*plumbing.Reference,
	remoteRefs storer.ReferenceStorer,
	cmds *[]*packp.Command,
	prune bool,
	forceWithLease *ForceWithLease,
) error {
	// This references dictionary will be used to search references by name.
	refsDict := make(map[string]*plumbing.Reference)
	for _, ref := range localRefs {
		refsDict[ref.Name().String()] = ref
	}

	for _, rs := range refspecs {
		if rs.IsDelete() {
			if err := r.deleteReferences(rs, remoteRefs, refsDict, cmds, false); err != nil {
				return err
			}
		} else {
			err := r.addOrUpdateReferences(rs, localRefs, refsDict, remoteRefs, cmds, forceWithLease)
			if err != nil {
				return err
			}

			if prune {
				if err := r.deleteReferences(rs, remoteRefs, refsDict, cmds, true); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *Remote) addOrUpdateReferences(
	rs config.RefSpec,
	localRefs []*plumbing.Reference,
	refsDict map[string]*plumbing.Reference,
	remoteRefs storer.ReferenceStorer,
	cmds *[]*packp.Command,
	forceWithLease *ForceWithLease,
) error {
	// If it is not a wildcard refspec we can directly search for the reference
	// in the references dictionary.
	if !rs.IsWildcard() {
		ref, ok := refsDict[rs.Src()]
		if !ok {
			object, err := object.GetObject(r.s, plumbing.NewHash(rs.Src()))
			if err == nil {
				return r.addObject(rs, remoteRefs, object.ID(), cmds)
			}
			return nil
		}

		return r.addReferenceIfRefSpecMatches(rs, remoteRefs, ref, cmds, forceWithLease)
	}

	for _, ref := range localRefs {
		err := r.addReferenceIfRefSpecMatches(rs, remoteRefs, ref, cmds, forceWithLease)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Remote) deleteReferences(rs config.RefSpec,
	remoteRefs storer.ReferenceStorer,
	refsDict map[string]*plumbing.Reference,
	cmds *[]*packp.Command,
	prune bool,
) error {
	iter, err := remoteRefs.IterReferences()
	if err != nil {
		return err
	}

	return iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}

		if prune {
			rs := rs.Reverse()
			if !rs.Match(ref.Name()) {
				return nil
			}

			if _, ok := refsDict[rs.Dst(ref.Name()).String()]; ok {
				return nil
			}
		} else if rs.Dst("") != ref.Name() {
			return nil
		}

		cmd := &packp.Command{
			Name: ref.Name(),
			Old:  ref.Hash(),
			New:  plumbing.ZeroHash,
		}
		*cmds = append(*cmds, cmd)
		return nil
	})
}

func (r *Remote) addObject(rs config.RefSpec,
	remoteRefs storer.ReferenceStorer, localObject plumbing.Hash,
	cmds *[]*packp.Command,
) error {
	if rs.IsWildcard() {
		return errors.New("can't use wildcard together with hash refspecs")
	}

	cmd := &packp.Command{
		Name: rs.Dst(""),
		Old:  plumbing.ZeroHash,
		New:  localObject,
	}
	remoteRef, err := remoteRefs.Reference(cmd.Name)
	if err == nil {
		if remoteRef.Type() != plumbing.HashReference {
			// TODO: check actual git behavior here
			return nil
		}

		cmd.Old = remoteRef.Hash()
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return err
	}
	if cmd.Old == cmd.New {
		return nil
	}
	if !rs.IsForceUpdate() {
		if err := checkTagUpdate(cmd); err != nil {
			return err
		}
		if err := checkFastForwardUpdate(r.s, remoteRefs, cmd); err != nil {
			return err
		}
	}

	*cmds = append(*cmds, cmd)
	return nil
}

func (r *Remote) addReferenceIfRefSpecMatches(rs config.RefSpec,
	remoteRefs storer.ReferenceStorer, localRef *plumbing.Reference,
	cmds *[]*packp.Command, forceWithLease *ForceWithLease,
) error {
	if localRef.Type() != plumbing.HashReference {
		return nil
	}

	if !rs.Match(localRef.Name()) {
		return nil
	}

	cmd := &packp.Command{
		Name: rs.Dst(localRef.Name()),
		Old:  plumbing.ZeroHash,
		New:  localRef.Hash(),
	}

	remoteRef, err := remoteRefs.Reference(cmd.Name)
	if err == nil {
		if remoteRef.Type() != plumbing.HashReference {
			// TODO: check actual git behavior here
			return nil
		}

		cmd.Old = remoteRef.Hash()
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return err
	}

	if cmd.Old == cmd.New {
		return nil
	}

	if forceWithLease != nil {
		if err = r.checkForceWithLease(localRef, cmd, forceWithLease); err != nil {
			return err
		}
	} else if !rs.IsForceUpdate() {
		if err := checkTagUpdate(cmd); err != nil {
			return err
		}
		if err := checkFastForwardUpdate(r.s, remoteRefs, cmd); err != nil {
			return err
		}
	}

	*cmds = append(*cmds, cmd)
	return nil
}

func (r *Remote) checkForceWithLease(localRef *plumbing.Reference, cmd *packp.Command, forceWithLease *ForceWithLease) error {
	remotePrefix := fmt.Sprintf("refs/remotes/%s/", r.Config().Name)

	ref, err := storer.ResolveReference(
		r.s,
		plumbing.ReferenceName(remotePrefix+strings.ReplaceAll(localRef.Name().String(), plumbing.RefHeadPrefix, "")),
	)
	if err != nil {
		return err
	}

	if forceWithLease.RefName.String() == "" || (forceWithLease.RefName == cmd.Name) {
		expectedOID := ref.Hash()

		if !forceWithLease.Hash.IsZero() {
			expectedOID = forceWithLease.Hash
		}

		if cmd.Old != expectedOID {
			return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
		}
	}

	return nil
}

func checkTagUpdate(cmd *packp.Command) error {
	if cmd.Name.IsTag() && cmd.Old != plumbing.ZeroHash {
		return fmt.Errorf("tag already exists: %s", cmd.Name.String())
	}

	return nil
}

func checkFastForwardUpdate(s storer.EncodedObjectStorer, remoteRefs storer.ReferenceStorer, cmd *packp.Command) error {
	if cmd.Old.IsZero() {
		_, err := remoteRefs.Reference(cmd.Name)
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil
		}

		if err != nil {
			return err
		}

		return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
	}

	var shallows []plumbing.Hash
	if ss, ok := s.(storer.ShallowStorer); ok {
		shallows, _ = ss.Shallow()
	}

	ff, err := isFastForward(s, cmd.Old, cmd.New, shallows)
	if err != nil {
		return err
	}

	if !ff {
		return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
	}

	return nil
}

func objectsToPush(commands []*packp.Command) []plumbing.Hash {
	objects := make([]plumbing.Hash, 0, len(commands))
	for _, cmd := range commands {
		if cmd.New.IsZero() {
			continue
		}
		objects = append(objects, cmd.New)
	}
	return objects
}

func referencesToHashes(refs storer.ReferenceStorer) ([]plumbing.Hash, error) {
	iter, err := refs.IterReferences()
	if err != nil {
		return nil, err
	}

	var hs []plumbing.Hash
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}

		hs = append(hs, ref.Hash())
		return nil
	})
	if err != nil {
		return nil, err
	}

	return hs, nil
}

func pushHashes(
	ctx context.Context,
	sess transport.Session,
	s storage.Storer,
	cmds []*packp.Command,
	hs []plumbing.Hash,
	allDelete bool,
	o *PushOptions,
) error {
	useRefDeltas := !sess.Capabilities().Supports(capability.OFSDelta)
	rd, wr := io.Pipe()

	config, err := s.Config()
	if err != nil {
		return err
	}

	// Set buffer size to 1 so the error message can be written when
	// ReceivePack fails. Otherwise the goroutine will be blocked writing
	// to the channel.
	done := make(chan error, 1)
	req := &transport.PushRequest{
		Commands: cmds,
		Progress: o.Progress,
		Options:  o.Options,
		Atomic:   o.Atomic,
		Quiet:    o.Quiet,
	}

	if !allDelete {
		req.Packfile = rd
		go func() {
			e := packfile.NewEncoder(wr, s, useRefDeltas)
			if _, err := e.Encode(hs, config.Pack.Window); err != nil {
				done <- wr.CloseWithError(err)
				return
			}

			done <- wr.Close()
		}()
	} else {
		close(done)
	}

	if err := sess.Push(ctx, s, req); err != nil {
		// close the pipe to unlock encode write
		_ = rd.Close()
		return err
	}

	if err := <-done; err != nil {
		return err
	}

	return nil
}

func (r *Remote) checkRequireRemoteRefs(requires []config.RefSpec, remoteRefs storer.ReferenceStorer) error {
	for _, require := range requires {
		if require.IsWildcard() {
			return fmt.Errorf("wildcards not supported in RequireRemoteRefs, got %s", require.String())
		}

		name := require.Dst("")
		remote, err := remoteRefs.Reference(name)
		if err != nil {
			return fmt.Errorf("remote ref %s required to be %s but is absent", name.String(), require.Src())
		}

		var requireHash string
		if require.IsExactSHA1() {
			requireHash = require.Src()
		} else {
			target, err := storer.ResolveReference(remoteRefs, plumbing.ReferenceName(require.Src()))
			if err != nil {
				return fmt.Errorf("could not resolve ref %s in RequireRemoteRefs", require.Src())
			}
			requireHash = target.Hash().String()
		}

		if remote.Hash().String() != requireHash {
			return fmt.Errorf("remote ref %s required to be %s but is %s", name.String(), requireHash, remote.Hash().String())
		}
	}
	return nil
}
