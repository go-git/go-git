package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

// CommandRunner sends a single fully-encoded protocol v2 command request and
// returns a reader over the server's response. Stream transports write to the
// persistent connection and read the reply back; HTTP performs one POST per
// call (stateless RPC). The returned reader is read until the command's
// terminating flush packet; Close releases any per-call resource (a no-op for
// stream transports, which keep the connection open for the next command).
type CommandRunner interface {
	Run(ctx context.Context, requestBody []byte) (io.ReadCloser, error)
	// Close releases the runner's resources. For stream transports this
	// closes the underlying connection; for HTTP it is a no-op.
	Close() error
}

// v2Session implements Session over Git wire protocol version 2. It supports
// the fetch side only (ls-refs and fetch); push has no v2 equivalent and
// always uses v0/v1.
type v2Session struct {
	runner       CommandRunner
	caps         packp.V2Capabilities
	svc          string
	statelessRPC bool

	refs *packp.LsRefsResponse
}

// NewV2Session creates a Session that speaks Git wire protocol version 2
// over the given CommandRunner. Stream transports pass statelessRPC=false
// and HTTP passes true; the flag only tunes how aggressively the have batch
// grows between rounds (see nextFlush). Wants and acknowledged common commits
// are resent every round regardless, since v2 servers are stateless per
// command even on a persistent connection.
func NewV2Session(runner CommandRunner, caps packp.V2Capabilities, service string, statelessRPC bool) Session {
	return &v2Session{
		runner:       runner,
		caps:         caps,
		svc:          service,
		statelessRPC: statelessRPC,
	}
}

var _ Session = (*v2Session)(nil)

// Capabilities bridges the advertised v2 capabilities into the v0/v1
// capability list callers expect. Only the fields consumers read for fetch
// are populated.
func (s *v2Session) Capabilities() *capability.List {
	caps := &capability.List{}
	if of := s.caps.Get(capability.ObjectFormat); of != "" {
		caps.Set(capability.ObjectFormat, of)
	}
	if agent := s.caps.Get(capability.Agent); agent != "" {
		caps.Set(capability.Agent, agent)
	}
	// The client-side exact-SHA1 gate (ErrExactSHA1NotSupported) only applies
	// to protocol v0/v1, where requesting an unadvertised object by OID requires
	// the server to advertise allow-*-sha1-in-want. In protocol v2, fetch accepts
	// "want <oid>" without that capability gate.
	caps.Set(capability.AllowReachableSHA1InWant)
	caps.Set(capability.AllowTipSHA1InWant)
	return caps
}

// commandCapabilities returns the capability lines sent with every v2 command
// request. The agent capability is only sent when the server advertised it
// (the spec forbids sending it otherwise), and object-format is echoed back
// only after validating that go-git supports the advertised algorithm.
func (s *v2Session) commandCapabilities() ([]string, error) {
	var caps []string

	// gitprotocol-v2: a client MUST NOT send the agent capability unless
	// the server advertised it. Matches upstream's server_supports_v2("agent").
	if s.caps.Supports(capability.Agent) {
		caps = append(caps, "agent="+capability.DefaultAgent())
	}

	// Reject an object-format we cannot speak rather than echoing it back
	// blindly; upstream dies on an unknown/mismatched algorithm.
	if of := s.caps.Get(capability.ObjectFormat); of != "" {
		if _, err := hash.FromObjectFormat(format.ObjectFormat(of)); err != nil {
			return nil, fmt.Errorf("server advertised unsupported object-format %q: %w", of, err)
		}
		caps = append(caps, "object-format="+of)
	}

	return caps, nil
}

// GetRemoteRefs runs ls-refs and returns the advertised references. The full
// (unscoped) advertisement is cached so repeated discovery is free; a
// prefix-scoped request always issues a fresh ls-refs so a narrowed result is
// never served to an unscoped caller, or vice versa.
func (s *v2Session) GetRemoteRefs(ctx context.Context, req *RefsRequest) ([]*plumbing.Reference, error) {
	var prefixes []string
	if req != nil {
		prefixes = req.Prefixes
	}

	refs := s.refs
	if len(prefixes) > 0 || refs == nil {
		caps, err := s.commandCapabilities()
		if err != nil {
			return nil, err
		}
		lsreq := &packp.LsRefsRequest{
			Capabilities: caps,
			Symrefs:      true,
			Peel:         true,
			Unborn:       s.caps.SupportsArgument("ls-refs", "unborn"),
			RefPrefixes:  prefixes,
		}

		var buf bytes.Buffer
		if err := lsreq.Encode(&buf); err != nil {
			return nil, err
		}

		resp, err := s.runner.Run(ctx, buf.Bytes())
		if err != nil {
			return nil, err
		}
		defer resp.Close() //nolint:errcheck

		var lr packp.LsRefsResponse
		if err := lr.Decode(resp); err != nil {
			return nil, err
		}
		// ls-refs only reports a symref-target for a symbolic HEAD. When the
		// remote HEAD is detached the server sends a bare hash, so apply the
		// same hash→branch heuristic the v0/v1 advertisement uses, otherwise a
		// clone would record a detached HEAD instead of a symbolic one.
		resolveDetachedHead(lr.References, DefaultBranchRef(req))
		refs = &lr
		if len(prefixes) == 0 {
			s.refs = refs
		}
	}

	if len(refs.References) == 0 {
		return nil, ErrEmptyRemoteRepository
	}
	return refs.References, nil
}

// resolveDetachedHead rewrites a hash-reference HEAD in place to the symbolic
// reference produced by packp.ResolveHeadFromHashHeuristic, so a detached
// remote HEAD still yields a symbolic local HEAD on clone. defaultBranch is the
// branch to prefer (the client's init.defaultBranch), or empty.
func resolveDetachedHead(refs []*plumbing.Reference, defaultBranch plumbing.ReferenceName) {
	for i, r := range refs {
		if r.Name() == plumbing.HEAD && r.Type() == plumbing.HashReference {
			refs[i] = packp.ResolveHeadFromHashHeuristic(r, refs, defaultBranch)
			return
		}
	}
}

// Fetch negotiates and downloads a packfile using the v2 fetch command.
func (s *v2Session) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	if err := s.reconcileObjectFormat(st); err != nil {
		return err
	}

	base, err := s.buildFetchRequest(st, req)
	if err != nil {
		return err
	}

	shallowInfo, packReader, err := s.negotiate(ctx, base, req)
	if err != nil {
		return err
	}
	defer packReader.Close() //nolint:errcheck

	// In protocol v2 the packfile section is always sideband-multiplexed.
	demuxer := sideband.NewDemuxer(sideband.Sideband64k, packReader)
	demuxer.Progress = req.Progress
	reader := ioutil.NewContextReader(ctx, demuxer)

	if err := packfile.UpdateObjectStorage(st, reader); err != nil {
		return err
	}

	if shallowInfo != nil {
		return updateShallow(st, shallowInfo)
	}
	return nil
}

// buildFetchRequest assembles the static parts of the fetch request shared
// across negotiation rounds.
func (s *v2Session) buildFetchRequest(st storage.Storer, req *FetchRequest) (*packp.FetchRequestV2, error) {
	caps, err := s.commandCapabilities()
	if err != nil {
		return nil, err
	}

	fr := &packp.FetchRequestV2{
		Capabilities: caps,
		Wants:        req.Wants,
		OfsDelta:     true,
		NoProgress:   req.Progress == nil,
		IncludeTag:   req.IncludeTags,
	}

	if req.Filter != "" {
		if !s.caps.SupportsArgument("fetch", "filter") {
			return nil, ErrFilterNotSupported
		}
		fr.Filter = req.Filter
	}

	if req.Depth > 0 {
		if !s.caps.SupportsArgument("fetch", "shallow") {
			return nil, ErrShallowNotSupported
		}
		fr.Depth = req.Depth
		shallows, err := st.Shallow()
		if err != nil {
			return nil, err
		}
		fr.Shallows = shallows
	}

	return fr, nil
}

// reconcileObjectFormat aligns the storer's object format with the server's
// advertised one before the packfile is written. On a fresh clone the storer's
// format is unset (HEAD points at refs/heads/.invalid) and the server's sha256
// is adopted; otherwise a mismatch is a hard error, since indexing a sha256
// pack as sha1 (or vice versa) corrupts the store and fails checksum
// validation. Mirrors NegotiatePack's handling for the v0/v1 path.
func (s *v2Session) reconcileObjectFormat(st storage.Storer) error {
	var clientFormat format.ObjectFormat
	if cfg, err := st.Config(); err == nil {
		clientFormat = cfg.Extensions.ObjectFormat
	}

	advertised := s.caps.Get(capability.ObjectFormat)
	if advertised == "" {
		// Server advertised no object-format, so it only speaks sha1. Upstream
		// (fetch-pack.c) errors when the client repo uses a different
		// algorithm rather than letting it fail later on a checksum mismatch.
		if clientFormat != format.UnsetObjectFormat && clientFormat != format.SHA1 {
			return fmt.Errorf("the server does not support algorithm '%s'", clientFormat)
		}
		return nil
	}

	var serverFormat format.ObjectFormat
	switch format.ObjectFormat(advertised) {
	case format.SHA1, format.SHA256:
		serverFormat = format.ObjectFormat(advertised)
	default:
		return nil
	}

	// Adopt the server format on a fresh clone: unset client + sha256 server,
	// with HEAD still at the clone placeholder.
	if clientFormat == format.UnsetObjectFormat && serverFormat == format.SHA256 {
		if ref, err := st.Reference(plumbing.HEAD); err == nil && ref.Target().String() == "refs/heads/.invalid" {
			if setter, ok := st.(xstorage.ObjectFormatSetter); ok {
				if err := setter.SetObjectFormat(serverFormat); err != nil {
					return fmt.Errorf("unable to set object format: %w", err)
				}
				clientFormat = serverFormat
			}
		}
	}

	if clientFormat == format.UnsetObjectFormat {
		clientFormat = format.SHA1
	}

	if serverFormat != clientFormat {
		return fmt.Errorf("mismatched algorithms: client %s; server %s", clientFormat, serverFormat)
	}
	return nil
}

// negotiate runs the fetch negotiation loop until the server returns a
// packfile, returning the shallow update (if any) and a reader positioned at
// the start of the sideband-multiplexed packfile stream.
func (s *v2Session) negotiate(ctx context.Context, base *packp.FetchRequestV2, req *FetchRequest) (*packp.ShallowUpdate, io.ReadCloser, error) {
	haves := append([]plumbing.Hash(nil), req.Haves...)
	var common []plumbing.Hash
	var shallowInfo *packp.ShallowUpdate
	flushAt := initialFlush
	inVein := 0
	var seenAck bool

	for {
		fr := *base

		// Protocol v2 is stateless server-side even on a persistent
		// connection: upload-pack reinitializes its set of haves on every
		// fetch command. The previously acknowledged common commits must be
		// resent each round so the server can find the cut point, regardless
		// of transport. This mirrors upstream's unconditional add_common.
		fr.Haves = append([]plumbing.Hash(nil), common...)

		havesAdded := 0
		for i := 0; i < flushAt && len(haves) > 0; i++ {
			fr.Haves = append(fr.Haves, haves[0])
			haves = haves[1:]
			havesAdded++
		}
		inVein += havesAdded

		// Give up negotiating and ask for the pack once the local haves are
		// exhausted, or once we have an acknowledged base and have sent
		// maxInVein haves without further progress (matches upstream).
		fr.Done = havesAdded == 0 || (seenAck && inVein >= maxInVein)

		var buf bytes.Buffer
		if err := fr.Encode(&buf); err != nil {
			return nil, nil, err
		}

		resp, err := s.runner.Run(ctx, buf.Bytes())
		if err != nil {
			return nil, nil, err
		}

		var fres packp.FetchResponseV2
		if err := fres.Decode(resp); err != nil {
			_ = resp.Close()
			return nil, nil, err
		}

		if len(fres.Shallows) > 0 || len(fres.Unshallows) > 0 {
			shallowInfo = &packp.ShallowUpdate{
				Shallows:   fres.Shallows,
				Unshallows: fres.Unshallows,
			}
		}
		if len(fres.Acks) > 0 {
			inVein = 0
			seenAck = true
			common = append(common, fres.Acks...)
		}

		if fres.HasPackfile {
			return shallowInfo, resp, nil
		}

		_ = resp.Close()

		if fr.Done {
			return nil, nil, fmt.Errorf("server sent no packfile after done: %w", ErrInvalidResponse)
		}

		flushAt = nextFlush(s.statelessRPC, flushAt)
	}
}

// Push is not supported over protocol v2; push always uses v0/v1.
func (s *v2Session) Push(_ context.Context, _ storage.Storer, _ *PushRequest) error {
	return fmt.Errorf("push is not supported over protocol v2")
}

// Close implements Session by closing the runner, which releases the
// underlying connection for stream transports.
func (s *v2Session) Close() error { return s.runner.Close() }
