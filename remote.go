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
	"github.com/go-git/go-git/v6/internal/repository"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// Remote operation errors and sentinel values.
var (
	NoErrAlreadyUpToDate     = errors.New("already up-to-date") //nolint:staticcheck,revive // sentinel value, not an error
	ErrDeleteRefNotSupported = errors.New("server does not support delete-refs")
	ErrForceNeeded           = errors.New("some refs were not updated")
	ErrExactSHA1NotSupported = errors.New("server does not support exact SHA1 refspec")
	ErrEmptyUrls             = errors.New("URLs cannot be empty")
	ErrRemoteRefNotFound     = errors.New("couldn't find remote ref")
)

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

// Push performs a push to the remote. Returns NoErrAlreadyUpToDate if
// the remote was already up-to-date.
//
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned.
func (r *Remote) Push(ctx context.Context, o *PushOptions) (err error) {
	if trace.Performance.Enabled() {
		start := time.Now()
		defer func() {
			trace.Performance.Printf("performance: %.9f s: git command: git push", time.Since(start).Seconds())
		}()
	}

	if err := o.Validate(); err != nil {
		return err
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

	remoteRefs := referenceStorageFromRefs(ctx, rRefs.References, true)
	if err := r.checkRequireRemoteRefs(ctx, o.RequireRemoteRefs, remoteRefs); err != nil {
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

	localRefs, err := reference.References(ctx, r.s)
	if err != nil {
		return err
	}

	cmds := make([]*packp.Command, 0)
	if err := r.addReferencesToUpdate(ctx, o.RefSpecs, localRefs, remoteRefs, &cmds, o.Prune, o.ForceWithLease); err != nil {
		return err
	}

	if o.FollowTags {
		if err := r.addReachableTags(ctx, localRefs, remoteRefs, &cmds); err != nil {
			return err
		}
	}

	if len(cmds) == 0 {
		return NoErrAlreadyUpToDate
	}

	objects := objectsToPush(cmds)
	haves, err := referencesToHashes(ctx, remoteRefs)
	if err != nil {
		return err
	}

	stop, err := r.s.Shallow(ctx)
	if err != nil {
		return err
	}

	// if we have shallow we should include this as part of the objects that
	// we are aware.
	haves = append(haves, stop...)

	var hashesToPush []plumbing.Hash
	// Avoid the expensive revlist operation if we're only doing deletes.
	if !allDelete {
		hashesToPush, err = revlist.Objects(ctx, r.s, objects, haves)
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

	return r.updateRemoteReferenceStorage(ctx, cmds)
}

func (r *Remote) useRefDeltas(ar *packp.AdvRefs) bool {
	return !ar.Capabilities.Supports(capability.OFSDelta)
}

func (r *Remote) addReachableTags(ctx context.Context, localRefs []*plumbing.Reference, remoteRefs storer.ReferenceStorer, cmds *[]*packp.Command) error {
	tags := make(map[plumbing.Reference]struct{})
	// get a list of all tags locally
	for _, ref := range localRefs {
		if strings.HasPrefix(string(ref.Name()), "refs/tags") {
			tags[*ref] = struct{}{}
		}
	}

	remoteRefIter, err := remoteRefs.IterReferences(ctx)
	if err != nil {
		return err
	}

	// remove any that are already on the remote
	if err := remoteRefIter.ForEach(ctx, func(reference *plumbing.Reference) error {
		delete(tags, *reference)
		return nil
	}); err != nil {
		return err
	}

	for tag := range tags {
		tagObject, err := object.GetObject(ctx, r.s, tag.Hash())
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

		tagCommit, err = object.GetCommit(ctx, r.s, annotatedTag.Target)
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

			c, err := object.GetCommit(ctx, r.s, cmd.New)
			if err != nil {
				return fmt.Errorf("get commit %v: %w", cmd.Name, err)
			}

			if isAncestor, err := tagCommit.IsAncestor(ctx, c); err == nil && isAncestor {
				*cmds = append(*cmds, &packp.Command{Name: tag.Name(), New: tag.Hash()})
			}
		}
	}

	return nil
}

func (r *Remote) updateRemoteReferenceStorage(
	ctx context.Context,
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
				if err := r.s.SetReference(ctx, ref); err != nil {
					return err
				}
			case packp.Delete:
				if err := r.s.RemoveReference(ctx, local); err != nil {
					return err
				}
			}
		}
	}

	return nil
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
			prefixes = append(prefixes, "refs/heads/")
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
//
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned.
func (r *Remote) Fetch(ctx context.Context, o *FetchOptions) error {
	_, err := r.fetch(ctx, o)
	return err
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
	req.Protocol = r.transportProtocol(ctx)
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

	remoteRefs := referenceStorageFromRefs(ctx, rRefs.References, true)
	localRefs, err := reference.References(ctx, r.s)
	if err != nil {
		return nil, err
	}
	refs, specToRefs, err := calculateRefs(ctx, o.RefSpecs, remoteRefs, o.Tags)
	if err != nil {
		return nil, err
	}

	var shallows []plumbing.Hash
	if o.Depth != 0 {
		shallows, err = r.s.Shallow(ctx)
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
	wants, _ := getWants(ctx, r.s, refs, o.Depth)
	if len(wants) > 0 {
		haves, err = getHaves(ctx, localRefs, remoteRefs, r.s, o.Depth)
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
	}

	var updatedPrune bool
	if o.Prune {
		updatedPrune, err = r.pruneRemotes(ctx, o.RefSpecs, localRefs, remoteRefs)
		if err != nil {
			return nil, err
		}
	}

	updated, err := r.updateLocalReferenceStorage(ctx, o.RefSpecs, refs, remoteRefs, specToRefs, o.Tags, o.Force)
	if err != nil {
		return nil, err
	}

	if !updated {
		updated, err = depthChanged(ctx, shallows, r.s)
		if err != nil {
			return nil, fmt.Errorf("error checking depth change: %v", err)
		}
	}

	if !updated && !updatedPrune {
		// No references updated, but may have fetched new objects, check if we now have any of our wants
		for _, hash := range wants {
			exists, _ := objectExists(ctx, r.s, hash)
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

func referenceStorageFromRefs(ctx context.Context, refs []*plumbing.Reference, filterPeeled bool) memory.ReferenceStorage {
	refStore := memory.ReferenceStorage{}
	for _, ref := range refs {
		if filterPeeled && strings.HasSuffix(ref.Name().String(), peeledSuffix) {
			continue
		}
		_ = refStore.SetReference(ctx, ref)
	}
	return refStore
}

func depthChanged(ctx context.Context, before []plumbing.Hash, s storage.Storer) (bool, error) {
	after, err := s.Shallow(ctx)
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

func newClient(rawURL string, opts []client.Option) (*client.Client, *transport.Request, error) {
	u, err := transport.ParseURL(rawURL)
	if err != nil {
		return nil, nil, err
	}

	cl := client.New(opts...)
	return cl, &transport.Request{URL: u}, nil
}

// transportProtocol returns the wire protocol version configured for this
// remote's repository (the protocol.version setting), defaulting to
// config.DefaultProtocolVersion. It is used for ref discovery and fetch;
// push always uses v0/v1, since protocol v2 has no push.
func (r *Remote) transportProtocol(ctx context.Context) protocol.Version {
	if r.s == nil {
		return config.DefaultProtocolVersion
	}
	cfg, err := r.s.Config(ctx)
	if err != nil || cfg == nil {
		return config.DefaultProtocolVersion
	}
	return cfg.Protocol.Version
}

func (r *Remote) pruneRemotes(ctx context.Context, specs []config.RefSpec, localRefs []*plumbing.Reference, remoteRefs storer.ReferenceStorer) (bool, error) {
	var updatedPrune bool
	for _, spec := range specs {
		rev := spec.Reverse()
		for _, ref := range localRefs {
			if !rev.Match(ref.Name()) {
				continue
			}
			_, err := remoteRefs.Reference(ctx, rev.Dst(ref.Name()))
			if errors.Is(err, plumbing.ErrReferenceNotFound) {
				updatedPrune = true
				err := r.s.RemoveReference(ctx, ref.Name())
				if err != nil {
					return false, err
				}
			}
		}
	}
	return updatedPrune, nil
}

func (r *Remote) addReferencesToUpdate(
	ctx context.Context,
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
			if err := r.deleteReferences(ctx, rs, remoteRefs, refsDict, cmds, false); err != nil {
				return err
			}
		} else {
			err := r.addOrUpdateReferences(ctx, rs, localRefs, refsDict, remoteRefs, cmds, forceWithLease)
			if err != nil {
				return err
			}

			if prune {
				if err := r.deleteReferences(ctx, rs, remoteRefs, refsDict, cmds, true); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *Remote) addOrUpdateReferences(
	ctx context.Context,
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
			object, err := object.GetObject(ctx, r.s, plumbing.NewHash(rs.Src()))
			if err == nil {
				return r.addObject(ctx, rs, remoteRefs, object.ID(), cmds)
			}
			return nil
		}

		return r.addReferenceIfRefSpecMatches(ctx, rs, remoteRefs, ref, cmds, forceWithLease)
	}

	for _, ref := range localRefs {
		err := r.addReferenceIfRefSpecMatches(ctx, rs, remoteRefs, ref, cmds, forceWithLease)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Remote) deleteReferences(ctx context.Context, rs config.RefSpec,
	remoteRefs storer.ReferenceStorer,
	refsDict map[string]*plumbing.Reference,
	cmds *[]*packp.Command,
	prune bool,
) error {
	iter, err := remoteRefs.IterReferences(ctx)
	if err != nil {
		return err
	}

	return iter.ForEach(ctx, func(ref *plumbing.Reference) error {
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

func (r *Remote) addObject(ctx context.Context, rs config.RefSpec,
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
	remoteRef, err := remoteRefs.Reference(ctx, cmd.Name)
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
		if err := checkFastForwardUpdate(ctx, r.s, remoteRefs, cmd); err != nil {
			return err
		}
	}

	*cmds = append(*cmds, cmd)
	return nil
}

func (r *Remote) addReferenceIfRefSpecMatches(ctx context.Context, rs config.RefSpec,
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

	remoteRef, err := remoteRefs.Reference(ctx, cmd.Name)
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
		if err = r.checkForceWithLease(ctx, localRef, cmd, forceWithLease); err != nil {
			return err
		}
	} else if !rs.IsForceUpdate() {
		if err := checkTagUpdate(cmd); err != nil {
			return err
		}
		if err := checkFastForwardUpdate(ctx, r.s, remoteRefs, cmd); err != nil {
			return err
		}
	}

	*cmds = append(*cmds, cmd)
	return nil
}

func (r *Remote) checkForceWithLease(ctx context.Context, localRef *plumbing.Reference, cmd *packp.Command, forceWithLease *ForceWithLease) error {
	remotePrefix := fmt.Sprintf("refs/remotes/%s/", r.Config().Name)

	ref, err := storer.ResolveReference(
		ctx,
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

func checkTagUpdate(cmd *packp.Command) error {
	if cmd.Name.IsTag() && cmd.Old != plumbing.ZeroHash {
		return fmt.Errorf("tag already exists: %s", cmd.Name.String())
	}

	return nil
}

func getRemoteRefsFromStorer(ctx context.Context, remoteRefStorer storer.ReferenceStorer) (
	map[plumbing.Hash]bool, error,
) {
	remoteRefs := map[plumbing.Hash]bool{}
	iter, err := remoteRefStorer.IterReferences(ctx)
	if err != nil {
		return nil, err
	}
	err = iter.ForEach(ctx, func(ref *plumbing.Reference) error {
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
	ctx context.Context,
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

	commit, err := object.GetCommit(ctx, s, h)
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
	_ = walker.ForEach(ctx, func(c *object.Commit) error {
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
	ctx context.Context,
	localRefs []*plumbing.Reference,
	remoteRefStorer storer.ReferenceStorer,
	s storage.Storer,
	depth int,
) ([]plumbing.Hash, error) {
	haves := map[plumbing.Hash]bool{}

	// Build a map of all the remote references, to avoid loading too
	// many parent commits for references we know don't need to be
	// transferred.
	remoteRefs, err := getRemoteRefsFromStorer(ctx, remoteRefStorer)
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

		err = getHavesFromRef(ctx, ref, remoteRefs, s, haves, depth)
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
	ctx context.Context,
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
		specToRefs[i], err = doCalculateRefs(ctx, spec[i], remoteRefs, refs)
		if err != nil {
			return nil, nil, err
		}
	}

	return refs, specToRefs, nil
}

func doCalculateRefs(
	ctx context.Context,
	s config.RefSpec,
	remoteRefs storer.ReferenceStorer,
	refs memory.ReferenceStorage,
) ([]*plumbing.Reference, error) {
	var refList []*plumbing.Reference

	if s.IsExactSHA1() {
		ref := plumbing.NewHashReference(s.Dst(""), plumbing.NewHash(s.Src()))

		refList = append(refList, ref)
		return refList, refs.SetReference(ctx, ref)
	}

	var matched bool
	onMatched := func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.SymbolicReference {
			target, err := storer.ResolveReference(ctx, remoteRefs, ref.Name())
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
		return refs.SetReference(ctx, ref)
	}

	var ret error
	if s.IsWildcard() {
		iter, err := remoteRefs.IterReferences(ctx)
		if err != nil {
			return nil, err
		}
		ret = iter.ForEach(ctx, func(ref *plumbing.Reference) error {
			if !s.Match(ref.Name()) {
				return nil
			}

			return onMatched(ref)
		})
	} else {
		var resolvedRef *plumbing.Reference
		src := s.Src()
		resolvedRef, ret = repository.ExpandRef(ctx, remoteRefs, plumbing.ReferenceName(src))
		if ret == nil {
			ret = onMatched(resolvedRef)
		}
	}

	if !matched && !s.IsWildcard() {
		return nil, fmt.Errorf("%w: %s", ErrRemoteRefNotFound, s.Src())
	}

	return refList, ret
}

func getWants(ctx context.Context, localStorer storage.Storer, refs memory.ReferenceStorage, depth int) ([]plumbing.Hash, error) {
	// If depth is anything other than 1 and the repo has shallow commits then just because we have the commit
	// at the reference doesn't mean that we don't still need to fetch the parents
	shallow := false
	if depth != 1 {
		if s, _ := localStorer.Shallow(ctx); len(s) > 0 {
			shallow = true
		}
	}

	wants := map[plumbing.Hash]bool{}
	for _, ref := range refs {
		hash := ref.Hash()
		exists, err := objectExists(ctx, localStorer, ref.Hash())
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

func objectExists(ctx context.Context, s storer.EncodedObjectStorer, h plumbing.Hash) (bool, error) {
	_, err := s.EncodedObject(ctx, plumbing.AnyObject, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return false, nil
	}

	return true, err
}

func checkFastForwardUpdate(ctx context.Context, s storer.EncodedObjectStorer, remoteRefs storer.ReferenceStorer, cmd *packp.Command) error {
	if cmd.Old == plumbing.ZeroHash {
		_, err := remoteRefs.Reference(ctx, cmd.Name)
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
		shallows, _ = ss.Shallow(ctx)
	}

	ff, err := isFastForward(ctx, s, cmd.Old, cmd.New, shallows)
	if err != nil {
		return err
	}

	if !ff {
		return fmt.Errorf("non-fast-forward update: %s", cmd.Name.String())
	}

	return nil
}

// isFastForward reports whether newHash is a descendant of old in the commit
// graph stored in s. shallows is the list of commits that act as boundary
// nodes for a shallow clone; commits reachable only through those boundaries
// are not locally available.
//
// When shallows are present and the ancestry of newHash cannot be fully
// traced back to old using only local commits, we conservatively return
// true (assume fast-forward) to avoid a false negative caused by the
// shallow boundary. This mirrors git(1)'s behavior for shallow fetches:
// ancestry checks are relaxed once history is truncated, at the cost of
// not being able to prove fast-forward strictly from local data.
func isFastForward(ctx context.Context, s storer.EncodedObjectStorer, old, newHash plumbing.Hash, shallows []plumbing.Hash) (bool, error) {
	c, err := object.GetCommit(ctx, s, newHash)
	if err != nil {
		return false, err
	}

	// Build a set of shallow commits so we can detect when the walk actually
	// reaches a shallow boundary (as opposed to merely knowing shallows exist).
	shallowsSet := make(map[plumbing.Hash]struct{}, len(shallows))
	for _, sh := range shallows {
		shallowsSet[sh] = struct{}{}
	}

	// For each known shallow commit, mark its parent hashes as boundaries so
	// the walker never tries to load commits that are not stored locally.
	parentsToIgnore := make([]plumbing.Hash, 0, len(shallows))
	for _, sh := range shallows {
		shallowCommit, err := object.GetCommit(ctx, s, sh)
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				// Shallow marker may reference a commit we no longer have; skip.
				continue
			}
			return false, err
		}
		parentsToIgnore = append(parentsToIgnore, shallowCommit.ParentHashes...)
	}

	found := false
	boundedByShallow := false
	iter := object.NewCommitPreorderIter(c, nil, parentsToIgnore)
	err = iter.ForEach(ctx, func(c *object.Commit) error {
		if _, isShallow := shallowsSet[c.Hash]; isShallow {
			// The walk reached a shallow commit; history is truncated here.
			boundedByShallow = true
		}
		if c.Hash != old {
			return nil
		}

		found = true
		return storer.ErrStop
	})
	if err != nil {
		return false, err
	}
	if !found && boundedByShallow {
		// The walk was bounded by shallow markers and could not reach `old`.
		// We cannot disprove fast-forward from local data alone, so allow the
		// update. This matches the behaviour of git(1) for shallow fetches.
		return true, nil
	}
	return found, nil
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
	ctx context.Context,
	specs []config.RefSpec,
	fetchedRefs, remoteRefs memory.ReferenceStorage,
	specToRefs [][]*plumbing.Reference,
	tagMode plumbing.TagMode,
	force bool,
) (updated bool, err error) {
	isWildcard := true
	forceNeeded := false

	shallows, _ := r.s.Shallow(ctx)

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
			if !strings.HasPrefix(localName.String(), "refs/") {
				if plumbing.IsHash(localName.String()) {
					// Bare-hash dst: the intent is to fetch the object only;
					// no local reference should be created.
					continue
				}
				localName = plumbing.NewBranchReferenceName(localName.String())
			}
			old, _ := storer.ResolveReference(ctx, r.s, localName)
			newRef := plumbing.NewHashReference(localName, ref.Hash())

			if old != nil && localName.IsTag() && old.Hash() != newRef.Hash() && !force && !spec.IsForceUpdate() {
				forceNeeded = true
				continue
			}

			// If the ref exists locally as a non-tag and force is not
			// specified, only update if the new ref is an ancestor of the old
			if old != nil && !old.Name().IsTag() && !force && !spec.IsForceUpdate() {
				ff, err := isFastForward(ctx, r.s, old.Hash(), newRef.Hash(), shallows)
				if err != nil {
					return updated, err
				}

				if !ff {
					forceNeeded = true
					continue
				}
			}

			refUpdated, err := checkAndUpdateReferenceStorerIfNeeded(ctx, r.s, newRef, old)
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
	tagUpdated, tagForceNeeded, err := r.buildFetchedTags(ctx, tags, tagMode == plumbing.AllTags, force)
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

func (r *Remote) buildFetchedTags(ctx context.Context, refs memory.ReferenceStorage, allTags, force bool) (updated, forceNeeded bool, err error) {
	for _, ref := range refs {
		if !ref.Name().IsTag() {
			continue
		}

		_, err := r.s.EncodedObject(ctx, plumbing.AnyObject, ref.Hash())
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			continue
		}

		if err != nil {
			return updated, forceNeeded, err
		}

		old, err := r.s.Reference(ctx, ref.Name())
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

		refUpdated, err := updateReferenceStorerIfNeeded(ctx, r.s, ref)
		if err != nil {
			return updated, forceNeeded, err
		}

		if refUpdated {
			updated = true
		}
	}

	return updated, forceNeeded, err
}

// List lists the references on the remote repository.
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned.
// If ListOptions.Timeout is set, the context is additionally bounded by it.
func (r *Remote) List(ctx context.Context, o *ListOptions) (rfs []*plumbing.Reference, err error) {
	timeout := o.Timeout
	if timeout < 0 {
		return nil, fmt.Errorf("invalid timeout: %d", timeout)
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}
	return r.list(ctx, o)
}

func (r *Remote) list(ctx context.Context, o *ListOptions) (rfs []*plumbing.Reference, err error) {
	if r.c == nil || len(r.c.URLs) == 0 {
		return nil, ErrEmptyUrls
	}

	cl, req, err := newClient(r.c.URLs[0], o.ClientOptions)
	if err != nil {
		return nil, err
	}

	req.Command = transport.UploadPackService
	req.Protocol = r.transportProtocol(ctx)
	sess, err := cl.Handshake(ctx, req)
	if err != nil {
		return nil, err
	}

	defer ioutil.CheckClose(sess, &err)

	allRefs, err := sess.GetRemoteRefs(ctx, nil)
	if err != nil {
		return nil, err
	}

	var resultRefs []*plumbing.Reference
	for _, ref := range allRefs.References {
		isPeeled := strings.HasSuffix(ref.Name().String(), peeledSuffix)
		switch o.PeelingOption {
		case IgnorePeeled:
			if !isPeeled {
				resultRefs = append(resultRefs, ref)
			}
		case OnlyPeeled:
			if isPeeled {
				resultRefs = append(resultRefs, ref)
			}
		case AppendPeeled:
			resultRefs = append(resultRefs, ref)
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

func referencesToHashes(ctx context.Context, refs storer.ReferenceStorer) ([]plumbing.Hash, error) {
	iter, err := refs.IterReferences(ctx)
	if err != nil {
		return nil, err
	}

	var hs []plumbing.Hash
	err = iter.ForEach(ctx, func(ref *plumbing.Reference) error {
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

	config, err := s.Config(ctx)
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
			e := packfile.NewEncoder(ctx, wr, s, useRefDeltas)
			if _, err := e.Encode(ctx, hs, config.Pack.Window); err != nil {
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

func (r *Remote) checkRequireRemoteRefs(ctx context.Context, requires []config.RefSpec, remoteRefs storer.ReferenceStorer) error {
	for _, require := range requires {
		if require.IsWildcard() {
			return fmt.Errorf("wildcards not supported in RequireRemoteRefs, got %s", require.String())
		}

		name := require.Dst("")
		remote, err := remoteRefs.Reference(ctx, name)
		if err != nil {
			return fmt.Errorf("remote ref %s required to be %s but is absent", name.String(), require.Src())
		}

		var requireHash string
		if require.IsExactSHA1() {
			requireHash = require.Src()
		} else {
			target, err := storer.ResolveReference(ctx, remoteRefs, plumbing.ReferenceName(require.Src()))
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
