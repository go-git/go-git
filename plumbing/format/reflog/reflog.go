package reflog

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
)

// Signature represents an author or committer identity with a timestamp.
// This mirrors object.Signature but is defined here to avoid an import cycle
// (reflog -> object -> storer -> reflog).
type Signature struct {
	// Name represents a person name.
	Name string
	// Email is an email address.
	Email string
	// When is the timestamp of the signature.
	When time.Time
}

// Entry represents a single reflog entry.
type Entry struct {
	// OldHash is the hash the reference pointed to before the change.
	OldHash plumbing.Hash
	// NewHash is the hash the reference points to after the change.
	NewHash plumbing.Hash
	// Committer holds the signature for the entry, including name, email and when it was created.
	Committer Signature
	// Message describes the action that caused the change (e.g. "commit: Add feature").
	Message string
}

// Decoder reads reflog entries from a reader one at a time.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder creates a Decoder that reads reflog entries from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// Next returns the next reflog entry. It returns io.EOF when there are no more entries.
func (d *Decoder) Next() (*Entry, error) {
	for {
		line, err := d.r.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}

		line = bytes.TrimSuffix(line, []byte{'\n'})
		if len(line) != 0 {
			return decodeLine(line)
		}

		if err == io.EOF {
			return nil, io.EOF
		}
	}
}

// Decode reads all reflog entries from the reader.
// Entries are returned in file order (oldest first).
func Decode(r io.Reader) ([]*Entry, error) {
	if r == nil {
		return nil, fmt.Errorf("reader is nil")
	}
	d := NewDecoder(r)
	var entries []*Entry
	for {
		e, err := d.Next()
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return entries, err
		}
		entries = append(entries, e)
	}
}

// decodeLine parses a single reflog line.
// Format: <old-hash> <new-hash> <name> <<email>> <unix-timestamp> <timezone>\t<message>
func decodeLine(line []byte) (*Entry, error) {
	e := &Entry{}

	// Parse old hash (up to first space)
	spaceIdx := bytes.IndexByte(line, ' ')
	if spaceIdx == -1 {
		return nil, fmt.Errorf("reflog entry too short")
	}
	oldHashStr := string(line[:spaceIdx])
	if !plumbing.IsHash(oldHashStr) {
		return nil, fmt.Errorf("invalid old hash in reflog entry: %q", oldHashStr)
	}
	e.OldHash = plumbing.NewHash(oldHashStr)
	line = line[spaceIdx+1:]

	// Parse new hash (up to next space)
	spaceIdx = bytes.IndexByte(line, ' ')
	if spaceIdx == -1 {
		return nil, fmt.Errorf("expected space after new hash")
	}
	newHashStr := string(line[:spaceIdx])
	if !plumbing.IsHash(newHashStr) {
		return nil, fmt.Errorf("invalid new hash in reflog entry: %q", newHashStr)
	}
	e.NewHash = plumbing.NewHash(newHashStr)
	line = line[spaceIdx+1:]

	// Split on tab to separate signature from message
	sigBytes := line
	before, after, ok := bytes.Cut(line, []byte{'\t'})
	if ok {
		sigBytes = before
		e.Message = string(after)
	}

	// Parse signature: Name <email> timestamp timezone
	open := bytes.LastIndexByte(sigBytes, '<')
	closeBracket := bytes.LastIndexByte(sigBytes, '>')
	if open == -1 || closeBracket == -1 || closeBracket < open {
		return nil, fmt.Errorf("invalid signature in reflog entry")
	}

	e.Committer.Name = string(bytes.TrimSpace(sigBytes[:open]))
	e.Committer.Email = string(sigBytes[open+1 : closeBracket])

	// Parse timestamp and timezone after '> '
	if closeBracket+2 >= len(sigBytes) {
		return nil, fmt.Errorf("missing timestamp in reflog entry")
	}
	var err error
	e.Committer.When, err = decodeTimestamp(sigBytes[closeBracket+2:])
	if err != nil {
		return nil, err
	}

	return e, nil
}

func decodeTimestamp(s []byte) (time.Time, error) {
	// Format: "1234567890 +0000"
	parts := bytes.Fields(s)
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid timestamp in reflog entry: %q", s)
	}

	secs, err := strconv.ParseInt(string(parts[0]), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp seconds in reflog entry: %w", err)
	}

	t := time.Unix(secs, 0)

	tz := string(parts[1])
	if len(tz) != 5 || (tz[0] != '+' && tz[0] != '-') {
		return time.Time{}, fmt.Errorf("invalid timezone in reflog entry: %q", tz)
	}
	h, err := strconv.Atoi(tz[1:3])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone hours in reflog entry: %w", err)
	}
	m, err := strconv.Atoi(tz[3:5])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone minutes in reflog entry: %w", err)
	}
	offset := h*3600 + m*60
	if tz[0] == '-' {
		offset = -offset
	}
	t = t.In(time.FixedZone("", offset))

	return t, nil
}

// normalizeMessage normalizes a reflog message the same way Git does:
// collapse consecutive whitespace to a single space, strip leading/trailing
// whitespace, and remove newlines.
// See copy_reflog_msg in refs.c:
// https://github.com/git/git/blob/7ff1e8dc1e1680510c96e69965b3fa81372c5037/refs.c#L1026-L1049
func normalizeMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	fields := strings.Fields(msg)
	return strings.Join(fields, " ")
}

// Encode writes a single reflog entry to the writer.
func Encode(w io.Writer, e *Entry) error {
	if w == nil {
		return fmt.Errorf("writer is nil")
	}
	if e == nil {
		return fmt.Errorf("entry is nil")
	}
	_, offset := e.Committer.When.Zone()
	sign := '+'
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60

	msg := normalizeMessage(e.Message)

	if msg != "" {
		_, err := fmt.Fprintf(w, "%s %s %s <%s> %d %c%02d%02d\t%s\n",
			e.OldHash, e.NewHash,
			e.Committer.Name, e.Committer.Email,
			e.Committer.When.Unix(), sign, hours, minutes,
			msg,
		)
		return err
	}

	_, err := fmt.Fprintf(w, "%s %s %s <%s> %d %c%02d%02d\n",
		e.OldHash, e.NewHash,
		e.Committer.Name, e.Committer.Email,
		e.Committer.When.Unix(), sign, hours, minutes,
	)
	return err
}
