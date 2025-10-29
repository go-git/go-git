package transport

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// buildUpdateRequests constructs a new update-requests object for the given
// connection and push request.
func buildUpdateRequests(caps *capability.List, req *PushRequest) *packp.UpdateRequests {
	upreq := packp.NewUpdateRequests()

	// The atomic, report-status, report-status-v2, delete-refs, quiet, and
	// push-cert capabilities are sent and recognized by the receive-pack (push
	// to server) process.
	//
	// The ofs-delta and side-band-64k capabilities are sent and recognized by
	// both upload-pack and receive-pack protocols. The agent and session-id
	// capabilities may optionally be sent in both protocols.
	//
	// All other capabilities are only recognized by the upload-pack (fetch
	// from server) process.
	//
	// In addition to the ones listed above, receive-pack special capabilities
	// include object-format and push-options.
	//
	// However, upstream Git does *not* send all of these capabilities by
	// client side. See
	// https://github.com/git/git/blob/485f5f863615e670fd97ae40af744e14072cfe18/send-pack.c#L589
	// for more details.
	//
	// See https://git-scm.com/docs/gitprotocol-capabilities for more details.
	if caps.Supports(capability.ReportStatus) {
		upreq.Capabilities.Set(capability.ReportStatus) //nolint:errcheck
	}
	if req.Progress != nil {
		if caps.Supports(capability.Sideband64k) {
			upreq.Capabilities.Set(capability.Sideband64k) //nolint:errcheck
		} else if caps.Supports(capability.Sideband) {
			upreq.Capabilities.Set(capability.Sideband) //nolint:errcheck
		}
		if req.Quiet && caps.Supports(capability.Quiet) {
			upreq.Capabilities.Set(capability.Quiet) //nolint:errcheck
		}
	}
	if req.Atomic && caps.Supports(capability.Atomic) {
		upreq.Capabilities.Set(capability.Atomic) //nolint:errcheck
	}
	if len(req.Options) > 0 && caps.Supports(capability.PushOptions) {
		upreq.Capabilities.Set(capability.PushOptions) //nolint:errcheck
	}
	if caps.Supports(capability.Agent) {
		upreq.Capabilities.Set(capability.Agent, capability.DefaultAgent()) //nolint:errcheck
	}

	upreq.Commands = req.Commands

	return upreq
}

// SendPack is a function that sends a packfile to a remote server.
func SendPack(
	ctx context.Context,
	st storage.Storer,
	conn Connection,
	writer io.WriteCloser,
	reader io.ReadCloser,
	req *PushRequest,
) error {
	writer = ioutil.NewContextWriteCloser(ctx, writer)
	reader = ioutil.NewContextReadCloser(ctx, reader)

	var needPackfile bool
	for _, cmd := range req.Commands {
		if cmd.Action() != packp.Delete {
			needPackfile = true
			break
		}
	}

	if !needPackfile && req.Packfile != nil {
		return fmt.Errorf("packfile is not accepted for push request without new objects")
	}
	if needPackfile && req.Packfile == nil {
		return fmt.Errorf("packfile is required for push request with new objects")
	}

	caps := conn.Capabilities()
	upreq := buildUpdateRequests(caps, req)
	if err := upreq.Encode(writer); err != nil {
		return err
	}

	if upreq.Capabilities.Supports(capability.PushOptions) {
		var opts packp.PushOptions
		opts.Options = req.Options
		if err := opts.Encode(writer); err != nil {
			return fmt.Errorf("encoding push-options: %w", err)
		}
	}

	// Send the packfile.
	if req.Packfile != nil {
		if _, err := ioutil.CopyBufferPool(writer, req.Packfile); err != nil {
			return err
		}

		if err := req.Packfile.Close(); err != nil {
			return fmt.Errorf("closing packfile: %w", err)
		}
	}

	// Close the write pipe to signal the end of the request.
	if err := writer.Close(); err != nil {
		return err
	}

	if !upreq.Capabilities.Supports(capability.ReportStatus) {
		// If we don't have report-status, we're done here.
		return nil
	}

	var r io.Reader = reader
	if req.Progress != nil {
		var d *sideband.Demuxer
		if upreq.Capabilities.Supports(capability.Sideband64k) {
			d = sideband.NewDemuxer(sideband.Sideband64k, reader)
		} else if upreq.Capabilities.Supports(capability.Sideband) {
			d = sideband.NewDemuxer(sideband.Sideband, reader)
		}
		if d != nil {
			if !upreq.Capabilities.Supports(capability.Quiet) {
				// If we want quiet mode, we don't report progress messages
				// which means the demuxer won't have a progress writer.
				d.Progress = req.Progress
			}
			r = d
		}
	}

	report := packp.NewReportStatus()
	if err := report.Decode(r); err != nil {
		return fmt.Errorf("decode report-status: %w", err)
	}

	if err := reader.Close(); err != nil {
		return fmt.Errorf("closing reader: %w", err)
	}

	return report.Error()
}
