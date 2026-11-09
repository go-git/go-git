package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
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

// peeledSuffix is the suffix used to build peeled reference names.
const peeledSuffix = "^{}"

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

func referenceStorageFromRefs(refs []*plumbing.Reference, filterPeeled bool) memory.ReferenceStorage {
	refStore := memory.ReferenceStorage{}
	for _, ref := range refs {
		if filterPeeled && strings.HasSuffix(ref.Name().String(), peeledSuffix) {
			continue
		}
		if !usableRemoteRef(ref.Name()) {
			trace.General.Printf("ignoring ref with broken name %q", ref.Name().String())
			continue
		}
		_ = refStore.SetReference(ref)
	}
	return refStore
}

// unstorableRefName reports whether err says the storer refused name for what
// the name is, rather than for anything about the fetch.
//
// A refspec builds a destination name the remote did not choose, so a name the
// storer will not take can arrive even after the advertisement has been
// filtered — "+refs/*:refs/*" onto a remote name go-git holds to a stricter
// rule than check_refname_format, say. Git answers per-reference rather than
// per-fetch, in get_fetch_map:
//
//	error(_("* Ignoring funny ref '%s' locally"), (*rmp)->peer_ref->name);
//
// and finishes with the references that remain, exiting 0.
//
// Every arm of the storer's name gate wraps plumbing.ErrInvalidReferenceName,
// so this recognises all of them rather than the format rule alone. That
// matters because go-git refuses a wider set than Git does — the components an
// HFS+ or NTFS filesystem folds to a dot — and those names reach here having
// passed the advertisement filter, which is a faithful check_refname_format.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/remote.c#L2165-L2176
func unstorableRefName(name, source plumbing.ReferenceName, err error) bool {
	if !errors.Is(err, plumbing.ErrInvalidReferenceName) {
		return false
	}

	trace.General.Printf("ignoring local ref %q from remote ref %q: %q", name.String(), source.String(), err.Error())
	return true
}

// usableRemoteRef reports whether an advertised reference name is one this
// side can do anything with.
//
// A remote is free to advertise a name go-git will not store, and one that
// serves a repository holding a stale lock file or a name some other tool left
// behind will do exactly that. Dropping it here, where the advertisement first
// becomes a list, keeps it out of refspec matching, out of the want list and
// out of the storer, so a single unusable name costs that one reference rather
// than the whole fetch.
//
// This is filter_refs in fetch-pack.c, which checks the same thing in the same
// place and says nothing about it:
//
//	if (starts_with(ref->name, "refs/") &&
//	    check_refname_format(ref->name, 0)) {
//		/* trash or a peeled value; do not even add it to unmatched list */
//		free_one_ref(ref);
//		continue;
//	}
//
// Only names under refs/ are judged, as there: HEAD is advertised and is not
// one. Git also relies on this to drop the null-object-id entries it emits for
// a broken name, which go-git would otherwise ask the remote to send.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/fetch-pack.c#L711-L718
func usableRemoteRef(name plumbing.ReferenceName) bool {
	if !name.IsUnderRefs() {
		return true
	}

	return name.Validate() == nil
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
func (r *Remote) transportProtocol() protocol.Version {
	if r.s == nil {
		return config.DefaultProtocolVersion
	}
	cfg, err := r.s.Config()
	if err != nil || cfg == nil {
		return config.DefaultProtocolVersion
	}
	return cfg.Protocol.Version
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
func isFastForward(s storer.EncodedObjectStorer, old, newHash plumbing.Hash, shallows []plumbing.Hash) (bool, error) {
	c, err := object.GetCommit(s, newHash)
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
		shallowCommit, err := object.GetCommit(s, sh)
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
	err = iter.ForEach(func(c *object.Commit) error {
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

// ListContext lists the references on the remote repository.
// The provided Context must be non-nil. If the context expires before the
// operation is complete, an error is returned. The context only affects to the
// transport operations.
func (r *Remote) ListContext(ctx context.Context, o *ListOptions) (rfs []*plumbing.Reference, err error) {
	return r.list(ctx, o)
}

// List lists the references on the remote repository.
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

	cl, req, err := newClient(r.c.URLs[0], o.ClientOptions)
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
