package index

import (
	"bytes"
	"crypto"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestEncode(t *testing.T) {
	t.Parallel()
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
	e := NewEncoder(buf, crypto.SHA1.New())
	err := e.Encode(idx)
	assert.NoError(t, err)

	output := &Index{}
	d := NewDecoder(buf, crypto.SHA1.New())
	err = d.Decode(output)
	assert.NoError(t, err)

	assert.EqualExportedValues(t, idx, output)

	assert.Equal(t, strings.Repeat(" ", 20), output.Entries[0].Name)
	assert.Equal(t, "bar", output.Entries[1].Name)
	assert.Equal(t, "foo", output.Entries[2].Name)
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
	e := NewEncoder(buf, crypto.SHA1.New())
	err := e.Encode(idx)
	require.NoError(t, err)

	output := &Index{}
	d := NewDecoder(buf, crypto.SHA1.New())
	err = d.Decode(output)
	require.NoError(t, err)

	assert.EqualExportedValues(t, idx, output)

	assert.Equal(t, strings.Repeat(" ", 20), output.Entries[0].Name)
	assert.Equal(t, "bar", output.Entries[1].Name)
	assert.Equal(t, "baz/bar", output.Entries[2].Name)
	assert.Equal(t, "baz/bar/bar", output.Entries[3].Name)
	assert.Equal(t, "foo", output.Entries[4].Name)
}

func TestEncodeUnsupportedVersion(t *testing.T) {
	t.Parallel()
	idx := &Index{Version: 5}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())
	err := e.Encode(idx)
	assert.Equal(t, ErrUnsupportedVersion, err)
}

func TestEncodeWithIntentToAddUnsupportedVersion(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 3,
		Entries: []*Entry{{IntentToAdd: true}},
	}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())
	err := e.Encode(idx)
	assert.NoError(t, err)

	output := &Index{}
	d := NewDecoder(buf, crypto.SHA1.New())
	err = d.Decode(output)
	assert.NoError(t, err)

	assert.EqualExportedValues(t, idx, output)
	assert.Equal(t, true, output.Entries[0].IntentToAdd)
}

func TestEncodeWithSkipWorktreeUnsupportedVersion(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Version: 3,
		Entries: []*Entry{{SkipWorktree: true}},
	}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf, crypto.SHA1.New())
	err := e.Encode(idx)
	assert.NoError(t, err)

	output := &Index{}
	d := NewDecoder(buf, crypto.SHA1.New())
	err = d.Decode(output)
	assert.NoError(t, err)

	assert.EqualExportedValues(t, idx, output)
	assert.Equal(t, true, output.Entries[0].SkipWorktree)
}
