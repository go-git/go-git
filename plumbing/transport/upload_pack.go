package transport

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// UploadPackOptions is a set of options for the UploadPack service.
type UploadPackOptions struct {
	GitProtocol   string
	AdvertiseRefs bool
	StatelessRPC  bool
}

// UploadPack is a server command that serves the upload-pack service.
func UploadPack(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
	opts *UploadPackOptions,
) error {
	if w == nil {
		return fmt.Errorf("nil writer")
	}

	w = ioutil.NewContextWriteCloser(ctx, w)

	if opts == nil {
		opts = &UploadPackOptions{}
	}

	version := ProtocolVersion(opts.GitProtocol)

	if opts.AdvertiseRefs || !opts.StatelessRPC {
		switch version {
		case protocol.V2:
			if err := AdvertiseCapabilitiesV2(ctx, st, w, UploadPackService, opts.StatelessRPC); err != nil {
				return fmt.Errorf("advertising V2 capabilities: %w", err)
			}
		case protocol.V1:
			if _, err := pktline.Writef(w, "version %d\n", version); err != nil {
				return err
			}
			fallthrough
		case protocol.V0:
			if err := AdvertiseReferences(ctx, st, w, UploadPackService, opts.StatelessRPC); err != nil {
				return fmt.Errorf("advertising references: %w", err)
			}
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedVersion, version)
		}
	}

	if opts.AdvertiseRefs {
		// Done, there's nothing else to do
		return nil
	}

	// V2: enter command dispatch loop.
	if version == protocol.V2 {
		return uploadPackV2(ctx, st, r, w)
	}

	if r == nil {
		return fmt.Errorf("nil reader")
	}

	r = ioutil.NewContextReadCloser(ctx, r)

	rd := bufio.NewReader(r)
	l, _, err := pktline.PeekLine(rd)
	if err != nil {
		return fmt.Errorf("peeking line: %w", err)
	}

	// In case the client has nothing to send, it sends a flush packet to
	// indicate that it is done sending data. In that case, we're done
	// here.
	if l == pktline.Flush {
		return nil
	}

	var done bool
	var haves []plumbing.Hash
	var upreq *packp.UploadRequest
	var havesWithRef map[plumbing.Hash][]plumbing.Hash
	var multiAck, multiAckDetailed bool
	var caps *capability.List
	var wants []plumbing.Hash
	firstRound := true
	for !done {
		writec := make(chan error)
		if firstRound || opts.StatelessRPC {
			upreq = packp.NewUploadRequest()
			if err := upreq.Decode(rd); err != nil {
				return fmt.Errorf("decoding upload-request: %w", err)
			}

			wants = upreq.Wants
			caps = upreq.Capabilities

			if err := r.Close(); err != nil {
				return fmt.Errorf("closing reader: %w", err)
			}

			// Find common commits/objects
			havesWithRef, err = revlist.ObjectsWithRef(st, wants, nil)
			if err != nil {
				return fmt.Errorf("getting objects with ref: %w", err)
			}

			// Encode objects to packfile and write to client
			multiAck = caps.Supports(capability.MultiACK)
			multiAckDetailed = caps.Supports(capability.MultiACKDetailed)

			go func() {
				// TODO: support deepen-since, and deepen-not
				var shupd packp.ShallowUpdate
				if !upreq.Depth.IsZero() {
					switch depth := upreq.Depth.(type) {
					case packp.DepthCommits:
						if err := getShallowCommits(st, wants, int(depth), &shupd); err != nil {
							writec <- fmt.Errorf("getting shallow commits: %w", err)
							return
						}
					default:
						writec <- fmt.Errorf("unsupported depth type %T", upreq.Depth)
						return
					}

					if err := shupd.Encode(w); err != nil {
						writec <- fmt.Errorf("sending shallow-update: %w", err)
						return
					}
				}

				writec <- nil
			}()
		}

		if err := <-writec; err != nil {
			return err
		}

		var uphav packp.UploadHaves
		if err := uphav.Decode(rd); err != nil {
			return fmt.Errorf("decoding upload-haves: %w", err)
		}

		if err := r.Close(); err != nil {
			return fmt.Errorf("closing reader: %w", err)
		}

		haves = append(haves, uphav.Haves...)
		done = uphav.Done

		common := map[plumbing.Hash]struct{}{}
		var ack packp.ACK
		var acks []packp.ACK
		for _, hu := range uphav.Haves {
			refs, ok := havesWithRef[hu]
			if ok {
				for _, ref := range refs {
					common[ref] = struct{}{}
				}
			}

			var status packp.ACKStatus
			if multiAckDetailed {
				status = packp.ACKCommon
				if !ok {
					status = packp.ACKReady
				}
			} else if multiAck {
				status = packp.ACKContinue
			}

			if ok || multiAck || multiAckDetailed {
				ack = packp.ACK{Hash: hu, Status: status}
				acks = append(acks, ack)
				if !multiAck && !multiAckDetailed {
					break
				}
			}
		}

		go func() {
			defer close(writec)

			if len(haves) > 0 {
				// Encode ACKs to client when we have haves
				srvrsp := packp.ServerResponse{ACKs: acks}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending acks server-response: %w", err)
					return
				}
			}

			switch {
			case !done:
				if multiAck || multiAckDetailed {
					// Encode a NAK for multi-ack
					srvrsp := packp.ServerResponse{}
					if err := srvrsp.Encode(w); err != nil {
						writec <- fmt.Errorf("sending nak server-response: %w", err)
						return
					}
				}
			case !ack.Hash.IsZero() && (multiAck || multiAckDetailed):
				// We're done, send the final ACK
				ack.Status = 0
				srvrsp := packp.ServerResponse{ACKs: []packp.ACK{ack}}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending final ack server-response: %w", err)
					return
				}
			case ack.Hash.IsZero():
				// We don't have multi-ack and there are no haves. Encode a NAK.
				srvrsp := packp.ServerResponse{}
				if err := srvrsp.Encode(w); err != nil {
					writec <- fmt.Errorf("sending final nak server-response: %w", err)
					return
				}
			}

			writec <- nil
		}()

		if err := <-writec; err != nil {
			return err
		}

		firstRound = false
	}

	// Done with the request, now close the reader
	// to indicate that we are done reading from it.
	if err := r.Close(); err != nil {
		return fmt.Errorf("closing reader: %w", err)
	}

	objs, err := objectsToUpload(st, wants, haves)
	if err != nil {
		_ = w.Close()
		return fmt.Errorf("getting objects to upload: %w", err)
	}

	var (
		useSideband bool
		writer      io.Writer = w
	)
	if caps.Supports(capability.Sideband64k) {
		writer = sideband.NewMuxer(sideband.Sideband64k, w)
		useSideband = true
	} else if caps.Supports(capability.Sideband) {
		writer = sideband.NewMuxer(sideband.Sideband, w)
		useSideband = true
	}

	// TODO: Support shallow-file
	// TODO: Support thin-pack
	e := packfile.NewEncoder(writer, st, false)
	_, err = e.Encode(objs, 10)
	if err != nil {
		return fmt.Errorf("encoding packfile: %w", err)
	}

	if useSideband {
		if err := pktline.WriteFlush(w); err != nil {
			return fmt.Errorf("flushing sideband: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("closing writer: %w", err)
	}

	return nil
}

func objectsToUpload(st storage.Storer, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	return revlist.Objects(st, wants, haves)
}

func getShallowCommits(st storage.Storer, heads []plumbing.Hash, depth int, upd *packp.ShallowUpdate) error {
	var i, curDepth int
	var commit *object.Commit
	depths := map[*object.Commit]int{}
	stack := []object.Object{}

	for commit != nil || i < len(heads) || len(stack) > 0 {
		if commit == nil {
			if i < len(heads) {
				obj, err := st.EncodedObject(plumbing.CommitObject, heads[i])
				i++
				if err != nil {
					continue
				}

				commit, err = object.DecodeCommit(st, obj)
				if err != nil {
					commit = nil
					continue
				}

				depths[commit] = 0
				curDepth = 0
			} else if len(stack) > 0 {
				commit = stack[len(stack)-1].(*object.Commit)
				stack = stack[:len(stack)-1]
				curDepth = depths[commit]
			}
		}

		curDepth++

		if depth != math.MaxInt && curDepth >= depth {
			upd.Shallows = append(upd.Shallows, commit.Hash)
			commit = nil
			continue
		}

		upd.Unshallows = append(upd.Unshallows, commit.Hash)

		parents := commit.Parents()
		commit = nil
		for {
			parent, err := parents.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			if depths[parent] != 0 && curDepth >= depths[parent] {
				continue
			}

			depths[parent] = curDepth

			if _, err := parents.Next(); err == nil {
				stack = append(stack, parent)
			} else {
				commit = parent
				curDepth = depths[commit]
			}
		}
	}

	return nil
}

// uploadPackV2 handles the V2 command dispatch loop for upload-pack.
// After the capability advertisement, the client sends one command per
// request. For stateless (HTTP) connections each command is a separate
// request; for stateful connections the commands arrive on the same stream.
func uploadPackV2(
	ctx context.Context,
	st storage.Storer,
	r io.ReadCloser,
	w io.WriteCloser,
) error {
	if r == nil {
		return fmt.Errorf("nil reader")
	}

	r = ioutil.NewContextReadCloser(ctx, r)
	rd := bufio.NewReader(r)

	for {
		l, line, err := pktline.PeekLine(rd)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading V2 command: %w", err)
		}

		// Flush means the client is done.
		if l == pktline.Flush {
			return nil
		}

		text := string(bytes.TrimSuffix(line, []byte("\n")))

		// Consume the peeked line.
		_, _, _ = pktline.ReadLine(rd)

		switch {
		case text == "command=ls-refs":
			if err := HandleLsRefs(ctx, st, rd, w); err != nil {
				return fmt.Errorf("ls-refs: %w", err)
			}
		case text == "command=fetch":
			if err := handleV2Fetch(ctx, st, rd, w); err != nil {
				return fmt.Errorf("fetch: %w", err)
			}
		default:
			// Skip unknown command arguments until flush.
			for {
				sl, _, serr := pktline.ReadLine(rd)
				if serr != nil || sl == pktline.Flush {
					break
				}
			}
		}
	}
}

// v2FetchArgs holds parsed arguments from a V2 fetch command.
type v2FetchArgs struct {
	wants    []plumbing.Hash
	wantRefs []string
	haves    []plumbing.Hash
	shallows []plumbing.Hash

	done        bool
	waitForDone bool
	includeTag  bool
	sidebandAll bool

	packfileURIProtocols []string

	depth          int
	deepenSince    int64
	deepenNot      string
	deepenRelative bool

	filter packp.Filter
}

// handleV2Fetch handles a single V2 fetch command server-side.
func handleV2Fetch(
	_ context.Context,
	st storage.Storer,
	r io.Reader,
	w io.Writer,
) error {
	args, err := parseV2FetchArgs(r)
	if err != nil {
		return err
	}

	// Upstream: packfile-uris requires sideband-all; clear if not set.
	if len(args.packfileURIProtocols) > 0 && !args.sidebandAll {
		args.packfileURIProtocols = nil
	}

	resp := packp.NewV2FetchResponse()

	// Resolve want-ref names to OIDs.
	for _, refName := range args.wantRefs {
		ref, err := st.Reference(plumbing.ReferenceName(refName))
		if err != nil {
			return fmt.Errorf("resolving want-ref %q: %w", refName, err)
		}
		hash := ref.Hash()
		if ref.Type() == plumbing.SymbolicReference {
			resolved, err := storer.ResolveReference(st, ref.Target())
			if err != nil {
				return fmt.Errorf("resolving want-ref symref %q: %w", refName, err)
			}
			hash = resolved.Hash()
		}
		resp.WantedRefs[refName] = hash
		args.wants = append(args.wants, hash)
	}

	// Handle shallow/deepen.
	if args.depth > 0 {
		var shupd packp.ShallowUpdate
		if err := getShallowCommits(st, args.wants, args.depth, &shupd); err != nil {
			return fmt.Errorf("getting shallow commits: %w", err)
		}
		if len(shupd.Shallows) > 0 || len(shupd.Unshallows) > 0 {
			resp.ShallowUpdate = &shupd
		}
	}

	// Build acknowledgments.
	havesWithRef, err := revlist.ObjectsWithRef(st, args.wants, nil)
	if err != nil {
		return fmt.Errorf("getting objects with ref: %w", err)
	}

	for _, h := range args.haves {
		if _, ok := havesWithRef[h]; ok {
			resp.ACKs = append(resp.ACKs, h)
		}
	}

	// Upstream state machine:
	//   - No wants (and no wait-for-done): DONE (no response)
	//   - Has haves: send acks, if ready → send pack
	//   - Wants only (no haves): skip acks → send pack immediately
	//   - Done set: skip acks → send pack immediately
	seenHaves := len(args.haves) > 0
	if !seenHaves && !args.waitForDone && len(args.wants) == 0 {
		return nil
	}

	if args.done {
		// Client said "done" — skip acks, go straight to pack.
		resp.SkipAcknowledgments = true
		resp.Ready = true
	} else if !seenHaves {
		// Wants only, no haves — skip acks, send pack immediately.
		resp.SkipAcknowledgments = true
		resp.Ready = true
	} else if args.waitForDone {
		// Have haves, wait-for-done: send acks but never "ready".
		resp.Ready = false
	} else {
		// Have haves, check if we can give up (have common ancestors).
		// For simplicity, send "ready" if we have any ACKs.
		resp.Ready = len(resp.ACKs) > 0
	}

	if resp.Ready {
		resp.Packfile = bytes.NewReader(nil)
	}

	if err := resp.Encode(w); err != nil {
		return fmt.Errorf("encoding V2 fetch response: %w", err)
	}

	if !resp.Ready {
		return nil
	}

	// Send the packfile.
	objs, err := objectsToUpload(st, args.wants, args.haves)
	if err != nil {
		return fmt.Errorf("getting objects to upload: %w", err)
	}

	// When include-tag is requested, add tag objects that point to
	// objects we are already sending.
	if args.includeTag {
		tagObjs, err := includeTagObjects(st, objs)
		if err != nil {
			return fmt.Errorf("including tags: %w", err)
		}
		objs = append(objs, tagObjs...)
	}

	// V2 packfile section is always sideband-encoded.
	sbw := sideband.NewMuxer(sideband.Sideband64k, w)

	e := packfile.NewEncoder(sbw, st, false)
	if _, err = e.Encode(objs, 10); err != nil {
		return fmt.Errorf("encoding packfile: %w", err)
	}

	return pktline.WriteFlush(w)
}

// parseV2FetchArgs reads capability lines (until delimiter) and argument
// lines (until flush) from a V2 fetch command request.
func parseV2FetchArgs(r io.Reader) (*v2FetchArgs, error) {
	// Phase 1: skip capability lines until delimiter.
	for {
		l, _, err := pktline.ReadLine(r)
		if err != nil {
			return nil, err
		}
		if l == pktline.Delim {
			break
		}
		if l == pktline.Flush {
			return nil, fmt.Errorf("unexpected flush before arguments")
		}
	}

	// Phase 2: read arguments until flush.
	args := &v2FetchArgs{}
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return nil, err
		}
		if l == pktline.Flush {
			break
		}

		text := string(bytes.TrimSuffix(line, []byte("\n")))

		switch {
		case strings.HasPrefix(text, "want "):
			args.wants = append(args.wants, plumbing.NewHash(strings.TrimPrefix(text, "want ")))
		case strings.HasPrefix(text, "want-ref "):
			args.wantRefs = append(args.wantRefs, strings.TrimPrefix(text, "want-ref "))
		case strings.HasPrefix(text, "have "):
			args.haves = append(args.haves, plumbing.NewHash(strings.TrimPrefix(text, "have ")))
		case strings.HasPrefix(text, "shallow "):
			args.shallows = append(args.shallows, plumbing.NewHash(strings.TrimPrefix(text, "shallow ")))
		case strings.HasPrefix(text, "deepen "):
			n, err := strconv.Atoi(strings.TrimPrefix(text, "deepen "))
			if err == nil {
				args.depth = n
			}
		case strings.HasPrefix(text, "deepen-since "):
			ts, err := strconv.ParseInt(strings.TrimPrefix(text, "deepen-since "), 10, 64)
			if err == nil {
				args.deepenSince = ts
			}
		case strings.HasPrefix(text, "deepen-not "):
			args.deepenNot = strings.TrimPrefix(text, "deepen-not ")
		case text == "deepen-relative":
			args.deepenRelative = true
		case strings.HasPrefix(text, "filter "):
			args.filter = packp.Filter(strings.TrimPrefix(text, "filter "))
		case text == "done":
			args.done = true
		case text == "include-tag":
			args.includeTag = true
		case text == string(capability.WaitForDone):
			args.waitForDone = true
		case text == string(capability.SidebandAll):
			args.sidebandAll = true
		case strings.HasPrefix(text, "packfile-uris "):
			protos := strings.TrimPrefix(text, "packfile-uris ")
			args.packfileURIProtocols = strings.Split(protos, ",")
		// thin-pack, no-progress, ofs-delta are accepted but don't
		// change server behavior currently.
		}
	}

	return args, nil
}

// includeTagObjects finds tag objects that point to any of the given OIDs
// and returns them. This implements the include-tag behavior where the
// server sends annotated tags if their referent is included in the pack.
func includeTagObjects(st storage.Storer, objs []plumbing.Hash) ([]plumbing.Hash, error) {
	objSet := make(map[plumbing.Hash]struct{}, len(objs))
	for _, h := range objs {
		objSet[h] = struct{}{}
	}

	var tags []plumbing.Hash
	iter, err := st.IterReferences()
	if err != nil {
		return nil, err
	}

	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if !ref.Name().IsTag() {
			return nil
		}
		tag, err := object.GetTag(st, ref.Hash())
		if err != nil {
			// Not an annotated tag, skip.
			return nil
		}
		// If the tag's target is in our object set, include the tag object.
		if _, ok := objSet[tag.Target]; ok {
			tags = append(tags, ref.Hash())
		}
		return nil
	})

	return tags, err
}
