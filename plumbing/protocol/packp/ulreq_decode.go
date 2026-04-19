package packp

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// Decode reads the next upload-request from its input and
// stores it in the UploadRequest.
func (req *UploadRequest) Decode(r io.Reader) error {
	var (
		nLine         int
		line          []byte
		deepenRevList bool
	)

	nextLine := func() (hasData bool, err error) {
		nLine++
		l, p, err := pktline.ReadLine(r)
		if err == io.EOF {
			return false, NewErrUnexpectedData(fmt.Sprintf("pkt-line %d: EOF", nLine), line)
		}
		if err != nil {
			return false, err
		}
		if l == pktline.Flush {
			return false, nil
		}
		line = bytes.TrimSuffix(p, eol)
		return true, nil
	}

	decodeError := func(format string, a ...any) error {
		msg := fmt.Sprintf("pkt-line %d: %s", nLine, fmt.Sprintf(format, a...))
		return NewErrUnexpectedData(msg, line)
	}

	readHash := func() (plumbing.Hash, error) {
		h, err := hashFrom(line)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("malformed hash: %v", line)
		}
		line = line[h.HexSize():]
		return h, nil
	}

	// --- Want lines ---

	// First want line: want <hash>[ capabilities]
	ok, err := nextLine()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("empty input")
	}

	if !bytes.HasPrefix(line, want) {
		return decodeError("missing 'want ' prefix")
	}
	line = bytes.TrimPrefix(line, want)

	hash, err := readHash()
	if err != nil {
		return err
	}
	req.Wants = append(req.Wants, hash)

	// Capabilities (if present after SP)
	line = bytes.TrimPrefix(line, sp)
	capability.DecodeList(line, &req.Capabilities)

	// Additional want lines
	for {
		ok, err := nextLine()
		if err != nil {
			return err
		}
		if !ok || len(line) == 0 {
			return nil
		}

		if !bytes.HasPrefix(line, want) {
			break
		}

		line = bytes.TrimPrefix(line, want)
		h, err := readHash()
		if err != nil {
			return err
		}
		req.Wants = append(req.Wants, h)
	}

	// --- Shallow lines ---

	for bytes.HasPrefix(line, shallow) {
		line = bytes.TrimPrefix(line, shallow)

		h, err := readHash()
		if err != nil {
			return err
		}
		req.Shallows = append(req.Shallows, h)

		ok, err := nextLine()
		if err != nil {
			return err
		}
		if !ok || len(line) == 0 {
			return nil
		}
	}

	// --- Deepen lines ---

	for bytes.HasPrefix(line, deepen) {
		switch {
		case bytes.HasPrefix(line, deepenCommits):
			if deepenRevList {
				return fmt.Errorf("deepen and deepen-since (or deepen-not) cannot be used together")
			}
			line = bytes.TrimPrefix(line, deepenCommits)
			n, err := strconv.Atoi(string(line))
			if err != nil {
				return err
			}
			if n < 0 {
				return fmt.Errorf("negative depth")
			}
			req.Depth = DepthRequest{Deepen: n}
		case bytes.HasPrefix(line, deepenSince):
			if req.Depth.Deepen > 0 {
				return fmt.Errorf("deepen and deepen-since (or deepen-not) cannot be used together")
			}
			line = bytes.TrimPrefix(line, deepenSince)
			secs, err := strconv.ParseInt(string(line), 10, 64)
			if err != nil {
				return err
			}
			req.Depth.DeepenSince = time.Unix(secs, 0).UTC()
			deepenRevList = true
		case bytes.HasPrefix(line, deepenReference):
			if req.Depth.Deepen > 0 {
				return fmt.Errorf("deepen and deepen-since (or deepen-not) cannot be used together")
			}
			line = bytes.TrimPrefix(line, deepenReference)
			req.Depth.DeepenNot = append(req.Depth.DeepenNot, string(line))
			deepenRevList = true
		default:
			return decodeError("unexpected deepen specification: %q", line)
		}

		ok, err := nextLine()
		if err != nil {
			return err
		}
		if !ok || len(line) == 0 {
			return nil
		}

		// After deepen <n>, only flush-pkt is valid
		if req.Depth.Deepen > 0 {
			if bytes.HasPrefix(line, deepenSince) || bytes.HasPrefix(line, deepenReference) {
				return fmt.Errorf("deepen and deepen-since (or deepen-not) cannot be used together")
			}
			return decodeError("unexpected payload while expecting a flush-pkt: %q", line)
		}
		// After deepen-since/deepen-not, only deepen-since/deepen-not or flush is valid
		if deepenRevList && bytes.HasPrefix(line, deepen) && !bytes.HasPrefix(line, deepenSince) && !bytes.HasPrefix(line, deepenReference) {
			return fmt.Errorf("deepen and deepen-since (or deepen-not) cannot be used together")
		}
	}

	// Unexpected payload after shallows or wants
	if len(line) != 0 {
		return decodeError("unexpected payload while expecting a flush-pkt: %q", line)
	}

	return nil
}
