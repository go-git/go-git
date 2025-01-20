package packp

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSmartReply_Decode(t *testing.T) {
	var s SmartReply
	r := strings.NewReader("001e# service=git-upload-pack\n0000")
	err := s.Decode(r)
	require.NoError(t, err)
	require.Equal(t, "git-upload-pack", s.Service)
}

func TestSmartReply_Decode_Error(t *testing.T) {
	var s SmartReply
	r := strings.NewReader("0000")
	err := s.Decode(r)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidSmartReply))
}

func TestSmartReply_Decode_Error_Flush(t *testing.T) {
	var s SmartReply
	r := strings.NewReader("001e# service=git-upload-pack\n")
	err := s.Decode(r)
	require.Error(t, err)
}

func TestSmartReply_Encode(t *testing.T) {
	var s SmartReply
	s.Service = "git-upload-pack"
	var buf bytes.Buffer
	err := s.Encode(&buf)
	require.NoError(t, err)
	require.Equal(t, "001e# service=git-upload-pack\n0000", buf.String())
}
