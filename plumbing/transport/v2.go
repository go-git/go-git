package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
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
// over the given CommandRunner. Stream transports pass statelessRPC=false;
// HTTP passes true so wants and accumulated haves are resent each round.
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
	return caps
}

// commandCapabilities returns the capability lines sent with every v2 command
// request (agent and, when the server advertised it, object-format).
func (s *v2Session) commandCapabilities() []string {
	caps := []string{"agent=" + capability.DefaultAgent()}
	if of := s.caps.Get(capability.ObjectFormat); of != "" {
		caps = append(caps, "object-format="+of)
	}
	return caps
}

// GetRemoteRefs runs ls-refs once and caches the result.
func (s *v2Session) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		req := &packp.LsRefsRequest{
			Capabilities: s.commandCapabilities(),
			Symrefs:      true,
			Peel:         true,
			Unborn:       s.caps.SupportsArgument("ls-refs", "unborn"),
		}

		var buf bytes.Buffer
		if err := req.Encode(&buf); err != nil {
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
		s.refs = &lr
	}

	if len(s.refs.References) == 0 {
		return nil, ErrEmptyRemoteRepository
	}
	return s.refs.References, nil
}

// Fetch negotiates and downloads a packfile using the v2 fetch command.
func (s *v2Session) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
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
	fr := &packp.FetchRequestV2{
		Capabilities: s.commandCapabilities(),
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

// negotiate runs the fetch negotiation loop until the server returns a
// packfile, returning the shallow update (if any) and a reader positioned at
// the start of the sideband-multiplexed packfile stream.
func (s *v2Session) negotiate(ctx context.Context, base *packp.FetchRequestV2, req *FetchRequest) (*packp.ShallowUpdate, io.ReadCloser, error) {
	haves := append([]plumbing.Hash(nil), req.Haves...)
	var common []plumbing.Hash
	var shallowInfo *packp.ShallowUpdate
	flushAt := initialFlush

	for {
		fr := *base
		fr.Haves = nil
		if s.statelessRPC {
			fr.Haves = append(fr.Haves, common...)
		}

		batch := flushAt
		for i := 0; i < batch && len(haves) > 0; i++ {
			fr.Haves = append(fr.Haves, haves[0])
			haves = haves[1:]
		}
		fr.Done = len(haves) == 0

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
		common = append(common, fres.Acks...)

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
