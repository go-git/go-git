package pktline_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

func BenchmarkReadPacket(b *testing.B) {
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
			for i := 0; i < b.N; i++ {
				for {
					_, _, err := pktline.ReadPacket(r)
					if err != nil {
						break
					}
				}
			}
		})
	}
}

func BenchmarkWritePacket(b *testing.B) {
	sections, err := sectionsExample(2, 4)
	if err != nil {
		b.Fatal(err)
	}

	cases := []struct {
		name  string
		input []byte
	}{
		{
			name:  "empty",
			input: []byte(""),
		},
		{
			name:  "one message",
			input: []byte("hello\n"),
		},
		{
			name:  "two messages",
			input: []byte("hello\nworld!\n"),
		},
		{
			name:  "sections",
			input: sections.Bytes(),
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var buf bytes.Buffer
			for i := 0; i < b.N; i++ {
				_, err := pktline.WritePacket(&buf, tc.input)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
