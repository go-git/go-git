package packp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

type ShallowUpdateSuite struct {
	suite.Suite
}

func TestShallowUpdateSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ShallowUpdateSuite))
}

func (s *ShallowUpdateSuite) TestDecodeWithLF() {
	raw := "" +
		"0035shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"0035shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NoError(err)

	plumbing.HashesSort(su.Shallows)

	s.Len(su.Unshallows, 0)
	s.Len(su.Shallows, 2)
	s.Equal([]plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}, su.Shallows)
}

func (s *ShallowUpdateSuite) TestDecode() {
	raw := "" +
		"0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0034shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NoError(err)

	plumbing.HashesSort(su.Shallows)

	s.Len(su.Unshallows, 0)
	s.Len(su.Shallows, 2)
	s.Equal([]plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}, su.Shallows)
}

func (s *ShallowUpdateSuite) TestDecodeUnshallow() {
	raw := "" +
		"0036unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0036unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NoError(err)

	plumbing.HashesSort(su.Unshallows)

	s.Len(su.Shallows, 0)
	s.Len(su.Unshallows, 2)
	s.Equal([]plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}, su.Unshallows)
}

func (s *ShallowUpdateSuite) TestDecodeMalformed() {
	raw := "" +
		"0035unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NotNil(err)
}

func (s *ShallowUpdateSuite) TestDecodeNotHex() {
	for _, raw := range []string{
		"0034shallow " + strings.Repeat("z", 40) + "0000",
		"0036unshallow " + strings.Repeat("z", 40) + "0000",
		"004cshallow " + strings.Repeat("z", 64) + "0000",
		"004eunshallow " + strings.Repeat("z", 64) + "0000",
	} {
		su := &ShallowUpdate{}
		s.Error(su.Decode(bytes.NewBufferString(raw)), raw)
	}
}

func (s *ShallowUpdateSuite) TestEncodeEmpty() {
	su := &ShallowUpdate{}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))
	s.Equal("0000", buf.String())
}

func (s *ShallowUpdateSuite) TestEncode() {
	su := &ShallowUpdate{
		Shallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
		Unshallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))

	expected := "" +
		"0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0034shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0036unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0036unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	s.Equal(expected, buf.String())
}

func (s *ShallowUpdateSuite) TestEncodeShallow() {
	su := &ShallowUpdate{
		Shallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))

	expected := "" +
		"0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0034shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	s.Equal(expected, buf.String())
}

func (s *ShallowUpdateSuite) TestEncodeUnshallow() {
	su := &ShallowUpdate{
		Unshallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))

	expected := "" +
		"0036unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0036unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	s.Equal(expected, buf.String())
}

func (s *ShallowUpdateSuite) TestDecodeSHA256() {
	shallow := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unshallow := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	raw := "" +
		"004cshallow " + shallow +
		"004funshallow " + unshallow + "\n" +
		"0000"

	su := &ShallowUpdate{}
	s.Require().NoError(su.Decode(bytes.NewBufferString(raw)))

	s.Equal([]plumbing.Hash{plumbing.NewHash(shallow)}, su.Shallows)
	s.Equal([]plumbing.Hash{plumbing.NewHash(unshallow)}, su.Unshallows)
}

// TestShallowUpdateDecodeLineLimit mutates the package-wide maxSectionLines,
// so it cannot run in parallel with other tests that read it.
//
//nolint:paralleltest // mutates the shared maxSectionLines cap
func TestShallowUpdateDecodeLineLimit(t *testing.T) {
	old := maxSectionLines
	maxSectionLines = 3
	defer func() { maxSectionLines = old }()

	tests := []struct {
		name       string
		shallows   int
		unshallows int
		wantErr    string
	}{
		{name: "both lists at limit", shallows: maxSectionLines, unshallows: maxSectionLines},
		{name: "shallow over limit", shallows: maxSectionLines + 1, wantErr: "too many shallow lines (limit 3)"},
		{name: "unshallow over limit", unshallows: maxSectionLines + 1, wantErr: "too many unshallow lines (limit 3)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			for range tc.shallows {
				_, err := pktline.WriteString(&buf, "shallow 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
				require.NoError(t, err)
			}
			for range tc.unshallows {
				_, err := pktline.WriteString(&buf, "unshallow 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
				require.NoError(t, err)
			}
			require.NoError(t, pktline.WriteFlush(&buf))

			var su ShallowUpdate
			err := su.Decode(&buf)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, su.Shallows, tc.shallows)
			require.Len(t, su.Unshallows, tc.unshallows)
		})
	}
}
