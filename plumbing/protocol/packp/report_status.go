package packp

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

const (
	ok = "ok"
)

// UnpackStatusErr is the error returned when the report status is not ok.
type UnpackStatusErr struct {
	Status string
}

// Error implements the error interface.
func (e UnpackStatusErr) Error() string {
	return fmt.Sprintf("unpack error: %s", e.Status)
}

// CommandStatusErr is the error returned when the command status is not ok.
type CommandStatusErr struct {
	ReferenceName plumbing.ReferenceName
	Status        string
}

// Error implements the error interface.
func (e CommandStatusErr) Error() string {
	return fmt.Sprintf("command error on %s: %s", e.ReferenceName.String(), e.Status)
}

// ReportStatus is a report status message, as used in the git-receive-pack
// process whenever the 'report-status' capability is negotiated.
// The zero value is safe to use.
type ReportStatus struct {
	UnpackStatus    string
	CommandStatuses []*CommandStatus
}

// Error returns the first error if any.
func (s *ReportStatus) Error() error {
	if s.UnpackStatus != ok {
		return UnpackStatusErr{s.UnpackStatus}
	}

	for _, cs := range s.CommandStatuses {
		if err := cs.Error(); err != nil {
			// XXX: Here, we only return the first error following canonical
			// Git behavior.
			return err
		}
	}

	return nil
}

// Encode writes the report status to a writer.
func (s *ReportStatus) Encode(w io.Writer) error {
	if _, err := pktline.Writef(w, "unpack %s\n", s.UnpackStatus); err != nil {
		return err
	}

	for _, cs := range s.CommandStatuses {
		if err := cs.encode(w); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

// Decode reads from the given reader and decodes a report-status message. It
// does not read more input than what is needed to fill the report status.
func (s *ReportStatus) Decode(r io.Reader) error {
	sc := pktline.NewScanner(r)

	b, err := s.scanFirstLine(sc)
	if err != nil {
		return err
	}

	if err := s.decodeReportStatus(b); err != nil {
		return err
	}

	flushed := false
	for sc.Scan() {
		if sc.Len() == pktline.Flush {
			flushed = true
			break
		}

		if err := s.decodeCommandStatus(sc.Bytes()); err != nil {
			return err
		}
	}

	if !flushed {
		return fmt.Errorf("missing flush: %w", sc.Err())
	}

	return sc.Err()
}

func (s *ReportStatus) scanFirstLine(sc *pktline.Scanner) ([]byte, error) {
	if !sc.Scan() {
		if sc.Err() == nil {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, sc.Err()
	}

	return sc.Bytes(), nil
}

func (s *ReportStatus) decodeReportStatus(b []byte) error {
	if isFlush(b) {
		return fmt.Errorf("premature flush")
	}

	b = bytes.TrimSuffix(b, eol)

	line := string(b)
	fields := strings.SplitN(line, " ", 2)
	if len(fields) != 2 || fields[0] != "unpack" {
		return fmt.Errorf("malformed unpack status: %s", line)
	}

	s.UnpackStatus = fields[1]
	return nil
}

func (s *ReportStatus) decodeCommandStatus(b []byte) error {
	b = bytes.TrimSuffix(b, eol)

	line := string(b)
	fields := strings.SplitN(line, " ", 3)
	status := ok
	if len(fields) == 3 && fields[0] == "ng" {
		status = fields[2]
	} else if len(fields) != 2 || fields[0] != "ok" {
		return fmt.Errorf("malformed command status: %s", line)
	}

	cs := &CommandStatus{
		ReferenceName: plumbing.ReferenceName(fields[1]),
		Status:        status,
	}
	s.CommandStatuses = append(s.CommandStatuses, cs)
	return nil
}

// CommandStatus is the status of a reference in a report status.
// See ReportStatus struct.
type CommandStatus struct {
	ReferenceName plumbing.ReferenceName
	Status        string
}

// Error returns the error, if any.
func (s *CommandStatus) Error() error {
	if s.Status == ok {
		return nil
	}

	return CommandStatusErr{
		ReferenceName: s.ReferenceName,
		Status:        s.Status,
	}
}

func (s *CommandStatus) encode(w io.Writer) error {
	if s.Error() == nil {
		_, err := pktline.Writef(w, "ok %s\n", s.ReferenceName.String())
		return err
	}

	_, err := pktline.Writef(w, "ng %s %s\n", s.ReferenceName.String(), s.Status)
	return err
}
