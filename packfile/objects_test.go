package packfile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSignature(t *testing.T) {
	cases := map[string]Signature{
		`Foo Bar <foo@bar.com> 1257894000 +0100`: {
			Name:  "Foo Bar",
			Email: "foo@bar.com",
			When:  time.Unix(1257894000, 0),
		},
		`Foo Bar <> 1257894000 +0100`: {
			Name:  "Foo Bar",
			Email: "",
			When:  time.Unix(1257894000, 0),
		},
		` <> 1257894000`: {
			Name:  "",
			Email: "",
			When:  time.Unix(1257894000, 0),
		},
		`Foo Bar <foo@bar.com>`: {
			Name:  "Foo Bar",
			Email: "foo@bar.com",
			When:  time.Time{},
		},
		``: {
			Name:  "",
			Email: "",
			When:  time.Time{},
		},
		`<`: {
			Name:  "",
			Email: "",
			When:  time.Time{},
		},
	}

	for raw, exp := range cases {
		got := NewSignature([]byte(raw))
		assert.Equal(t, exp.Name, got.Name)
		assert.Equal(t, exp.Email, got.Email)
		assert.Equal(t, exp.When.Unix(), got.When.Unix())
	}
}
