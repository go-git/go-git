//go:build sha256
// +build sha256

package hash

import "crypto"

const (
	// CryptoType defines what hash algorithm is being used.
	CryptoType = crypto.SHA256
	// Size defines the amount of bytes the hash yields.
	Size = SHA256Size
	// HexSize defines the strings size of the hash when represented in hexadecimal.
	HexSize = SHA256HexSize
)
