package pktline_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

func BenchmarkScanner(b *testing.B) {
	sections, err := sectionsExample(2, 4)
	if err != nil {
		b.Fatal(err)
	}

	var maxp bytes.Buffer
	if _, err := pktline.WriteString(&maxp, strings.Repeat("a", pktline.MaxPayloadSize)); err != nil {
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
		{
			name:  "max packet size",
			input: maxp.String(),
		},
	}
	for _, tc := range cases {
		r := strings.NewReader("")
		s := pktline.NewScanner(r)
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				r.Reset(tc.input)
				for s.Scan() {
					if err := s.Err(); err != nil && err != io.EOF {
						b.Error(err)
					}
				}
			}
		})
	}
}

func BenchmarkReadPacket(b *testing.B) {
	sections, err := sectionsExample(2, 4)
	if err != nil {
		b.Fatal(err)
	}

	var maxp bytes.Buffer
	if _, err := pktline.WriteString(&maxp, strings.Repeat("a", pktline.MaxPayloadSize)); err != nil {
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
		{
			name:  "max packet size",
			input: maxp.String(),
		},
	}
	for _, tc := range cases {
		r := strings.NewReader("")
		b.Run(tc.name, func(b *testing.B) {
			buf := pktline.GetPacketBuffer()
			for i := 0; i < b.N; i++ {
				r.Reset(tc.input)
				for {
					_, err := pktline.Read(r, (*buf)[:])
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Error(err)
					}
				}
			}
			pktline.PutPacketBuffer(buf)
		})
	}
}

func BenchmarkReadPacketLine(b *testing.B) {
	sections, err := sectionsExample(2, 4)
	if err != nil {
		b.Fatal(err)
	}

	var maxp bytes.Buffer
	if _, err := pktline.WriteString(&maxp, strings.Repeat("a", pktline.MaxPayloadSize)); err != nil {
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
		{
			name:  "max packet size",
			input: maxp.String(),
		},
	}
	for _, tc := range cases {
		r := strings.NewReader("")
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				r.Reset(tc.input)
				for {
					_, _, err := pktline.ReadLine(r)
					if err == io.EOF {
						break
					}
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
				_, err := pktline.Write(&buf, tc.input)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
