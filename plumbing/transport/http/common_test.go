package http

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErr_ErrorRedactsCredentials(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "https://user:s3cr3t@example.com/repo.git/info/refs?service=git-upload-pack", nil)
	resp := &http.Response{
		Request:    req,
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("boom")),
	}
	err := checkError(resp)
	require.Error(t, err)
	msg := err.Error()
	assert.NotContains(t, msg, "s3cr3t")
	assert.Contains(t, msg, "REDACTED")
	// the rest of the URL is still reported so the error stays useful
	assert.Contains(t, msg, "example.com/repo.git")
}
