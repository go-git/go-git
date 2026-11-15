package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/internal/repository"
	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// This describes the maximum number of commits to walk when
// computing the haves to send to a server, for each ref in the
// repo containing this remote, when not using the multi-ack
// protocol.  Setting this to 0 means there is no limit.
const maxHavesToVisitPerRef = 100

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

// fetchRefPrefixes derives the ls-refs "ref-prefix" hints for a fetch from its
// refspecs and tag mode, mirroring canonical git (builtin/fetch.c,
// builtin/clone.c). HEAD is always included so default-branch resolution keeps
// working (e.g. on clone), and refs/tags/ is added when tags are being
// followed.
//
// ref-prefix is purely an optimization, so the returned prefixes must cover
// every ref the fetch could match. When a refspec cannot be safely turned into
// a prefix (an exact-OID source) or there are no refspecs, it returns nil to
// request the full advertisement rather than risk under-scoping it.
func fetchRefPrefixes(specs []config.RefSpec, tags plumbing.TagMode) []string {
	if len(specs) == 0 {
		return nil
	}

	prefixes := make([]string, 0, len(specs)+2)
	for _, rs := range specs {
		if rs.IsExactSHA1() {
			return nil
		}
		src := rs.Src()
		if src == "" {
			return nil
		}
		if prefix, _, found := strings.Cut(src, "*"); found {
			// A wildcard: the prefix is the literal part before '*'. A leading
			// wildcard trims to an empty prefix, which would emit an invalid
			// "ref-prefix " argument, so request the full advertisement instead
			// of under-scoping it.
			if prefix == "" {
				return nil
			}
			prefixes = append(prefixes, prefix)
			continue
		}

		// A HEAD source (single-branch clone, "+HEAD:...") resolves through a
		// symref to a branch under refs/heads/. HEAD itself is appended
		// unconditionally below, so advertise that namespace too; otherwise a
		// v2 server that strictly honours ref-prefix omits the resolved branch
		// and it cannot be fetched. Matches git clone.
		if src == "HEAD" {
			prefixes = append(prefixes, plumbing.RefHeadPrefix)
			continue
		}

		// A non-wildcard source may be a short name (e.g. "master"). A v2 server
		// prefix-matches ref-prefix against the full refname, so "master" alone
		// would never match refs/heads/master. Expand it to every candidate
		// full name, mirroring canonical git's refspec_ref_prefixes ->
		// expand_ref_prefix (refspec.c, refs.c).
		for _, rule := range plumbing.RefRevParseRules {
			prefixes = append(prefixes, fmt.Sprintf(rule, src))
		}
	}

	// Order matches canonical git: refspec prefixes, then refs/tags/, then
	// HEAD last (builtin/clone.c, builtin/fetch.c).
	if tags == plumbing.AllTags || tags == plumbing.TagFollowing {
		prefixes = append(prefixes, "refs/tags/")
	}
	prefixes = append(prefixes, "HEAD")
	return prefixes
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
	if trace.Performance.Enabled() {
		start := time.Now()
		defer func() {
			trace.Performance.Printf("performance: %.9f s: git command: git fetch", time.Since(start).Seconds())
		}()
	}

	if r.c == nil {
		return nil, errors.New("cannot fetch: RemoteConfig is nil")
	}

	if o.RemoteName == "" {
		o.RemoteName = r.c.Name
	}

	if err = o.Validate(); err != nil {
		return nil, err
	}

	// Fail before opening a connection rather than after the objects have
	// landed. A filtered fetch stored as an ordinary pack leaves a repository
	// git reports as corrupt and refuses to gc, so if this storage cannot record
	// the pack as coming from a promisor remote, the fetch does not start.
	if o.Filter != "" && !packfile.SupportsPromisorPacks(r.s) {
		return nil, fmt.Errorf("%w: refusing to fetch with filter %q",
			packfile.ErrPromisorPacksUnsupported, o.Filter)
	}

	if len(o.RefSpecs) == 0 {
		o.RefSpecs = r.c.Fetch
	}

	if o.RemoteURL == "" {
		o.RemoteURL = r.c.URLs[0]
	}

	cl, req, err := newClient(o.RemoteURL, o.ClientOptions)
	if err != nil {
		return nil, err
	}

	req.Command = transport.UploadPackService
	req.Protocol = r.transportProtocol()
	sess, err := cl.Handshake(ctx, req)
	if err != nil {
		return nil, err
	}
	defer ioutil.CheckClose(sess, &err)

	if err := r.isSupportedRefSpec(o.RefSpecs, sess.Capabilities()); err != nil {
		return nil, err
	}

	rRefs, err := sess.GetRemoteRefs(ctx, &transport.GetRemoteRefsOptions{
		RefPrefixes: fetchRefPrefixes(o.RefSpecs, o.Tags),
	})
	if err != nil {
		return nil, err
	}

	remoteRefs := referenceStorageFromRefs(rRefs.References, true)
	localRefs, err := reference.References(r.s)
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

	isWildcard := true
	for _, s := range o.RefSpecs {
		if !s.IsWildcard() {
			isWildcard = false
			break
		}
	}

	var haves []plumbing.Hash
	wants, _ := getWants(r.s, refs, o.Depth)
	if len(wants) > 0 {
		haves, err = getHaves(localRefs, remoteRefs, r.s, o.Depth)
		if err != nil {
			return nil, err
		}

		// When performing a shallow fetch, exclude any shallow-boundary commits
		// from the haves list. Shallow commits are already communicated to the
		// server via the "shallow" packets in the upload-request. Including them
		// in HAVE would lead the server to treat their ancestors as present on
		// the client (because HAVE X implies the client has X and all its
		// ancestors), which contradicts the shallow boundary and causes the
		// server to send an empty packfile even when the client is missing
		// objects that are ancestors of its shallow commits.
		if len(shallows) > 0 {
			shallowSet := make(map[plumbing.Hash]bool, len(shallows))
			for _, h := range shallows {
				shallowSet[h] = true
			}
			filtered := haves[:0]
			for _, h := range haves {
				if !shallowSet[h] {
					filtered = append(filtered, h)
				}
			}
			haves = filtered
		}

		req := &transport.FetchRequest{
			Wants:       wants,
			Haves:       haves,
			Depth:       o.Depth,
			Progress:    o.Progress,
			IncludeTags: isWildcard && o.Tags == plumbing.TagFollowing,
			Filter:      o.Filter,
		}

		if err := sess.Fetch(ctx, r.s, req); err != nil && !errors.Is(err, transport.ErrNoChange) {
			// Note: We receive ErrNoChange when remote is the same as local. At
			// this point, we have everything we're asking for.
			return nil, err
		}

		if o.Filter != "" {
			if err := r.recordPromisor(o.Filter); err != nil {
				return nil, err
			}
		}
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
		// No references updated, but may have fetched new objects, check if we now have any of our wants
		for _, hash := range wants {
			exists, _ := objectExists(r.s, hash)
			if exists {
				updated = true
				break
			}
		}

		if !updated {
			return remoteRefs, NoErrAlreadyUpToDate
		}
	}

	return remoteRefs, nil
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

// recordPromisor marks this remote as a promisor remote and stores the filter
// that was used, mirroring what git records for a partial clone.
//
// Both keys matter. promisor is what lets git accept that the filtered-out
// objects are absent on purpose, and partialclonefilter is what makes git
// reapply the same filter on later fetches. Recording the first without the
// second leaves the repository fetching unfiltered while missing objects, which
// fails in index-pack resolving deltas against bases it never receives.
//
// The filter is recorded once and then left alone, which is git's behaviour: it
// is the default reapplied to later fetches, not a record of the most recent
// one.
//
// Nothing is recorded for a fetch that did not come from a configured remote:
// an anonymous URL fetch has no remote section to write to, and git leaves the
// configured remotes alone in that case too.
func (r *Remote) recordPromisor(filter packp.Filter) error {
	if r.s == nil || r.c == nil || r.c.Name == "" {
		return nil
	}

	cfg, err := r.s.Config()
	if err != nil {
		return err
	}

	remote, ok := cfg.Remotes[r.c.Name]
	if !ok {
		return nil
	}

	// A filter already recorded for this remote is left alone, even when this
	// fetch used a different one. Git treats the first filter as the default to
	// reapply to later fetches and does not rewrite it
	// (list-objects-filter-options.c partial_clone_register returns early once
	// the remote has a partialclonefilter), so overwriting it here would change
	// what an unfiltered `git fetch` does afterwards.
	if remote.Promisor && remote.PartialCloneFilter != "" {
		return nil
	}

	if !remote.Promisor {
		remote.Promisor = true

		// Partial clone is a repository format extension, so the format
		// version has to allow extensions to be present at all. Git raises it
		// when first registering the remote, for the same reason.
		cfg.Core.RepositoryFormatVersion = formatcfg.Version1
	}

	remote.PartialCloneFilter = string(filter)

	if err := r.s.SetConfig(cfg); err != nil {
		return err
	}

	// Keep the in-memory view consistent with what was just stored, so a
	// caller holding this Remote sees the recorded filter.
	r.c.Promisor = remote.Promisor
	r.c.PartialCloneFilter = remote.PartialCloneFilter

	return nil
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

	commit, err := object.GetCommit(s, h)
	if err != nil {
		if !errors.Is(err, plumbing.ErrObjectNotFound) {
			// Ignore the error if this isn't a commit.
			haves[ref.Hash()] = true
		}
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

	result := make([]plumbing.Hash, 0, len(haves))
	for h := range haves {
		result = append(result, h)
	}

	return result, nil
}

const refspecAllTags = "refs/tags/*:refs/tags/*"

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
		return nil, fmt.Errorf("%w: %s", ErrRemoteRefNotFound, s.Src())
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

	result := make([]plumbing.Hash, 0, len(wants))
	for h := range wants {
		result = append(result, h)
	}

	return result, nil
}

func objectExists(s storer.EncodedObjectStorer, h plumbing.Hash) (bool, error) {
	_, err := s.EncodedObject(plumbing.AnyObject, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return false, nil
	}

	return true, err
}

func (r *Remote) isSupportedRefSpec(refs []config.RefSpec, caps *capability.List) error {
	var containsIsExact bool
	for _, ref := range refs {
		if ref.IsExactSHA1() {
			containsIsExact = true
		}
	}

	if !containsIsExact {
		return nil
	}

	if caps.Supports(capability.AllowReachableSHA1InWant) ||
		caps.Supports(capability.AllowTipSHA1InWant) {
		return nil
	}

	return ErrExactSHA1NotSupported
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

	shallows, _ := r.s.Shallow()

	for i, spec := range specs {
		if !spec.IsWildcard() {
			isWildcard = false
		}

		for _, ref := range specToRefs[i] {
			if ref.Type() != plumbing.HashReference {
				continue
			}

			localName := spec.Dst(ref.Name())
			// If localName doesn't start with "refs/" then treat as a branch,
			// unless localName is itself a SHA-1/SHA-256 hash (as happens when
			// a caller uses a bare-hash dst such as "+<hash>:<hash>"). Creating
			// a branch named after a commit hash is always wrong and produces
			// spurious refs that confuse ResolveRevision and other callers.
			if !localName.IsUnderRefs() {
				if plumbing.IsHash(localName.String()) {
					// Bare-hash dst: the intent is to fetch the object only;
					// no local reference should be created.
					continue
				}
				localName = plumbing.NewBranchReferenceName(localName.String())
			}
			old, _ := storer.ResolveReference(r.s, localName)
			newRef := plumbing.NewHashReference(localName, ref.Hash())

			if old != nil && localName.IsTag() && old.Hash() != newRef.Hash() && !force && !spec.IsForceUpdate() {
				forceNeeded = true
				continue
			}

			// If the ref exists locally as a non-tag and force is not
			// specified, only update if the new ref is an ancestor of the old
			if old != nil && !old.Name().IsTag() && !force && !spec.IsForceUpdate() {
				ff, err := isFastForward(r.s, old.Hash(), newRef.Hash(), shallows)
				if err != nil {
					return updated, err
				}

				if !ff {
					forceNeeded = true
					continue
				}
			}

			refUpdated, err := checkAndUpdateReferenceStorerIfNeeded(r.s, newRef, old)
			if unstorableRefName(localName, ref.Name(), err) {
				continue
			}
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
	tagUpdated, tagForceNeeded, err := r.buildFetchedTags(tags, tagMode == plumbing.AllTags, force)
	if err != nil {
		return updated, err
	}

	if tagUpdated {
		updated = true
	}
	if tagForceNeeded {
		forceNeeded = true
	}

	if forceNeeded {
		err = ErrForceNeeded
	}

	return updated, err
}

func (r *Remote) buildFetchedTags(refs memory.ReferenceStorage, allTags, force bool) (updated, forceNeeded bool, err error) {
	for _, ref := range refs {
		if !ref.Name().IsTag() {
			continue
		}

		_, err := r.s.EncodedObject(plumbing.AnyObject, ref.Hash())
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			continue
		}

		if err != nil {
			return updated, forceNeeded, err
		}

		old, err := r.s.Reference(ref.Name())
		if unstorableRefName(ref.Name(), ref.Name(), err) {
			continue
		}
		if err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return updated, forceNeeded, err
		}
		if err == nil && old.Hash() != ref.Hash() {
			if !allTags {
				// An auto-followed tag only creates one that is missing locally; it
				// never moves a tag that already points elsewhere.
				continue
			}
			if !force {
				forceNeeded = true
				continue
			}
		}

		refUpdated, err := updateReferenceStorerIfNeeded(r.s, ref)
		if unstorableRefName(ref.Name(), ref.Name(), err) {
			continue
		}
		if err != nil {
			return updated, forceNeeded, err
		}

		if refUpdated {
			updated = true
		}
	}

	return updated, forceNeeded, err
}
