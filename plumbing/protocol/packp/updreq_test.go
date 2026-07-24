package packp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

// A zero object id decoded from the wire carries the negotiated object
// format. On a sha256 repository (or from a client that sends a 64-hex
// id) the deletion/creation marker is a 64-hex zero, which does not
// compare equal to the format-unset plumbing.ZeroHash. Action must key
// off the bytes so create/delete/invalid are classified correctly.
func TestCommandActionZeroHashObjectFormat(t *testing.T) {
	t.Parallel()

	sha1Zero := plumbing.NewHash("0000000000000000000000000000000000000000")
	sha256Zero := plumbing.NewHash("0000000000000000000000000000000000000000000000000000000000000000")
	sha1Ref := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	sha256Ref := plumbing.NewHash("2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae")

	require.False(t, sha256Zero == plumbing.ZeroHash, "precondition: 64-hex zero is not == ZeroHash")
	require.True(t, sha256Zero.IsZero(), "precondition: 64-hex zero IsZero")

	for _, tc := range []struct {
		name string
		cmd  *Command
		want Action
	}{
		{"sha1 create", &Command{Name: "refs/heads/x", Old: sha1Zero, New: sha1Ref}, Create},
		{"sha1 delete", &Command{Name: "refs/heads/x", Old: sha1Ref, New: sha1Zero}, Delete},
		{"sha1 invalid", &Command{Name: "refs/heads/x", Old: sha1Zero, New: sha1Zero}, Invalid},
		{"sha256 create", &Command{Name: "refs/heads/x", Old: sha256Zero, New: sha256Ref}, Create},
		{"sha256 delete", &Command{Name: "refs/heads/x", Old: sha256Ref, New: sha256Zero}, Delete},
		{"sha256 invalid", &Command{Name: "refs/heads/x", Old: sha256Zero, New: sha256Zero}, Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.cmd.Action())
		})
	}

	// The both-zero command is malformed and validate must reject it even
	// when the ids arrive 64-hex encoded.
	require.Error(t, (&Command{Name: "refs/heads/x", Old: sha256Zero, New: sha256Zero}).validate())
}
