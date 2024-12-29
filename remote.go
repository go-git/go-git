package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/internal/repository"
	"github.com/go-git/go-git/v5/internal/url"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

var (
	NoErrAlreadyUpToDate     = errors.New("already up-to-date")
	ErrDeleteRefNotSupported = errors.New("server does not support delete-refs")
	ErrForceNeeded           = errors.New("some refs were not updated")
	ErrExactSHA1NotSupported = errors.New("server does not support exact SHA1 refspec")
	ErrEmptyUrls             = errors.New("URLs cannot be empty")
)

type NoMatchingRefSpecError struct {
	refSpec config.RefSpec
}

func (e NoMatchingRefSpecError) Error() string {
	return fmt.Sprintf("couldn't find remote ref %q", e.refSpec.Src())
}

func (e NoMatchingRefSpecError) Is(target error) bool {
	_, ok := target.(NoMatchingRefSpecError)
	return ok
}

const (
	// This describes the maximum number of commits to walk when
	// computing the haves to send to a server, for each ref in the
	// repo containing this remote, when not using the multi-ack
	// protocol.  Setting this to 0 means there is no limit.
	maxHavesToVisitPerRef = 100

	// peeledSuffix is the suffix used to build peeled reference names.
	peeledSuffix = "^{}"
)

// Remote represents a connection to a remote repository.
type Remote struct {
	c *config.RemoteConfig
	s storage.Storer
}

// NewRemote creates a new Remote.
// The intended purpose is to use the Remote for tasks such as listing remote references (like using git ls-remote).
// Otherwise Remotes should be created via the use of a Repository.
func NewRemote(s storage.Storer, c *config.RemoteConfig) *Remote {
	return &Remote{s: s, c: c}
}

// Config returns the RemoteConfig object used to instantiate this Remote.
func (r *Remote) Config() *config.RemoteConfig {
	return r.c
}

func (r *Remote) String() string {
	var fetch, push string
	if len(r.c.URLs) > 0 {
		fetch = r.c.URLs[0]
		push = r.c.URLs[len(r.c.URLs)-1]
	}

	return fmt.Sprintf("%s\t%s (fetch)\n%[1]s\t%[3]s (push)", r.c.Name, fetch, push)
}

// Push performs a push to the remote. Returns NoErrAlreadyUpToDate if the
// remote was already up-to-date.
func (r *Remote) Push(o *PushOptions) error {
	return r.PushContext(context.Background(), o)
}

// PushContext performs a push to the remote. Returns NoErrAlreadyUpToDate if
// the remote was already up-to-date.
//
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned. The context only affects the
// transport operations.
func (r *Remote) PushContext(ctx context.Context, o *PushOptions) (err error) {
	if err := o.Validate(); err != nil {
		return err
	}

	if o.RemoteName != r.c.Name {
		return fmt.Errorf("remote names don't match: %s != %s", o.RemoteName, r.c.Name)
	}

	if o.RemoteURL == "" && len(r.c.URLs) > 0 {
		o.RemoteURL = r.c.URLs[len(r.c.URLs)-1]
	}

	c, ep, err := newClient(o.RemoteURL, o.InsecureSkipTLS, o.CABundle, o.ProxyOptions)
	if err != nil {
		return err
	}

	s, err := c.NewSession(r.s, ep, o.Auth)
	if err != nil {
		return err
	}

	conn, err := s.Handshake(ctx, transport.ReceivePackService)
	if err != nil {
		return err
	}

	rRefs, err := conn.GetRemoteRefs(ctx)
	if err != nil {
		return err
	}

	remoteRefs := referenceStorageFromRefs(rRefs, true)
	if err := r.checkRequireRemoteRefs(o.RequireRemoteRefs, remoteRefs); err != nil {
		return err
	}

	return r.sendPack(ctx, conn, remoteRefs, o)
}

func (r *Remote) sendPack(ctx context.Context, conn transport.Connection, remoteRefs storer.ReferenceStorer, o *PushOptions) error {
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
	caps := conn.Capabilities() // server capabilities
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

	localRefs, err := r.references()
	if err != nil {
		return err
	}

	cmds := make([]*packp.Command, 0)
	if err := r.addReferencesToUpdate(o.RefSpecs, localRefs, remoteRefs, &cmds, o.Prune, o.ForceWithLease); err != nil {
		return err
	}

	if o.FollowTags {
		if err := r.addReachableTags(localRefs, remoteRefs, &cmds); err != nil {
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
		if url.IsLocalEndpoint(o.RemoteURL) {
			// If we're are pushing to a local repo, it might be much
			// faster to use a local storage layer to get the commits
			// to ignore, when calculating the object revlist.
			localStorer := filesystem.NewStorage(
				osfs.New(o.RemoteURL, osfs.WithBoundOS()), cache.NewObjectLRUDefault())
			hashesToPush, err = revlist.ObjectsWithStorageForIgnores(
				r.s, localStorer, objects, haves)
		} else {
			hashesToPush, err = revlist.Objects(r.s, objects, haves)
		}
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

	if err := pushHashes(ctx, conn, r.s, cmds, hashesToPush, allDelete, o); err != nil {
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

// FetchContext fetches references along with the objects necessary to complete
// their histories.
//
// Returns nil if the operation is successful, NoErrAlreadyUpToDate if there are
// no changes to be fetched, or an error.
//
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned. The context only affects the
// transport operations.
func (r *Remote) FetchContext(ctx context.Context, o *FetchOptions) error {
	_, err := r.fetch(ctx, o)
	return err
}

// Fetch fetches references along with the objects necessary to complete their
// histories.
//
// Returns nil if the operation is successful, NoErrAlreadyUpToDate if there are
// no changes to be fetched, or an error.
func (r *Remote) Fetch(o *FetchOptions) error {
	return r.FetchContext(context.Background(), o)
}

func (r *Remote) fetch(ctx context.Context, o *FetchOptions) (sto storer.ReferenceStorer, err error) {
	if o.RemoteName == "" {
		o.RemoteName = r.c.Name
	}

	if err = o.Validate(); err != nil {
		return nil, err
	}

	if len(o.RefSpecs) == 0 {
		o.RefSpecs = r.c.Fetch
	}

	if o.RemoteURL == "" {
		o.RemoteURL = r.c.URLs[0]
	}

	c, ep, err := newClient(o.RemoteURL, o.InsecureSkipTLS, o.CABundle, o.ProxyOptions)
	if err != nil {
		return nil, err
	}

	sess, err := c.NewSession(r.s, ep, o.Auth)
	if err != nil {
		return nil, err
	}

	conn, err := sess.Handshake(ctx, transport.UploadPackService)
	if err != nil {
		return nil, err
	}

	rRefs, err := conn.GetRemoteRefs(ctx)
	if err != nil {
		return nil, err
	}

	remoteRefs := referenceStorageFromRefs(rRefs, true)
	localRefs, err := r.references()
	if err != nil {
		return nil, err
	}
	refs, specToRefs, err := calculateRefs(o.RefSpecs, remoteRefs, o.Tags)
	if err != nil {
		return nil, err
	}

	var shallows []plumbing.Hash
	if o.Depth != 0 {
		shallows, err = r.s.Shallow()
		if err != nil {
			return nil, err
		}
	}

	var haves []plumbing.Hash
	wants, _ := getWants(r.s, refs, o.Depth)
	if len(wants) > 0 {
		haves, err = getHaves(localRefs, remoteRefs, r.s, o.Depth)
		if err != nil {
			return nil, err
		}

		isWildcard := true
		for _, s := range o.RefSpecs {
			if !s.IsWildcard() {
				isWildcard = false
				break
			}
		}

		req := &transport.FetchRequest{
			Wants:       wants,
			Haves:       haves,
			Depth:       o.Depth,
			Progress:    o.Progress,
			IncludeTags: isWildcard && o.Tags == plumbing.TagFollowing,
		}

		if err := conn.Fetch(ctx, req); err != nil && !errors.Is(err, transport.ErrNoChange) {
			// Note: We receive ErrNoChange when remote is the same as local. At
			// this point, we have everything we're asking for.
			return nil, err
		}
	}

	if err := conn.Close(); err != nil {
		return nil, fmt.Errorf("error closing connection: %v", err)
	}

	var updatedPrune bool
	if o.Prune {
		updatedPrune, err = r.pruneRemotes(o.RefSpecs, localRefs, remoteRefs)
		if err != nil {
			return nil, err
		}
	}

	updated, err := r.updateLocalReferenceStorage(o.RefSpecs, refs, remoteRefs, specToRefs, o.Tags, o.Force)
	if err != nil {
		return nil, err
	}

	if !updated {
		updated, err = depthChanged(shallows, r.s)
		if err != nil {
			return nil, fmt.Errorf("error checking depth change: %v", err)
		}
	}

	if !updated && !updatedPrune {
		return remoteRefs, NoErrAlreadyUpToDate
	}

	return remoteRefs, nil
}

func referenceStorageFromRefs(refs []*plumbing.Reference, filterPeeled bool) memory.ReferenceStorage {
	refStore := memory.ReferenceStorage{}
	for _, ref := range refs {
		if filterPeeled && strings.HasSuffix(ref.Name().String(), peeledSuffix) {
			continue
		}
		refStore.SetReference(ref) // nolint: errcheck
	}
	return refStore
}

func depthChanged(before []plumbing.Hash, s storage.Storer) (bool, error) {
	after, err := s.Shallow()
	if err != nil {
		return false, err
	}

	if len(before) != len(after) {
		return true, nil
	}

	bm := make(map[plumbing.Hash]bool, len(before))
	for _, b := range before {
		bm[b] = true
	}
	for _, a := range after {
		if _, ok := bm[a]; !ok {
			return true, nil
		}
	}

	return false, nil
}

func newClient(url string, insecure bool, cabundle []byte, proxyOpts transport.ProxyOptions) (transport.Transport, *transport.Endpoint, error) {
	ep, err := transport.NewEndpoint(url)
	if err != nil {
		return nil, nil, err
	}
	ep.InsecureSkipTLS = insecure
	ep.CaBundle = cabundle
	ep.Proxy = proxyOpts

	c, err := transport.Get(ep.Protocol)
	if err != nil {
		return nil, nil, err
	}

	return c, ep, err
}

func (r *Remote) pruneRemotes(specs []config.RefSpec, localRefs []*plumbing.Reference, remoteRefs storer.ReferenceStorer) (bool, error) {
	var updatedPrune bool
	for _, spec := range specs {
		rev := spec.Reverse()
		for _, ref := range localRefs {
			if !rev.Match(ref.Name()) {
				continue
			}
			_, err := remoteRefs.Reference(rev.Dst(ref.Name()))
			if errors.Is(err, plumbing.ErrReferenceNotFound) {
				updatedPrune = true
				err := r.s.RemoveReference(ref.Name())
				if err != nil {
					return false, err
				}
			}
		}
	}
	return updatedPrune, nil
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
			commit, err := object.GetCommit(r.s, plumbing.NewHash(rs.Src()))
			if err == nil {
				return r.addCommit(rs, remoteRefs, commit.Hash, cmds)
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

func (r *Remote) addCommit(rs config.RefSpec,
	remoteRefs storer.ReferenceStorer, localCommit plumbing.Hash,
	cmds *[]*packp.Command,
) error {
	if rs.IsWildcard() {
		return errors.New("can't use wildcard together with hash refspecs")
	}

	cmd := &packp.Command{
		Name: rs.Dst(""),
		Old:  plumbing.ZeroHash,
		New:  localCommit,
	}
	remoteRef, err := remoteRefs.Reference(cmd.Name)
	if err == nil {
		if remoteRef.Type() != plumbing.HashReference {
			// TODO: check actual git behavior here
			return nil
		}

		cmd.Old = remoteRef.Hash()
	} else if err != plumbing.ErrReferenceNotFound {
		return err
	}
	if cmd.Old == cmd.New {
		return nil
	}
	if !rs.IsForceUpdate() {
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
	} else if err != plumbing.ErrReferenceNotFound {
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
		plumbing.ReferenceName(remotePrefix+strings.ReplaceAll(localRef.Name().String(), "refs/heads/", "")))
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

func (r *Remote) references() ([]*plumbing.Reference, error) {
	var localRefs []*plumbing.Reference

	iter, err := r.s.IterReferences()
	if err != nil {
		return nil, err
	}

	for {
		ref, err := iter.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		localRefs = append(localRefs, ref)
	}

	return localRefs, nil
}

func getRemoteRefsFromStorer(remoteRefStorer storer.ReferenceStorer) (
	map[plumbing.Hash]bool, error,
) {
	remoteRefs := map[plumbing.Hash]bool{}
	iter, err := remoteRefStorer.IterReferences()
	if err != nil {
		return nil, err
	}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}
		remoteRefs[ref.Hash()] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return remoteRefs, nil
}

// getHavesFromRef populates the given `haves` map with the given
// reference, and up to `maxHavesToVisitPerRef` ancestor commits.
func getHavesFromRef(
	ref *plumbing.Reference,
	remoteRefs map[plumbing.Hash]bool,
	s storage.Storer,
	haves map[plumbing.Hash]bool,
	depth int,
) error {
	h := ref.Hash()
	if haves[h] {
		return nil
	}

	// No need to load the commit if we know the remote already
	// has this hash.
	if remoteRefs[h] {
		haves[h] = true
		return nil
	}

	commit, err := object.GetCommit(s, h)
	if err != nil {
		// Ignore the error if this isn't a commit.
		haves[ref.Hash()] = true
		return nil
	}

	// Until go-git supports proper commit negotiation during an
	// upload pack request, include up to `maxHavesToVisitPerRef`
	// commits from the history of each ref.
	walker := object.NewCommitPreorderIter(commit, haves, nil)
	toVisit := maxHavesToVisitPerRef
	// But only need up to the requested depth
	if depth > 0 && depth < maxHavesToVisitPerRef {
		toVisit = depth
	}
	// It is safe to ignore any error here as we are just trying to find the references that we already have
	// An example of a legitimate failure is we have a shallow clone and don't have the previous commit(s)
	_ = walker.ForEach(func(c *object.Commit) error {
		haves[c.Hash] = true
		toVisit--
		// If toVisit starts out at 0 (indicating there is no
		// max), then it will be negative here and we won't stop
		// early.
		if toVisit == 0 || remoteRefs[c.Hash] {
			return storer.ErrStop
		}
		return nil
	})

	return nil
}

func getHaves(
	localRefs []*plumbing.Reference,
	remoteRefStorer storer.ReferenceStorer,
	s storage.Storer,
	depth int,
) ([]plumbing.Hash, error) {
	haves := map[plumbing.Hash]bool{}

	// Build a map of all the remote references, to avoid loading too
	// many parent commits for references we know don't need to be
	// transferred.
	remoteRefs, err := getRemoteRefsFromStorer(remoteRefStorer)
	if err != nil {
		return nil, err
	}

	for _, ref := range localRefs {
		if haves[ref.Hash()] {
			continue
		}

		if ref.Type() != plumbing.HashReference {
			continue
		}

		err = getHavesFromRef(ref, remoteRefs, s, haves, depth)
		if err != nil {
			return nil, err
		}
	}

	var result []plumbing.Hash
	for h := range haves {
		result = append(result, h)
	}

	return result, nil
}

const refspecAllTags = "+refs/tags/*:refs/tags/*"

func calculateRefs(
	spec []config.RefSpec,
	remoteRefs storer.ReferenceStorer,
	tagMode plumbing.TagMode,
) (memory.ReferenceStorage, [][]*plumbing.Reference, error) {
	if tagMode == plumbing.AllTags {
		spec = append(spec, refspecAllTags)
	}

	refs := make(memory.ReferenceStorage)
	// list of references matched for each spec
	specToRefs := make([][]*plumbing.Reference, len(spec))
	for i := range spec {
		var err error
		specToRefs[i], err = doCalculateRefs(spec[i], remoteRefs, refs)
		if err != nil {
			return nil, nil, err
		}
	}

	return refs, specToRefs, nil
}

func doCalculateRefs(
	s config.RefSpec,
	remoteRefs storer.ReferenceStorer,
	refs memory.ReferenceStorage,
) ([]*plumbing.Reference, error) {
	var refList []*plumbing.Reference

	if s.IsExactSHA1() {
		ref := plumbing.NewHashReference(s.Dst(""), plumbing.NewHash(s.Src()))

		refList = append(refList, ref)
		return refList, refs.SetReference(ref)
	}

	var matched bool
	onMatched := func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.SymbolicReference {
			target, err := storer.ResolveReference(remoteRefs, ref.Name())
			if err != nil {
				return err
			}

			ref = plumbing.NewHashReference(ref.Name(), target.Hash())
		}

		if ref.Type() != plumbing.HashReference {
			return nil
		}

		matched = true
		refList = append(refList, ref)
		return refs.SetReference(ref)
	}

	var ret error
	if s.IsWildcard() {
		iter, err := remoteRefs.IterReferences()
		if err != nil {
			return nil, err
		}
		ret = iter.ForEach(func(ref *plumbing.Reference) error {
			if !s.Match(ref.Name()) {
				return nil
			}

			return onMatched(ref)
		})
	} else {
		var resolvedRef *plumbing.Reference
		src := s.Src()
		resolvedRef, ret = repository.ExpandRef(remoteRefs, plumbing.ReferenceName(src))
		if ret == nil {
			ret = onMatched(resolvedRef)
		}
	}

	if !matched && !s.IsWildcard() {
		return nil, NoMatchingRefSpecError{refSpec: s}
	}

	return refList, ret
}

func getWants(localStorer storage.Storer, refs memory.ReferenceStorage, depth int) ([]plumbing.Hash, error) {
	// If depth is anything other than 1 and the repo has shallow commits then just because we have the commit
	// at the reference doesn't mean that we don't still need to fetch the parents
	shallow := false
	if depth != 1 {
		if s, _ := localStorer.Shallow(); len(s) > 0 {
			shallow = true
		}
	}

	wants := map[plumbing.Hash]bool{}
	for _, ref := range refs {
		hash := ref.Hash()
		exists, err := objectExists(localStorer, ref.Hash())
		if err != nil {
			return nil, err
		}

		if !exists || shallow {
			wants[hash] = true
		}
	}

	var result []plumbing.Hash
	for h := range wants {
		result = append(result, h)
	}

	return result, nil
}

func objectExists(s storer.EncodedObjectStorer, h plumbing.Hash) (bool, error) {
	_, err := s.EncodedObject(plumbing.AnyObject, h)
	if err == plumbing.ErrObjectNotFound {
		return false, nil
	}

	return true, err
}

func checkFastForwardUpdate(s storer.EncodedObjectStorer, remoteRefs storer.ReferenceStorer, cmd *packp.Command) error {
	if cmd.Old == plumbing.ZeroHash {
		_, err := remoteRefs.Reference(cmd.Name)
		if err == plumbing.ErrReferenceNotFound {
			return nil
		}

		if err != nil {
			return err
		}

		return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
	}

	ff, err := isFastForward(s, cmd.Old, cmd.New, nil)
	if err != nil {
		return err
	}

	if !ff {
		return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
	}

	return nil
}

func isFastForward(s storer.EncodedObjectStorer, old, new plumbing.Hash, earliestShallow *plumbing.Hash) (bool, error) {
	c, err := object.GetCommit(s, new)
	if err != nil {
		return false, err
	}

	parentsToIgnore := []plumbing.Hash{}
	if earliestShallow != nil {
		earliestCommit, err := object.GetCommit(s, *earliestShallow)
		if err != nil {
			return false, err
		}

		parentsToIgnore = earliestCommit.ParentHashes
	}

	found := false
	// stop iterating at the earliest shallow commit, ignoring its parents
	// note: when pull depth is smaller than the number of new changes on the remote, this fails due to missing parents.
	//       as far as i can tell, without the commits in-between the shallow pull and the earliest shallow, there's no
	//       real way of telling whether it will be a fast-forward merge.
	iter := object.NewCommitPreorderIter(c, nil, parentsToIgnore)
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash != old {
			return nil
		}

		found = true
		return storer.ErrStop
	})
	return found, err
}

func (r *Remote) isSupportedRefSpec(refs []config.RefSpec, ar *packp.AdvRefs) error {
	var containsIsExact bool
	for _, ref := range refs {
		if ref.IsExactSHA1() {
			containsIsExact = true
		}
	}

	if !containsIsExact {
		return nil
	}

	if ar.Capabilities.Supports(capability.AllowReachableSHA1InWant) ||
		ar.Capabilities.Supports(capability.AllowTipSHA1InWant) {
		return nil
	}

	return ErrExactSHA1NotSupported
}

func buildSidebandIfSupported(l *capability.List, reader io.Reader, p sideband.Progress) io.Reader {
	var t sideband.Type

	switch {
	case l.Supports(capability.Sideband):
		t = sideband.Sideband
	case l.Supports(capability.Sideband64k):
		t = sideband.Sideband64k
	default:
		return reader
	}

	d := sideband.NewDemuxer(t, reader)
	d.Progress = p

	return d
}

func (r *Remote) updateLocalReferenceStorage(
	specs []config.RefSpec,
	fetchedRefs, remoteRefs memory.ReferenceStorage,
	specToRefs [][]*plumbing.Reference,
	tagMode plumbing.TagMode,
	force bool,
) (updated bool, err error) {
	isWildcard := true
	forceNeeded := false

	for i, spec := range specs {
		if !spec.IsWildcard() {
			isWildcard = false
		}

		for _, ref := range specToRefs[i] {
			if ref.Type() != plumbing.HashReference {
				continue
			}

			localName := spec.Dst(ref.Name())
			// If localName doesn't start with "refs/" then treat as a branch.
			if !strings.HasPrefix(localName.String(), "refs/") {
				localName = plumbing.NewBranchReferenceName(localName.String())
			}
			old, _ := storer.ResolveReference(r.s, localName)
			new := plumbing.NewHashReference(localName, ref.Hash())

			// If the ref exists locally as a non-tag and force is not
			// specified, only update if the new ref is an ancestor of the old
			if old != nil && !old.Name().IsTag() && !force && !spec.IsForceUpdate() {
				ff, err := isFastForward(r.s, old.Hash(), new.Hash(), nil)
				if err != nil {
					return updated, err
				}

				if !ff {
					forceNeeded = true
					continue
				}
			}

			refUpdated, err := checkAndUpdateReferenceStorerIfNeeded(r.s, new, old)
			if err != nil {
				return updated, err
			}

			if refUpdated {
				updated = true
			}
		}
	}

	if tagMode == plumbing.NoTags {
		return updated, nil
	}

	tags := fetchedRefs
	if isWildcard {
		tags = remoteRefs
	}
	tagUpdated, err := r.buildFetchedTags(tags)
	if err != nil {
		return updated, err
	}

	if tagUpdated {
		updated = true
	}

	if forceNeeded {
		err = ErrForceNeeded
	}

	return
}

func (r *Remote) buildFetchedTags(refs memory.ReferenceStorage) (updated bool, err error) {
	for _, ref := range refs {
		if !ref.Name().IsTag() {
			continue
		}

		_, err := r.s.EncodedObject(plumbing.AnyObject, ref.Hash())
		if err == plumbing.ErrObjectNotFound {
			continue
		}

		if err != nil {
			return false, err
		}

		refUpdated, err := updateReferenceStorerIfNeeded(r.s, ref)
		if err != nil {
			return updated, err
		}

		if refUpdated {
			updated = true
		}
	}

	return
}

// List the references on the remote repository.
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned. The context only affects to the
// transport operations.
func (r *Remote) ListContext(ctx context.Context, o *ListOptions) (rfs []*plumbing.Reference, err error) {
	return r.list(ctx, o)
}

func (r *Remote) List(o *ListOptions) (rfs []*plumbing.Reference, err error) {
	timeout := o.Timeout
	// Default to the old hardcoded 10s value if a timeout is not explicitly set.
	if timeout == 0 {
		timeout = 10
	}
	if timeout < 0 {
		return nil, fmt.Errorf("invalid timeout: %d", timeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	return r.ListContext(ctx, o)
}

func (r *Remote) list(ctx context.Context, o *ListOptions) (rfs []*plumbing.Reference, err error) {
	if r.c == nil || len(r.c.URLs) == 0 {
		return nil, ErrEmptyUrls
	}

	c, ep, err := newClient(r.c.URLs[0], o.InsecureSkipTLS, o.CABundle, o.ProxyOptions)
	if err != nil {
		return nil, err
	}

	s, err := c.NewSession(r.s, ep, o.Auth)
	if err != nil {
		return nil, err
	}

	conn, err := s.Handshake(ctx, transport.UploadPackService)
	if err != nil {
		return nil, err
	}

	defer ioutil.CheckClose(conn, &err)

	allRefs, err := conn.GetRemoteRefs(ctx)
	if err != nil {
		return nil, err
	}

	refs := storer.NewReferenceSliceIter(allRefs)

	var resultRefs []*plumbing.Reference
	if o.PeelingOption == AppendPeeled || o.PeelingOption == IgnorePeeled {
		err = refs.ForEach(func(ref *plumbing.Reference) error {
			resultRefs = append(resultRefs, ref)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	if o.PeelingOption == AppendPeeled || o.PeelingOption == OnlyPeeled {
		for _, ref := range allRefs {
			if strings.HasSuffix(ref.Name().String(), "^{}") {
				resultRefs = append(resultRefs, ref)
			}
		}
	}

	return resultRefs, nil
}

func objectsToPush(commands []*packp.Command) []plumbing.Hash {
	objects := make([]plumbing.Hash, 0, len(commands))
	for _, cmd := range commands {
		if cmd.New == plumbing.ZeroHash {
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
	conn transport.Connection,
	s storage.Storer,
	cmds []*packp.Command,
	hs []plumbing.Hash,
	allDelete bool,
	o *PushOptions,
) error {
	useRefDeltas := !conn.Capabilities().Supports(capability.OFSDelta)
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

	if err := conn.Push(ctx, req); err != nil {
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
