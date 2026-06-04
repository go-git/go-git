package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ArchiveRequest describes a git-upload-archive request.
type ArchiveRequest struct {
	// Args is the list of arguments sent as "argument <arg>\n" pkt-lines.
	// These are the same arguments accepted by git-archive:
	// e.g. []string{"--format=tar.gz", "--prefix=project/", "HEAD", "src/"}
	Args []string

	// Progress receives human-readable status from the server (sideband channel 2).
	Progress sideband.Progress
}

// Archiver is implemented by Sessions that support git-upload-archive.
// Callers should type-assert their Session to Archiver at the call
// site, following the io.WriterTo / io.ReaderFrom pattern.
type Archiver interface {
	Archive(ctx context.Context, req *ArchiveRequest) (io.ReadCloser, error)
}

// Archive speaks the git-upload-archive client wire protocol.
//
// It sends argument pkt-lines to w, closes w, then reads the ACK/NACK
// response and sideband-encoded archive stream from r. The returned
// io.ReadCloser yields archive data (sideband channel 1); closing it
// closes r.
//
// Wire protocol:
//
//	Client → Server: "argument <arg>\n" pkt-lines + flush
//	Server → Client: "ACK\n" pkt-line + flush
//	Server → Client: sideband packets (band 1 = data, band 2 = progress)
func Archive(ctx context.Context, w io.WriteCloser, r io.ReadCloser, req *ArchiveRequest) (io.ReadCloser, error) {
	w = ioutil.NewContextWriteCloser(ctx, w)

	for _, arg := range req.Args {
		if _, err := pktline.WriteString(w, fmt.Sprintf("argument %s\n", arg)); err != nil {
			return nil, fmt.Errorf("archive: writing argument: %w", err)
		}
	}
	if err := pktline.WriteFlush(w); err != nil {
		return nil, fmt.Errorf("archive: writing flush: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("archive: closing writer: %w", err)
	}

	rd := bufio.NewReader(r)

	l, line, err := pktline.ReadLine(rd)
	if err != nil {
		return nil, fmt.Errorf("archive: reading ACK/NACK: %w", err)
	}
	if l == pktline.Flush {
		return nil, fmt.Errorf("archive: expected ACK/NACK, got flush")
	}

	resp := strings.TrimSuffix(string(line), "\n")
	switch {
	case resp == "ACK":
	case strings.HasPrefix(resp, "NACK "):
		return nil, fmt.Errorf("archive: NACK %s", resp[5:])
	default:
		return nil, fmt.Errorf("archive: protocol error: %s", resp)
	}

	l, _, err = pktline.ReadLine(rd)
	if err != nil {
		return nil, fmt.Errorf("archive: reading flush after ACK: %w", err)
	}
	if l != pktline.Flush {
		return nil, fmt.Errorf("archive: expected flush after ACK, got data")
	}

	demuxer := sideband.NewDemuxer(sideband.Sideband64k, rd)
	if req.Progress != nil {
		demuxer.Progress = req.Progress
	}

	return ioutil.NewReadCloser(demuxer, r), nil
}
