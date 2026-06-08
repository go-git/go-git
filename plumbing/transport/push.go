package transport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// SendPack sends a packfile to a remote server.
func SendPack(
	ctx context.Context,
	_ storage.Storer,
	caps capability.List,
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

	if req.Packfile != nil {
		if _, err := ioutil.CopyBufferPool(writer, req.Packfile); err != nil {
			return err
		}
		if err := req.Packfile.Close(); err != nil {
			return fmt.Errorf("closing packfile: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return err
	}

	var reportStatus int
	if upreq.Capabilities.Supports(capability.ReportStatusV2) {
		reportStatus = 2
	} else if upreq.Capabilities.Supports(capability.ReportStatus) {
		reportStatus = 1
	}

	if reportStatus == 0 {
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
				d.Progress = req.Progress
			}
			r = d
		}
	}

	report := &packp.ReportStatus{}
	if err := report.Decode(r); err != nil {
		return fmt.Errorf("decode report-status: %w", err)
	}

	reportError := report.Error()

	if reportStatus > 0 && len(upreq.Commands) > 0 {
		_, err := io.ReadAll(r)
		if err != nil && !errors.Is(err, io.EOF) {
			_ = reader.Close()
			if reportError != nil {
				return reportError
			}
			return fmt.Errorf("reading progress messages: %w", err)
		}
	}

	if err := reader.Close(); err != nil {
		if reportError != nil {
			return reportError
		}
		return fmt.Errorf("closing reader: %w", err)
	}

	return reportError
}

func buildUpdateRequests(caps capability.List, req *PushRequest) *packp.UpdateRequests {
	upreq := &packp.UpdateRequests{}

	if caps.Supports(capability.ReportStatus) {
		upreq.Capabilities.Set(capability.ReportStatus)
	}
	if req.Progress != nil {
		if caps.Supports(capability.Sideband64k) {
			upreq.Capabilities.Set(capability.Sideband64k)
		} else if caps.Supports(capability.Sideband) {
			upreq.Capabilities.Set(capability.Sideband)
		}
		if req.Quiet && caps.Supports(capability.Quiet) {
			upreq.Capabilities.Set(capability.Quiet)
		}
	}
	if req.Atomic && caps.Supports(capability.Atomic) {
		upreq.Capabilities.Set(capability.Atomic)
	}
	if len(req.Options) > 0 && caps.Supports(capability.PushOptions) {
		upreq.Capabilities.Set(capability.PushOptions)
	}
	if caps.Supports(capability.Agent) {
		upreq.Capabilities.Set(capability.Agent, capability.DefaultAgent())
	}

	upreq.Commands = req.Commands
	return upreq
}
