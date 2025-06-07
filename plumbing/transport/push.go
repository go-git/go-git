package transport

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
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

	// The reference discovery phase is done nearly the same way as it is in
	// the fetching protocol. Each reference obj-id and name on the server is
	// sent in packet-line format to the client, followed by a flush-pkt. The
	// only real difference is that the capability listing is different - the
	// only possible values are report-status, report-status-v2, delete-refs,
	// ofs-delta, atomic and push-options.
	for _, cap := range []capability.Capability{
		capability.ReportStatus,
		capability.ReportStatusV2,
		capability.DeleteRefs,
		capability.OFSDelta,
		capability.Atomic,
		capability.PushOptions,
	} {
		if caps.Supports(cap) {
			upreq.Capabilities.Set(cap) //nolint:errcheck
		}
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

	var needPackData bool
	for _, cmd := range req.Commands {
		if cmd.New != plumbing.ZeroHash {
			needPackData = true
			break
		}
	}

	if needPackData && req.Packfile == nil {
		return fmt.Errorf("packfile is required for push request with new objects")
	}

	caps := conn.Capabilities()
	upreq := buildUpdateRequests(caps, req)
	usePushOptions := len(req.Options) > 0 && caps.Supports(capability.PushOptions)
	if usePushOptions {
		upreq.Capabilities.Set(capability.PushOptions) //nolint:errcheck
	}

	if err := upreq.Encode(writer); err != nil {
		return err
	}

	if usePushOptions {
		var opts packp.PushOptions
		opts.Options = req.Options
		if err := opts.Encode(writer); err != nil {
			return fmt.Errorf("encoding push-options: %w", err)
		}
	}

	// Send the packfile.
	if req.Packfile != nil {
		if _, err := io.Copy(writer, req.Packfile); err != nil {
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

	if !caps.Supports(capability.ReportStatus) {
		// If we don't have report-status, we're done here.
		return nil
	}

	var r io.Reader = reader
	if req.Progress != nil {
		var d *sideband.Demuxer
		if caps.Supports(capability.Sideband64k) {
			d = sideband.NewDemuxer(sideband.Sideband64k, reader)
		} else if caps.Supports(capability.Sideband) {
			d = sideband.NewDemuxer(sideband.Sideband, reader)
		}
		if d != nil {
			d.Progress = req.Progress
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
