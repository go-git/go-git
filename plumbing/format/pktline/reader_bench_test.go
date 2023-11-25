package pktline_test

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

func BenchmarkScanner(b *testing.B) {
	sections, err := sectionsExample(2, 4)
	if err != nil {
		b.Fatal(err)
	}

	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "empty",
			input: "",
		},
		{
			name:  "one message",
			input: "000ahello\n",
		},
		{
			name:  "two messages",
			input: "000ahello\n000bworld!\n",
		},
		{
			name:  "sections",
			input: sections.String(),
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			r := strings.NewReader(tc.input)
			sc := pktline.NewScanner(r)
			for i := 0; i < b.N; i++ {
				for sc.Scan() {
				}
			}
		})
	}
}

func BenchmarkReader(b *testing.B) {
	sections, err := sectionsExample(2, 4)
	if err != nil {
		b.Fatal(err)
	}

	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "empty",
			input: "",
		},
		{
			name:  "one message",
			input: "000ahello\n",
		},
		{
			name:  "two messages",
			input: "000ahello\n000bworld!\n",
		},
		{
			name:  "sections",
			input: sections.String(),
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			r := strings.NewReader(tc.input)
			sc := pktline.NewReader(r)
			for i := 0; i < b.N; i++ {
				for {
					_, _, err := sc.ReadPacket()
					if err != nil {
						break
					}
				}
			}
		})
	}
}
