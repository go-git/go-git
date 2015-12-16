package clients

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteCommon struct{}

var _ = Suite(&SuiteCommon{})

func (s *SuiteCommon) TestNewGitUploadPackService(c *C) {
	var tests = [...]struct {
		input    string
		err      bool
		expected string
	}{
		{"ht/ml://example.com", true, "<nil>"},
		{"", true, "<nil>"},
		{"-", true, "<nil>"},
		{"!@", true, "<nil>"},
		{"badscheme://github.com/src-d/go-git", true, "<nil>"},
		{"http://github.com/src-d/go-git", false, "*http.GitUploadPackService"},
		{"https://github.com/src-d/go-git", false, "*http.GitUploadPackService"},
		{"ssh://github.com/src-d/go-git", false, "*ssh.GitUploadPackService"},
		{"file://github.com/src-d/go-git", false, "*file.GitUploadPackService"},
	}

	for i, t := range tests {
		output, err := NewGitUploadPackService(t.input)
		c.Assert(err != nil, Equals, t.err, Commentf("%d) %q: wrong error value", i, t.input))
		c.Assert(fmt.Sprintf("%T", output), Equals, t.expected, Commentf("%d) %q: wrong type", i, t.input))
	}
}
