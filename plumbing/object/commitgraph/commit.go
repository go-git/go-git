package commitgraph

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/ioutil"
	gogitsync "github.com/go-git/go-git/v6/utils/sync"
)

var fixedZones sync.Map // int seconds east of UTC -> *time.Location

// Commit holds the base traversal fields parsed from an encoded commit object.
// It is a lightweight stand-in for object.Commit, carrying only what commit
// graph walking needs: tree, parents, and the committer/author timestamps.
// It is intended to be treated as a read-only value after decode.
type Commit struct {
	id           plumbing.Hash
	treeHash     plumbing.Hash
	parentHashes []plumbing.Hash
	when         time.Time
	authorWhen   time.Time
}

// ID returns the hash of the commit object.
func (c *Commit) ID() plumbing.Hash { return c.id }

// Tree returns the hash of the tree referenced by the commit.
func (c *Commit) Tree() plumbing.Hash { return c.treeHash }

// Parents returns the hashes of the commit's parents.
func (c *Commit) Parents() []plumbing.Hash { return c.parentHashes }

// When returns the committer timestamp.
func (c *Commit) When() time.Time { return c.when }

// AuthorWhen returns the author timestamp.
func (c *Commit) AuthorWhen() time.Time { return c.authorWhen }

// DecodeCommit parses the base traversal fields from an encoded commit object.
// It reads only up to and including the committer header, which in canonical
// git commit order precedes any gpgsig/mergetag/extra headers and the message
// body, so those are never parsed.
func DecodeCommit(obj plumbing.EncodedObject) (c *Commit, err error) {
	if obj.Type() != plumbing.CommitObject {
		return nil, object.ErrUnsupportedObject
	}

	reader, err := obj.Reader()
	if err != nil {
		return nil, fmt.Errorf("open commit reader: %w", err)
	}
	defer ioutil.CheckClose(reader, &err)

	c = &Commit{id: obj.Hash()}
	if err = c.decode(reader); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Commit) decode(reader io.Reader) error {
	br := gogitsync.GetBufioReader(reader)
	defer gogitsync.PutBufioReader(br)

	s := &commitScanner{r: br, commit: c}
	for state := scanCommitTree; state != nil; {
		state = state(s)
	}
	if s.err != nil {
		return s.err
	}
	if !s.sawTree {
		return errors.New("malformed commit: missing tree header")
	}
	return nil
}

type commitScanner struct {
	r       *bufio.Reader
	commit  *Commit
	pending []byte
	err     error

	sawTree bool
}

type commitState func(*commitScanner) commitState

// readLine returns the next line from the input. atEnd is true when EOF was
// reached (the returned line may still contain trailing unterminated bytes).
// Real I/O errors are recorded on the scanner and surfaced via decode's return.
func (s *commitScanner) readLine() (line []byte, atEnd bool) {
	if s.pending != nil {
		line = s.pending
		s.pending = nil
		return line, false
	}
	line, err := s.r.ReadBytes('\n')
	switch {
	case errors.Is(err, io.EOF):
		return line, true
	case err != nil:
		s.err = fmt.Errorf("read commit line: %w", err)
		return nil, true
	}
	return line, false
}

func (s *commitScanner) pushBack(line []byte) {
	s.pending = line
}

func (s *commitScanner) fail(err error) commitState {
	s.err = err
	return nil
}

func scanCommitTree(s *commitScanner) commitState {
	line, atEnd := s.readLine()
	if s.err != nil {
		return nil
	}
	if len(line) == 0 || isBlankLine(line) {
		return s.fail(errors.New("malformed commit: missing tree header"))
	}
	key, data := splitHeader(line)
	if key != "tree" {
		return s.fail(errors.New("malformed commit: tree header must be first"))
	}
	h, ok := hashFromHex(data)
	if !ok {
		return s.fail(errors.New("invalid tree hash"))
	}
	s.commit.treeHash = h
	s.sawTree = true
	if atEnd {
		return nil
	}
	return scanCommitParents
}

func scanCommitParents(s *commitScanner) commitState {
	line, atEnd := s.readLine()
	if s.err != nil {
		return nil
	}
	if len(line) == 0 || isBlankLine(line) {
		return nil
	}
	key, data := splitHeader(line)
	if key == "parent" {
		h, ok := hashFromHex(data)
		if !ok {
			return s.fail(errors.New("invalid parent hash"))
		}
		s.commit.parentHashes = append(s.commit.parentHashes, h)
		if atEnd {
			return nil
		}
		return scanCommitParents
	}
	s.pushBack(line)
	return scanCommitAuthor
}

func scanCommitAuthor(s *commitScanner) commitState {
	line, atEnd := s.readLine()
	if s.err != nil {
		return nil
	}
	if len(line) == 0 || isBlankLine(line) {
		return nil
	}
	key, data := splitHeader(line)
	if key == "author" {
		w, ok := parseWhen(data)
		if !ok {
			return s.fail(errors.New("invalid author line"))
		}
		s.commit.authorWhen = w
		if atEnd {
			return nil
		}
		return scanCommitCommitter
	}
	s.pushBack(line)
	return scanCommitCommitter
}

func scanCommitCommitter(s *commitScanner) commitState {
	line, _ := s.readLine()
	if s.err != nil {
		return nil
	}
	if len(line) == 0 || isBlankLine(line) {
		return nil
	}
	key, data := splitHeader(line)
	if key == "committer" {
		w, ok := parseWhen(data)
		if !ok {
			return s.fail(errors.New("invalid committer line"))
		}
		s.commit.when = w
	}
	return nil
}

func isBlankLine(line []byte) bool {
	return len(line) == 1 && line[0] == '\n'
}

func splitHeader(line []byte) (string, []byte) {
	trimmed := bytes.TrimRight(line, "\n")
	key, value, ok := bytes.Cut(trimmed, []byte{' '})
	if !ok {
		return string(trimmed), nil
	}
	return string(key), value
}

// parseWhen extracts the timestamp and timezone from a git signature line.
func parseWhen(in []byte) (time.Time, bool) {
	closeBracket := bytes.LastIndexByte(in, '>')
	if closeBracket < 0 || closeBracket+2 >= len(in) {
		return time.Time{}, false
	}
	tail := in[closeBracket+2:]
	space := bytes.IndexByte(tail, ' ')
	if space < 0 {
		space = len(tail)
	}
	ts, ok := parseIntBytes(tail[:space])
	if !ok {
		return time.Time{}, false
	}
	when := time.Unix(ts, 0).UTC()

	tzStart := space + 1
	if tzStart+5 > len(tail) {
		return when, true
	}
	offset, ok := parseTimezoneOffset(tail[tzStart : tzStart+5])
	if !ok {
		return when, true
	}
	return when.In(fixedZone(offset)), true
}

func parseIntBytes(in []byte) (int64, bool) {
	if len(in) == 0 {
		return 0, false
	}
	neg := false
	if in[0] == '-' {
		neg = true
		in = in[1:]
		if len(in) == 0 {
			return 0, false
		}
	}
	var n int64
	for _, b := range in {
		if b < '0' || b > '9' {
			return 0, false
		}
		n = n*10 + int64(b-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

func parseTimezoneOffset(in []byte) (int, bool) {
	if len(in) != 5 {
		return 0, false
	}
	sign := 1
	switch in[0] {
	case '-':
		sign = -1
	case '+':
	default:
		return 0, false
	}
	hours, ok := parseTwoDigits(in[1], in[2])
	if !ok {
		return 0, false
	}
	mins, ok := parseTwoDigits(in[3], in[4])
	if !ok {
		return 0, false
	}
	return sign * ((hours * 60 * 60) + (mins * 60)), true
}

func parseTwoDigits(a, b byte) (int, bool) {
	if a < '0' || a > '9' || b < '0' || b > '9' {
		return 0, false
	}
	return int(a-'0')*10 + int(b-'0'), true
}

func fixedZone(offset int) *time.Location {
	if z, ok := fixedZones.Load(offset); ok {
		location, ok := z.(*time.Location)
		if !ok {
			return time.FixedZone("", offset)
		}
		return location
	}
	z := time.FixedZone("", offset)
	actual, _ := fixedZones.LoadOrStore(offset, z)
	location, ok := actual.(*time.Location)
	if !ok {
		return z
	}
	return location
}

func hashFromHex(in []byte) (plumbing.Hash, bool) {
	if len(in) != 40 && len(in) != 64 {
		return plumbing.ZeroHash, false
	}
	return plumbing.FromHex(string(in))
}
