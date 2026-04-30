package compat

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat/oidmap"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Translator converts objects between native and compat hash formats,
// computing the compat-format hash and recording the mapping.
//
// Objects must be translated in topological order: blobs first, then
// trees, then commits and tags. Each object's referenced objects must
// already have mappings recorded before the object itself is translated.
type Translator struct {
	nativeHasher *plumbing.ObjectHasher
	compatHasher *plumbing.ObjectHasher
	mapping      oidmap.Map
}

// NewTranslator creates a Translator for the given format pair and mapping.
func NewTranslator(native, compat format.ObjectFormat, m oidmap.Map) *Translator {
	return &Translator{
		nativeHasher: plumbing.FromObjectFormat(native),
		compatHasher: plumbing.FromObjectFormat(compat),
		mapping:      m,
	}
}

// Mapping returns the underlying object ID map.
func (t *Translator) Mapping() oidmap.Map {
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

	content, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		return plumbing.Hash{}, fmt.Errorf("read object content: %w", err)
	}
	if err := reader.Close(); err != nil {
		return plumbing.Hash{}, fmt.Errorf("close object reader: %w", err)
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
	return t.translateTreeContent(
		content,
		t.nativeHasher.Size(),
		t.compatHasher.Size(),
		"compat",
		t.mapping.ToCompat,
	)
}

// translateCompatTree rewrites binary hashes in tree entries from compat to
// native format.
func (t *Translator) translateCompatTree(content []byte) ([]byte, error) {
	return t.translateTreeContent(
		content,
		t.compatHasher.Size(),
		t.nativeHasher.Size(),
		"native",
		t.mapping.ToNative,
	)
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
	return t.translateTextObjectContent(
		content,
		hashFields,
		t.nativeHasher.Size()*2,
		t.compatHasher.Size()*2,
		"compat",
		t.mapping.ToCompat,
	)
}

// translateCompatTextObject rewrites compat-format hashes on specified header
// lines back into native format.
func (t *Translator) translateCompatTextObject(content []byte, hashFields []string) ([]byte, error) {
	return t.translateTextObjectContent(
		content,
		hashFields,
		t.compatHasher.Size()*2,
		t.nativeHasher.Size()*2,
		"native",
		t.mapping.ToNative,
	)
}

func (t *Translator) translateTreeContent(
	content []byte,
	fromSize, toSize int,
	target string,
	lookup func(plumbing.Hash) (plumbing.Hash, error),
) ([]byte, error) {
	var out bytes.Buffer
	buf := content

	for len(buf) > 0 {
		nullIdx := bytes.IndexByte(buf, 0)
		if nullIdx < 0 {
			return nil, fmt.Errorf("malformed tree entry: missing null byte")
		}

		out.Write(buf[:nullIdx+1])
		buf = buf[nullIdx+1:]

		if len(buf) < fromSize {
			return nil, fmt.Errorf("malformed tree entry: truncated hash (have %d, want %d)", len(buf), fromSize)
		}

		fromHash, _ := plumbing.FromBytes(buf[:fromSize])
		buf = buf[fromSize:]

		toHash, err := lookup(fromHash)
		if err != nil {
			return nil, fmt.Errorf("tree entry hash %s: no %s mapping: %w", fromHash, target, err)
		}

		out.Write(toHash.Bytes()[:toSize])
	}

	return out.Bytes(), nil
}

func (t *Translator) translateTextObjectContent(
	content []byte,
	hashFields []string,
	fromHexSize, toHexSize int,
	target string,
	lookup func(plumbing.Hash) (plumbing.Hash, error),
) ([]byte, error) {
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

		if headerDone {
			out.Write(line)
			if nlIdx >= 0 {
				out.WriteByte('\n')
			}
			continue
		}

		if len(line) == 0 {
			headerDone = true
			out.WriteByte('\n')
			continue
		}

		replaced := false
		for _, field := range hashFields {
			prefix := field + " "
			if !bytes.HasPrefix(line, []byte(prefix)) || len(line) != len(prefix)+fromHexSize {
				continue
			}

			fromHash, ok := plumbing.FromHex(string(line[len(prefix):]))
			if !ok {
				return nil, fmt.Errorf("invalid hash on %s line: %q", field, line[len(prefix):])
			}

			toHash, err := lookup(fromHash)
			if err != nil {
				return nil, fmt.Errorf("%s hash %s: no %s mapping: %w", field, fromHash, target, err)
			}

			out.WriteString(prefix)
			out.WriteString(toHash.String()[:toHexSize])
			out.WriteByte('\n')
			replaced = true
			break
		}

		if !replaced {
			out.Write(line)
			out.WriteByte('\n')
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

func objectFormatFromHasher(h *plumbing.ObjectHasher) format.ObjectFormat {
	switch h.Size() {
	case format.SHA256Size:
		return format.SHA256
	default:
		return format.SHA1
	}
}

// NativeObjectFormat returns the native object format.
func (t *Translator) NativeObjectFormat() format.ObjectFormat {
	return objectFormatFromHasher(t.nativeHasher)
}

// CompatObjectFormat returns the compat object format.
func (t *Translator) CompatObjectFormat() format.ObjectFormat {
	return objectFormatFromHasher(t.compatHasher)
}
