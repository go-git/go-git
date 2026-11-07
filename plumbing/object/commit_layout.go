package object

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
)

// commitHeader connects a raw header (including continuation lines) to its
// decoded field. An empty key denotes an ignored duplicate or misplaced
// standard header, which is retained without changing the decoded fields.
type commitHeader struct {
	key   string
	index int
	raw   string
}

type commitLayout struct {
	headers         []commitHeader
	separator       bool
	tree            plumbing.Hash
	parents         []plumbing.Hash
	encoding        MessageEncoding
	extras          []ExtraHeader
	signature       string
	signatureSHA256 string
}

func (l *commitLayout) remember(c *Commit) {
	l.tree = c.TreeHash
	l.parents = slices.Clone(c.ParentHashes)
	l.encoding = c.Encoding
	l.extras = slices.Clone(c.ExtraHeaders)
	l.signature = c.Signature
	l.signatureSHA256 = c.SignatureSHA256
}

func (c *Commit) encodeLayout(w io.Writer, includeSig bool) error {
	l := c.layout
	parentsChanged := !slices.Equal(c.ParentHashes, l.parents)
	seen := make(map[string]bool)
	var headers bytes.Buffer
	for _, h := range l.headers {
		raw := h.raw
		switch h.key {
		case "tree":
			if c.TreeHash != l.tree {
				raw = fmt.Sprintf("tree %s\n", c.TreeHash)
			}
		case "parent":
			if parentsChanged {
				continue
			}
		case "author":
			if !signatureEqual(c.Author, c.authorSource.signature) {
				raw = commitIdentHeader(h.key, c.Author)
			}
		case "committer":
			if !signatureEqual(c.Committer, c.committerSource.signature) {
				raw = commitIdentHeader(h.key, c.Committer)
			}
		case headerencoding:
			if c.Encoding != l.encoding {
				raw = ""
				if c.Encoding != "" {
					raw = fmt.Sprintf("encoding %s\n", c.Encoding)
				}
			}
		case headerpgp, headerpgp256:
			// A header without the separating space is not a signature
			// header according to Git's signature-stripping rules.
			if !isSignatureHeader([]byte(raw)) {
				break
			}
			if !includeSig {
				continue
			}
			current, original := c.Signature, l.signature
			if h.key == headerpgp256 {
				current, original = c.SignatureSHA256, l.signatureSHA256
			}
			if current != original {
				raw = ""
				if !seen[h.key] && current != "" {
					raw = commitSignatureHeader(h.key, current)
				}
			}
		case "extra":
			raw = ""
			if h.index < len(c.ExtraHeaders) {
				extra := c.ExtraHeaders[h.index]
				if !isStandardHeader(extra.Key) {
					if extra == l.extras[h.index] {
						raw = h.raw
					} else {
						raw = fmt.Sprintf("%s\n", extra)
					}
				}
			}
		}
		appendCommitHeader(&headers, raw)
		seen[h.key] = true
		if h.key == "tree" && parentsChanged {
			for _, parent := range c.ParentHashes {
				appendCommitHeader(&headers, fmt.Sprintf("parent %s\n", parent))
			}
		}
	}

	// New fields have no source position. Append them in canonical order;
	// existing fields keep their original slots even when their values change.
	if !seen["author"] && !signatureEqual(c.Author, Signature{}) {
		appendCommitHeader(&headers, commitIdentHeader("author", c.Author))
	}
	if !seen["committer"] && !signatureEqual(c.Committer, Signature{}) {
		appendCommitHeader(&headers, commitIdentHeader("committer", c.Committer))
	}
	if !seen[headerencoding] && c.Encoding != "" && c.Encoding != defaultUtf8CommitMessageEncoding {
		appendCommitHeader(&headers, fmt.Sprintf("encoding %s\n", c.Encoding))
	}
	for i := len(l.extras); i < len(c.ExtraHeaders); i++ {
		if extra := c.ExtraHeaders[i]; !isStandardHeader(extra.Key) {
			appendCommitHeader(&headers, fmt.Sprintf("%s\n", extra))
		}
	}
	if includeSig {
		if !seen[headerpgp] && c.Signature != "" {
			appendCommitHeader(&headers, commitSignatureHeader(headerpgp, c.Signature))
		}
		if !seen[headerpgp256] && c.SignatureSHA256 != "" {
			appendCommitHeader(&headers, commitSignatureHeader(headerpgp256, c.SignatureSHA256))
		}
	}
	if l.separator || c.Message != "" {
		appendCommitHeader(&headers, "\n")
	}
	if _, err := w.Write(headers.Bytes()); err != nil {
		return err
	}
	_, err := io.WriteString(w, c.Message)
	return err
}

// appendCommitHeader separates newly added headers from an unterminated
// source header while preserving a final missing newline on a round trip.
func appendCommitHeader(dst *bytes.Buffer, raw string) {
	if raw == "" {
		return
	}
	if b := dst.Bytes(); len(b) > 0 && b[len(b)-1] != '\n' {
		dst.WriteByte('\n')
	}
	dst.WriteString(raw)
}

func commitIdentHeader(key string, sig Signature) string {
	var value bytes.Buffer
	// bytes.Buffer.Write always succeeds.
	_ = sig.Encode(&value)
	return key + " " + value.String() + "\n"
}

func commitSignatureHeader(key, value string) string {
	return key + " " + strings.ReplaceAll(strings.TrimSuffix(value, "\n"), "\n", "\n ") + "\n"
}
