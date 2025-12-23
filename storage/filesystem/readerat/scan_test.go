package readerat

import (
	"crypto"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			want:     "file is nil",
		},
		{
			name:     "nil idx file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Packfile() },
			idx:      func() billy.File { return nil },
			rev:      func() billy.File { return fixture.Rev() },
			want:     "file is nil",
		},
		{
			name:     "nil rev file",
			hashSize: crypto.SHA256.Size(),
			pack:     func() billy.File { return fixture.Packfile() },
			idx:      func() billy.File { return fixture.Idx() },
			rev:      func() billy.File { return nil },
			want:     "file is nil",
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
