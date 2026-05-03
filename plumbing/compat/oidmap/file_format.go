package oidmap

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

func objectFormatFromHash(h plumbing.Hash) (formatcfg.ObjectFormat, error) {
	switch h.Size() {
	case formatcfg.SHA1.Size():
		return formatcfg.SHA1, nil
	case formatcfg.SHA256.Size():
		return formatcfg.SHA256, nil
	default:
		return "", fmt.Errorf("unsupported hash length")
	}
}

func objectFormatFromID(id string) (formatcfg.ObjectFormat, error) {
	switch id {
	case "sha1":
		return formatcfg.SHA1, nil
	case "s256":
		return formatcfg.SHA256, nil
	case "sha256":
		return formatcfg.SHA256, nil
	default:
		return "", fmt.Errorf("unsupported format id %q", id)
	}
}

func formatID(of formatcfg.ObjectFormat) []byte {
	switch of {
	case formatcfg.SHA1:
		return []byte("sha1")
	case formatcfg.SHA256:
		return []byte("s256")
	default:
		return []byte("unkn")
	}
}

func checksumForFormat(of formatcfg.ObjectFormat, data []byte) ([]byte, error) {
	switch of {
	case formatcfg.SHA1:
		sum := sha1.Sum(data)
		return sum[:], nil
	case formatcfg.SHA256:
		sum := sha256.Sum256(data)
		return sum[:], nil
	default:
		return nil, fmt.Errorf("unsupported checksum format %q", of)
	}
}

func hexFromBytes(b []byte) string {
	return hex.EncodeToString(b)
}
