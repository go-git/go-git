package pktline_test

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

func BenchmarkEncoder(b *testing.B) {
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
				e := pktline.NewEncoder(&buf)
				err := e.Encode(tc.input)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWriter(b *testing.B) {
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
				e := pktline.NewWriter(&buf)
				_, err := e.WritePacket(tc.input)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
