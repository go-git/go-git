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
	err := NewEncoder(buf, crypto.SHA1.New()).Encode(idx)
	require.NoError(t, err)

	output := &Index{}
	err = NewDecoder(buf, crypto.SHA1.New()).Decode(output)
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

func TestEncodeSkipHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hash crypto.Hash
	}{
		{"SHA1", crypto.SHA1},
		{"SHA256", crypto.SHA256},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hashSize := tc.hash.New().Size()

			var h1, h2 plumbing.Hash
			h1.ResetBySize(hashSize)
			h2.ResetBySize(hashSize)
			copy(h1.Bytes(), []byte{0xe2, 0x5b, 0x29})
			copy(h2.Bytes(), []byte{0x00})

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
					Hash:       h1,
					Name:       "foo",
				}, {
					CreatedAt:  time.Now(),
					ModifiedAt: time.Now(),
					Name:       "bar",
					Hash:       h2,
					Size:       82,
				}},
			}

			buf := bytes.NewBuffer(nil)
			e := NewEncoder(buf, tc.hash.New(), WithSkipHashEncoder())
			err := e.Encode(idx)
			require.NoError(t, err)

			raw := buf.Bytes()

			checksum := raw[len(raw)-hashSize:]
			assert.Equal(t, make([]byte, hashSize), checksum)

			// A normal decoder must reject the null checksum.
			output := &Index{}
			d := NewDecoder(bytes.NewReader(raw), tc.hash.New())
			err = d.Decode(output)
			assert.ErrorIs(t, err, ErrInvalidChecksum)

			// A skipHash decoder must accept it and recover the entries.
			output = &Index{}
			d = NewDecoder(bytes.NewReader(raw), tc.hash.New(), WithSkipHash())
			err = d.Decode(output)
			require.NoError(t, err)

			assert.EqualExportedValues(t, idx, output)
		})
	}
}
