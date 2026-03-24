package index

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/go-cmp/cmp"
	. "gopkg.in/check.v1"
)

func (s *IndexSuite) TestEncode(c *C) {
	idx := &Index{
		Version: 2,
		Entries: []*Entry{{
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Dev:        4242,
			Inode:      424242,
			UID:        84,
			GID:        8484,
			Size:       42,
			Stage:      TheirMode,
			Hash:       plumbing.NewHash("e25b29c8946e0e192fae2edc1dabf7be71e8ecf3"),
			Name:       "foo",
		}, {
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Name:       "bar",
			Size:       82,
		}, {
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Name:       strings.Repeat(" ", 20),
			Size:       82,
		}},
	}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)
	err := e.Encode(idx)
	c.Assert(err, IsNil)

	output := &Index{}
	d := NewDecoder(buf)
	err = d.Decode(output)
	c.Assert(err, IsNil)

	c.Assert(cmp.Equal(idx, output), Equals, true)

	c.Assert(output.Entries[0].Name, Equals, strings.Repeat(" ", 20))
	c.Assert(output.Entries[1].Name, Equals, "bar")
	c.Assert(output.Entries[2].Name, Equals, "foo")
}

func TestEncodeLongName(t *testing.T) {
	t.Parallel()

	// Entry names >= 4095 bytes overflow the 12-bit length field in V2/V3
	// flags, which stores nameMask (0xFFF). The decoder must scan for the
	// NUL terminator to find the real length rather than trusting the field.
	longName := strings.Repeat("a", 5000)
	idx := &Index{
		Version: 2,
		Entries: []*Entry{
			{
				CreatedAt:  time.Now(),
				ModifiedAt: time.Now(),
				Name:       longName,
				Size:       1,
			},
			{
				CreatedAt:  time.Now(),
				ModifiedAt: time.Now(),
				Name:       "short",
				Size:       2,
			},
		},
	}

	buf := bytes.NewBuffer(nil)
	err := NewEncoder(buf).Encode(idx)
	require.NoError(t, err)

	output := &Index{}
	err = NewDecoder(buf).Decode(output)
	require.NoError(t, err)

	require.Len(t, output.Entries, 2)
	assert.Equal(t, longName, output.Entries[0].Name)
	assert.Equal(t, "short", output.Entries[1].Name)
	assert.Equal(t, uint32(1), output.Entries[0].Size)
	assert.Equal(t, uint32(2), output.Entries[1].Size)
}

func TestEncodeV4(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 4,
		Entries: []*Entry{{
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Dev:        4242,
			Inode:      424242,
			UID:        84,
			GID:        8484,
			Size:       42,
			Stage:      TheirMode,
			Hash:       plumbing.NewHash("e25b29c8946e0e192fae2edc1dabf7be71e8ecf3"),
			Name:       "foo",
		}, {
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Name:       "bar",
			Size:       82,
		}, {
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Name:       strings.Repeat(" ", 20),
			Size:       82,
		}, {
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Name:       "baz/bar",
			Size:       82,
		}, {
			CreatedAt:  time.Now(),
			ModifiedAt: time.Now(),
			Name:       "baz/bar/bar",
			Size:       82,
		}},
	}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)
	err := e.Encode(idx)
	require.NoError(t, err)

	output := &Index{}
	d := NewDecoder(buf)
	err = d.Decode(output)
	require.NoError(t, err)

	assert.EqualExportedValues(t, idx, output)

	assert.Equal(t, strings.Repeat(" ", 20), output.Entries[0].Name)
	assert.Equal(t, "bar", output.Entries[1].Name)
	assert.Equal(t, "baz/bar", output.Entries[2].Name)
	assert.Equal(t, "baz/bar/bar", output.Entries[3].Name)
	assert.Equal(t, "foo", output.Entries[4].Name)
}

func (s *IndexSuite) TestEncodeUnsupportedVersion(c *C) {
	idx := &Index{Version: 5}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)
	err := e.Encode(idx)
	c.Assert(err, Equals, ErrUnsupportedVersion)
}

func (s *IndexSuite) TestEncodeWithIntentToAddUnsupportedVersion(c *C) {
	idx := &Index{
		Version: 3,
		Entries: []*Entry{{IntentToAdd: true}},
	}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)
	err := e.Encode(idx)
	c.Assert(err, IsNil)

	output := &Index{}
	d := NewDecoder(buf)
	err = d.Decode(output)
	c.Assert(err, IsNil)

	c.Assert(cmp.Equal(idx, output), Equals, true)
	c.Assert(output.Entries[0].IntentToAdd, Equals, true)
}

func (s *IndexSuite) TestEncodeWithSkipWorktreeUnsupportedVersion(c *C) {
	idx := &Index{
		Version: 3,
		Entries: []*Entry{{SkipWorktree: true}},
	}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)
	err := e.Encode(idx)
	c.Assert(err, IsNil)

	output := &Index{}
	d := NewDecoder(buf)
	err = d.Decode(output)
	c.Assert(err, IsNil)

	c.Assert(cmp.Equal(idx, output), Equals, true)
	c.Assert(output.Entries[0].SkipWorktree, Equals, true)
}
