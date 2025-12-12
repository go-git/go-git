//go:build darwin || linux

package mmap

import (
	"crypto"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestNewPackScanner(t *testing.T) {
	t.Parallel()

	fixture := fixtures.NewOSFixture(
		fixtures.ByTag("packfile-sha256").One(),
		t.TempDir(),
	)

	tests := []struct {
		name     string
		hashSize int
		pack     func() billy.File
		idx      func() billy.File
		rev      func() billy.File
		want     string
	}{
		{
			name:     "nil pack file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return nil },
			idx:      func() billy.File { return fixture.Idx() },
			rev:      func() billy.File { return fixture.Rev() },
			want:     "cannot create mmap for .pack file",
		},
		{
			name:     "nil idx file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Packfile() },
			idx:      func() billy.File { return nil },
			rev:      func() billy.File { return fixture.Rev() },
			want:     "cannot create mmap for .idx file",
		},
		{
			name:     "nil rev file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Packfile() },
			idx:      func() billy.File { return fixture.Idx() },
			rev:      func() billy.File { return nil },
			want:     "cannot create mmap for .rev file",
		},
		{
			name:     "invalid pack file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Rev() },
			idx:      func() billy.File { return fixture.Idx() },
			rev:      func() billy.File { return fixture.Rev() },
			want:     "malformed pack file",
		},
		{
			name:     "invalid idx file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Packfile() },
			idx:      func() billy.File { return fixture.Rev() },
			rev:      func() billy.File { return fixture.Rev() },
			want:     "malformed idx file",
		},
		{
			name:     "invalid rev file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Packfile() },
			idx:      func() billy.File { return fixture.Idx() },
			rev:      func() billy.File { return fixture.Packfile() },
			want:     "malformed rev file",
		},
		{
			name:     "valid files sha256",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Packfile() },
			idx:      func() billy.File { return fixture.Idx() },
			rev:      func() billy.File { return fixture.Rev() },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scanner, err := NewPackScanner(tc.hashSize, tc.pack(), tc.idx(), tc.rev())

			if tc.want != "" {
				assert.ErrorContains(t, err, tc.want)
				assert.Nil(t, scanner)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, scanner)
				if scanner != nil {
					assert.NoError(t, scanner.Close())
				}
			}
		})
	}
}

func TestPackScannerClose(t *testing.T) {
	t.Parallel()

	fixture := fixtures.NewOSFixture(
		fixtures.ByTag("packfile-sha256").One(),
		t.TempDir(),
	)

	scanner, err := NewPackScanner(
		crypto.SHA256.Size(),
		fixture.Packfile(),
		fixture.Idx(),
		fixture.Rev(),
	)
	require.NoError(t, err)
	require.NotNil(t, scanner)

	err = scanner.Close()
	assert.NoError(t, err)

	// Closing again should not panic, but error as files are already closed.
	err = scanner.Close()
	assert.Error(t, err)
}

func TestSearchObjectID(t *testing.T) {
	t.Parallel()

	names := []byte{
		0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11,
		0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22,
		0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33,
	}
	names256 := []byte{
		0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11,
		0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22,
		0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33,
	}

	tests := []struct {
		name      string
		lo        int
		hi        int
		names     []byte
		want      plumbing.Hash
		wantIndex int
		wantFound bool
	}{
		{
			name:      "empty names",
			lo:        0,
			hi:        5,
			names:     []byte{},
			want:      plumbing.NewHash("1111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "find first hash",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: true,
		},
		{
			name:      "find middle hash",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("2222222222222222222222222222222222222222"),
			wantIndex: 1,
			wantFound: true,
		},
		{
			name:      "find last hash",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("3333333333333333333333333333333333333333"),
			wantIndex: 2,
			wantFound: true,
		},
		{
			name:      "hash not found - too low",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("0000000000000000000000000000000000000000"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "hash not found - too high",
			lo:        0,
			hi:        3,
			names:     names,
			want:      plumbing.NewHash("4444444444444444444444444444444444444444"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "empty range",
			lo:        0,
			hi:        0,
			names:     names,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "find first hash (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: true,
		},
		{
			name:      "find middle hash (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("2222222222222222222222222222222222222222222222222222222222222222"),
			wantIndex: 1,
			wantFound: true,
		},
		{
			name:      "find last hash (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("3333333333333333333333333333333333333333333333333333333333333333"),
			wantIndex: 2,
			wantFound: true,
		},
		{
			name:      "hash not found - too low (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("0000000000000000000000000000000000000000000000000000000000000000"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "hash not found - too high (256)",
			lo:        0,
			hi:        3,
			names:     names256,
			want:      plumbing.NewHash("4444444444444444444444444444444444444444444444444444444444444444"),
			wantIndex: 0,
			wantFound: false,
		},
		{
			name:      "empty range (256)",
			lo:        0,
			hi:        0,
			names:     names256,
			want:      plumbing.NewHash("1111111111111111111111111111111111111111111111111111111111111111"),
			wantIndex: 0,
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotIndex, gotFound := searchObjectID(tc.names, tc.lo, tc.hi, tc.want)
			assert.Equal(t, tc.wantFound, gotFound, "found mismatch")
			assert.Equal(t, tc.wantIndex, gotIndex, "index mismatch")
		})
	}
}
