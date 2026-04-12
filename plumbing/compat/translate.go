package compat

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Translator converts objects between native and compat hash formats,
// computing the compat-format hash and recording the mapping.
//
// Objects must be translated in topological order: blobs first, then
// trees, then commits and tags. Each object's referenced objects must
// already have mappings recorded before the object itself is translated.
type Translator struct {
	formats      Formats
	nativeHasher *plumbing.ObjectHasher
	compatHasher *plumbing.ObjectHasher
	mapping      HashMapping
}

// NewTranslator creates a Translator for the given format pair and mapping.
func NewTranslator(f Formats, m HashMapping) *Translator {
	return &Translator{
		formats:      f,
		nativeHasher: plumbing.FromObjectFormat(f.Native),
		compatHasher: plumbing.FromObjectFormat(f.Compat),
		mapping:      m,
	}
}

// Mapping returns the underlying HashMapping.
func (t *Translator) Mapping() HashMapping {
	return t.mapping
}

// TranslateObject computes the compat-format hash for an object stored in
// native format. It translates internal hash references (in trees, commits,
// and tags) using the mapping, then hashes the translated content with the
// compat hasher. The resulting mapping is recorded.
//
// For blobs, content is identical across formats; only the hash differs.
func (t *Translator) TranslateObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	reader, err := obj.Reader()
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("read object: %w", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("read object content: %w", err)
	}

	var compatContent []byte

	switch obj.Type() {
	case plumbing.BlobObject:
		// Blob content is identical in both formats.
		compatContent = content

	case plumbing.TreeObject:
		compatContent, err = t.translateTree(content)
		if err != nil {
			return plumbing.Hash{}, fmt.Errorf("translate tree: %w", err)
		}

	case plumbing.CommitObject:
		compatContent, err = t.translateCommit(content)
		if err != nil {
			return plumbing.Hash{}, fmt.Errorf("translate commit: %w", err)
		}

	case plumbing.TagObject:
		compatContent, err = t.translateTag(content)
		if err != nil {
			return plumbing.Hash{}, fmt.Errorf("translate tag: %w", err)
		}

	default:
		return plumbing.Hash{}, fmt.Errorf("unsupported object type: %s", obj.Type())
	}

	compatHash, err := t.compatHasher.Compute(obj.Type(), compatContent)
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("compute compat hash: %w", err)
	}

	if err := t.mapping.Add(obj.Hash(), compatHash); err != nil {
		return plumbing.Hash{}, fmt.Errorf("record mapping: %w", err)
	}

	return compatHash, nil
}

// translateTree rewrites binary hashes in tree entries from native to compat format.
// Tree entry format: <mode-octal> <name>\0<binary-hash>
func (t *Translator) translateTree(content []byte) ([]byte, error) {
	nativeSize := t.formats.Native.Size()
	compatSize := t.formats.Compat.Size()

	var out bytes.Buffer
	buf := content

	for len(buf) > 0 {
		// Find the null byte separating "mode name" from the binary hash.
		nullIdx := bytes.IndexByte(buf, 0)
		if nullIdx < 0 {
			return nil, fmt.Errorf("malformed tree entry: missing null byte")
		}

		// Copy everything up to and including the null byte.
		out.Write(buf[:nullIdx+1])
		buf = buf[nullIdx+1:]

		if len(buf) < nativeSize {
			return nil, fmt.Errorf("malformed tree entry: truncated hash (have %d, want %d)", len(buf), nativeSize)
		}

		// Read the native-format binary hash.
		nativeHash, _ := plumbing.FromBytes(buf[:nativeSize])
		buf = buf[nativeSize:]

		// Look up the compat hash.
		compatHash, err := t.mapping.NativeToCompat(nativeHash)
		if err != nil {
			return nil, fmt.Errorf("tree entry hash %s: no compat mapping: %w", nativeHash, err)
		}

		// Write the compat-format binary hash.
		out.Write(compatHash.Bytes()[:compatSize])
	}

	return out.Bytes(), nil
}

// translateCompatTree rewrites binary hashes in tree entries from compat to
// native format.
func (t *Translator) translateCompatTree(content []byte) ([]byte, error) {
	compatSize := t.formats.Compat.Size()
	nativeSize := t.formats.Native.Size()

	var out bytes.Buffer
	buf := content

	for len(buf) > 0 {
		nullIdx := bytes.IndexByte(buf, 0)
		if nullIdx < 0 {
			return nil, fmt.Errorf("malformed tree entry: missing null byte")
		}

		out.Write(buf[:nullIdx+1])
		buf = buf[nullIdx+1:]

		if len(buf) < compatSize {
			return nil, fmt.Errorf("malformed tree entry: truncated hash (have %d, want %d)", len(buf), compatSize)
		}

		compatHash, _ := plumbing.FromBytes(buf[:compatSize])
		buf = buf[compatSize:]

		nativeHash, err := t.mapping.CompatToNative(compatHash)
		if err != nil {
			return nil, fmt.Errorf("tree entry hash %s: no native mapping: %w", compatHash, err)
		}

		out.Write(nativeHash.Bytes()[:nativeSize])
	}

	return out.Bytes(), nil
}

// translateCommit rewrites hex hashes on "tree" and "parent" lines.
func (t *Translator) translateCommit(content []byte) ([]byte, error) {
	return t.translateTextObject(content, []string{"tree", "parent"})
}

// translateTag rewrites the hex hash on the "object" line.
func (t *Translator) translateTag(content []byte) ([]byte, error) {
	return t.translateTextObject(content, []string{"object"})
}

// translateTextObject rewrites hex hashes on specified header lines.
// It processes lines until it hits an empty line (the header/body separator).
func (t *Translator) translateTextObject(content []byte, hashFields []string) ([]byte, error) {
	nativeHexSize := t.formats.Native.HexSize()

	var out bytes.Buffer
	remaining := content
	headerDone := false

	for len(remaining) > 0 {
		nlIdx := bytes.IndexByte(remaining, '\n')
		var line []byte
		if nlIdx >= 0 {
			line = remaining[:nlIdx]
			remaining = remaining[nlIdx+1:]
		} else {
			line = remaining
			remaining = nil
		}

		if !headerDone {
			if len(line) == 0 {
				// Empty line = end of header.
				headerDone = true
				out.WriteByte('\n')
				continue
			}

			replaced := false
			for _, field := range hashFields {
				prefix := field + " "
				if bytes.HasPrefix(line, []byte(prefix)) && len(line) == len(prefix)+nativeHexSize {
					hexStr := string(line[len(prefix):])
					nativeHash, ok := plumbing.FromHex(hexStr)
					if !ok {
						return nil, fmt.Errorf("invalid hash on %s line: %q", field, hexStr)
					}

					compatHash, err := t.mapping.NativeToCompat(nativeHash)
					if err != nil {
						return nil, fmt.Errorf("%s hash %s: no compat mapping: %w", field, nativeHash, err)
					}

					out.WriteString(prefix)
					out.WriteString(compatHash.String()[:t.formats.Compat.HexSize()])
					out.WriteByte('\n')
					replaced = true
					break
				}
			}

			if !replaced {
				out.Write(line)
				out.WriteByte('\n')
			}
		} else {
			// Body: copy verbatim.
			out.Write(line)
			if nlIdx >= 0 {
				out.WriteByte('\n')
			}
		}
	}

	return out.Bytes(), nil
}

// translateCompatTextObject rewrites compat-format hashes on specified header
// lines back into native format.
func (t *Translator) translateCompatTextObject(content []byte, hashFields []string) ([]byte, error) {
	compatHexSize := t.formats.Compat.HexSize()

	var out bytes.Buffer
	remaining := content
	headerDone := false

	for len(remaining) > 0 {
		nlIdx := bytes.IndexByte(remaining, '\n')
		var line []byte
		if nlIdx >= 0 {
			line = remaining[:nlIdx]
			remaining = remaining[nlIdx+1:]
		} else {
			line = remaining
			remaining = nil
		}

		if !headerDone {
			if len(line) == 0 {
				headerDone = true
				out.WriteByte('\n')
				continue
			}

			replaced := false
			for _, field := range hashFields {
				prefix := field + " "
				if bytes.HasPrefix(line, []byte(prefix)) && len(line) == len(prefix)+compatHexSize {
					hexStr := string(line[len(prefix):])
					compatHash, ok := plumbing.FromHex(hexStr)
					if !ok {
						return nil, fmt.Errorf("invalid hash on %s line: %q", field, hexStr)
					}

					nativeHash, err := t.mapping.CompatToNative(compatHash)
					if err != nil {
						return nil, fmt.Errorf("%s hash %s: no native mapping: %w", field, compatHash, err)
					}

					out.WriteString(prefix)
					out.WriteString(nativeHash.String()[:t.formats.Native.HexSize()])
					out.WriteByte('\n')
					replaced = true
					break
				}
			}

			if !replaced {
				out.Write(line)
				out.WriteByte('\n')
			}
		} else {
			out.Write(line)
			if nlIdx >= 0 {
				out.WriteByte('\n')
			}
		}
	}

	return out.Bytes(), nil
}

// ReverseTranslateContent takes object content in native format and returns
// it in compat format. This is the inverse of what TranslateObject does
// internally -- it rewrites hash references from native to compat format.
//
// This is needed for push operations where objects must be sent in the
// compat format to a server that uses that format.
func (t *Translator) ReverseTranslateContent(objType plumbing.ObjectType, nativeContent []byte) ([]byte, error) {
	switch objType {
	case plumbing.BlobObject:
		return nativeContent, nil
	case plumbing.TreeObject:
		return t.translateTree(nativeContent)
	case plumbing.CommitObject:
		return t.translateCommit(nativeContent)
	case plumbing.TagObject:
		return t.translateTag(nativeContent)
	default:
		return nil, fmt.Errorf("unsupported object type: %s", objType)
	}
}

// TranslateCompatContent takes object content in compat format and rewrites it
// back into native format. This is used when importing objects from a compat
// remote during fetch.
func (t *Translator) TranslateCompatContent(objType plumbing.ObjectType, compatContent []byte) ([]byte, error) {
	switch objType {
	case plumbing.BlobObject:
		return compatContent, nil
	case plumbing.TreeObject:
		return t.translateCompatTree(compatContent)
	case plumbing.CommitObject:
		return t.translateCompatTextObject(compatContent, []string{"tree", "parent"})
	case plumbing.TagObject:
		return t.translateCompatTextObject(compatContent, []string{"object"})
	default:
		return nil, fmt.Errorf("unsupported object type: %s", objType)
	}
}

// ComputeNativeHash computes the native-format hash for raw content.
// This is a convenience method for callers that need to hash content
// that is already in native format.
func (t *Translator) ComputeNativeHash(objType plumbing.ObjectType, content []byte) (plumbing.Hash, error) {
	return t.nativeHasher.Compute(objType, content)
}

// ComputeCompatHash computes the compat-format hash for raw content.
func (t *Translator) ComputeCompatHash(objType plumbing.ObjectType, content []byte) (plumbing.Hash, error) {
	return t.compatHasher.Compute(objType, content)
}

// NativeObjectFormat returns the native object format.
func (t *Translator) NativeObjectFormat() format.ObjectFormat {
	return t.formats.Native
}

// CompatObjectFormat returns the compat object format.
func (t *Translator) CompatObjectFormat() format.ObjectFormat {
	return t.formats.Compat
}
