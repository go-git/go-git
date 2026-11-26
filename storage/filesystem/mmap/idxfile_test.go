//go:build darwin || linux

package mmap

import (
	"bytes"
	"crypto"
	"crypto/sha1" //nolint:gosec // the checksum of a SHA-1 pack index
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

// syntheticIdx returns a version 2 index for count objects whose ids all
// start with firstByte, followed by off64Bytes of 64-bit offset table.
// Only the layout is meaningful: ids, CRCs, offsets and checksums are zero.
func syntheticIdx(hashSize int, count uint32, firstByte byte, off64Bytes int) []byte {
	idx := append([]byte{}, idxSignature...)
	idx = binary.BigEndian.AppendUint32(idx, idxSupported)
	for b := range 256 {
		var n uint32
		if b >= int(firstByte) {
			n = count
		}
		idx = binary.BigEndian.AppendUint32(idx, n)
	}
	tables := int(count)*(hashSize+idxCrcSize+off32Size) + off64Bytes + 2*hashSize
	return append(idx, make([]byte, tables)...)
}

func setFanout(idx []byte, entry int, n uint32) []byte {
	binary.BigEndian.PutUint32(idx[idxHeaderSize+entry*4:], n)
	return idx
}

func TestValidateIdx(t *testing.T) {
	t.Parallel()

	for _, hashSize := range []int{crypto.SHA1.Size(), crypto.SHA256.Size()} {
		tests := []struct {
			name      string
			idx       []byte
			wantCount int
			wantErr   string
		}{
			{
				name: "no objects",
				idx:  syntheticIdx(hashSize, 0, 0, 0),
			},
			{
				name:    "no objects, trailing byte",
				idx:     syntheticIdx(hashSize, 0, 0, 1),
				wantErr: "0 objects need",
			},
			{
				name:      "one object",
				idx:       syntheticIdx(hashSize, 1, 0x80, 0),
				wantCount: 1,
			},
			{
				name:    "one object cannot have a 64-bit offset",
				idx:     syntheticIdx(hashSize, 1, 0x80, off64Size),
				wantErr: "1 objects need",
			},
			{
				name:      "no 64-bit offsets",
				idx:       syntheticIdx(hashSize, 5, 0x80, 0),
				wantCount: 5,
			},
			{
				name:      "one 64-bit offset",
				idx:       syntheticIdx(hashSize, 5, 0x80, off64Size),
				wantCount: 5,
			},
			{
				name:      "size between two 64-bit table sizes",
				idx:       syntheticIdx(hashSize, 5, 0x80, off64Size+1),
				wantCount: 5,
			},
			{
				name:      "every object but one has a 64-bit offset",
				idx:       syntheticIdx(hashSize, 5, 0x80, 4*off64Size),
				wantCount: 5,
			},
			{
				name:    "one byte past the largest 64-bit offset table",
				idx:     syntheticIdx(hashSize, 5, 0x80, 4*off64Size+1),
				wantErr: "5 objects need",
			},
			{
				name:    "a 64-bit offset for every object",
				idx:     syntheticIdx(hashSize, 5, 0x80, 5*off64Size),
				wantErr: "5 objects need",
			},
			{
				name:    "one byte short",
				idx:     syntheticIdx(hashSize, 5, 0x80, -1),
				wantErr: "5 objects need",
			},
			{
				name:    "count larger than the file",
				idx:     setFanout(syntheticIdx(hashSize, 5, 0x80, 0), 0xff, 6),
				wantErr: "6 objects need",
			},
			{
				name:    "largest count",
				idx:     setFanout(syntheticIdx(hashSize, 5, 0x80, 0), 0xff, math.MaxUint32),
				wantErr: "4294967295 objects need",
			},
			{
				name:    "first fanout entry above the second",
				idx:     setFanout(syntheticIdx(hashSize, 5, 0x80, 0), 0, 1),
				wantErr: "fanout entry 1 ",
			},
			{
				name:    "fanout drops in the middle",
				idx:     setFanout(syntheticIdx(hashSize, 5, 0x80, 0), 0x90, 4),
				wantErr: "fanout entry 144 ",
			},
			{
				name:    "last fanout entry below the one before",
				idx:     setFanout(syntheticIdx(hashSize, 5, 0x80, 0), 0xff, 4),
				wantErr: "fanout entry 255 ",
			},
			{
				name:    "empty file",
				idx:     nil,
				wantErr: "too short",
			},
			{
				name:    "header only",
				idx:     syntheticIdx(hashSize, 0, 0, 0)[:idxHeaderSize],
				wantErr: "too short",
			},
			{
				name:    "fanout cut short",
				idx:     syntheticIdx(hashSize, 0, 0, 0)[:idxHeaderSize+idxFanoutSize-1],
				wantErr: "too short",
			},
			{
				name:    "wrong signature",
				idx:     append([]byte("PACK"), syntheticIdx(hashSize, 5, 0x80, 0)[4:]...),
				wantErr: "signature",
			},
			{
				name:    "version 1",
				idx:     binary.BigEndian.AppendUint32(append([]byte{}, idxSignature...), 1),
				wantErr: "version 1",
			},
			{
				name:    "version 3",
				idx:     binary.BigEndian.AppendUint32(append([]byte{}, idxSignature...), 3),
				wantErr: "version 3",
			},
		}

		for _, tc := range tests {
			t.Run(fmt.Sprintf("%d-byte ids/%s", hashSize, tc.name), func(t *testing.T) {
				t.Parallel()

				idx := tc.idx
				if strings.HasPrefix(tc.name, "version") {
					idx = append(idx, syntheticIdx(hashSize, 5, 0x80, 0)[idxHeaderSize:]...)
				}

				count, err := validateIdx(idx, hashSize)
				if tc.wantErr == "" {
					require.NoError(t, err)
					assert.Equal(t, tc.wantCount, count)
					return
				}
				require.ErrorIs(t, err, ErrCorruptedIdx)
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Zero(t, count)
			})
		}
	}
}

// TestValidateIdxMatchesGit packs a few blobs with git, then checks that
// validateIdx accepts exactly the resized or rewritten copies of the index
// that git is still able to look an object up in.
func TestValidateIdxMatchesGit(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("oracle disabled: git not found: %v", err)
	}

	runGit := func(t *testing.T, dir, stdin string, args ...string) (string, error) {
		t.Helper()

		cmd := gitenv.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	mustGit := func(t *testing.T, dir, stdin string, args ...string) string {
		t.Helper()

		out, err := runGit(t, dir, stdin, args...)
		require.NoError(t, err, "git %s", strings.Join(args, " "))
		return out
	}

	source := t.TempDir()
	mustGit(t, source, "", "init", "-q", "--bare", ".")
	contents := []string{"one\n", "two\n", "three\n", "four\n", "five\n"}
	ids := make([]string, 0, len(contents))
	for _, content := range contents {
		ids = append(ids, mustGit(t, source, content, "hash-object", "-w", "--stdin"))
	}
	packDir := t.TempDir()
	packName := "pack-" + mustGit(t, source, strings.Join(ids, "\n")+"\n",
		"pack-objects", "-q", filepath.Join(packDir, "pack"))
	pack, err := os.ReadFile(filepath.Join(packDir, packName+".pack"))
	require.NoError(t, err)
	original, err := os.ReadFile(filepath.Join(packDir, packName+".idx"))
	require.NoError(t, err)

	hashSize := crypto.SHA1.Size()
	count := uint32(len(ids))
	// The 64-bit offset table sits right before the two trailing checksums.
	withOff64Bytes := func(n int) []byte {
		trailerStart := len(original) - 2*hashSize
		idx := bytes.Clone(original[:trailerStart])
		idx = append(idx, make([]byte, n)...)
		return append(idx, original[trailerStart:]...)
	}
	withFanout := func(entry int, n uint32) []byte {
		return setFanout(bytes.Clone(original), entry, n)
	}

	cases := []struct {
		name string
		idx  []byte
	}{
		{"unchanged", bytes.Clone(original)},
		{"one-byte-short", bytes.Clone(original[:len(original)-1])},
		{"one-64-bit-offset", withOff64Bytes(off64Size)},
		{"one-64-bit-offset-and-a-byte", withOff64Bytes(off64Size + 1)},
		{"largest-64-bit-table", withOff64Bytes(int(count-1) * off64Size)},
		{"largest-64-bit-table-and-a-byte", withOff64Bytes(int(count-1)*off64Size + 1)},
		{"64-bit-offset-per-object", withOff64Bytes(int(count) * off64Size)},
		{"count-above-file-size", withFanout(0xff, count+1)},
		{"count-1<<20", withFanout(0xff, 1<<20)},
		{"largest-count", withFanout(0xff, math.MaxUint32)},
		{"first-fanout-above-count", withFanout(0, count+1)},
		{"last-fanout-zero", withFanout(0xff, 0)},
		{"version-3", append(binary.BigEndian.AppendUint32(bytes.Clone(original[:4]), 3), original[8:]...)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The index ends with a checksum of everything before it.
			body := tc.idx[:len(tc.idx)-hashSize]
			sum := sha1.Sum(body) //nolint:gosec // as above
			idx := append(bytes.Clone(body), sum[:]...)

			probe := t.TempDir()
			mustGit(t, probe, "", "init", "-q", "--bare", ".")
			packPath := filepath.Join(probe, "objects", "pack", packName)
			require.NoError(t, os.WriteFile(packPath+".pack", pack, 0o600))
			require.NoError(t, os.WriteFile(packPath+".idx", idx, 0o600))

			_, gitErr := runGit(t, probe, "", "cat-file", "-e", ids[0])
			if gitErr != nil {
				require.IsType(t, &exec.ExitError{}, gitErr)
			}

			gotCount, err := validateIdx(idx, hashSize)
			if gitErr != nil {
				assert.ErrorIs(t, err, ErrCorruptedIdx, "git cannot use this index")
				return
			}
			require.NoError(t, err, "git reads an object through this index")
			assert.Equal(t, int(count), gotCount)
		})
	}
}
