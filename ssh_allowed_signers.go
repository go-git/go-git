package git

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ParseAllowedSignersFile parses an SSH allowed signers file from the given path.
// The path can use "~/" prefix to refer to the user's home directory.
// Returns a map of principal names to their public keys.
//
// The allowed signers file format follows Git's gpg.ssh.allowedSignersFile:
//   - Each line contains: principal(s) key-type base64-key [comment]
//   - Multiple principals can be comma-separated
//   - Lines starting with # are comments
//   - Empty lines are ignored
//   - Optional fields like namespaces=, valid-after=, valid-before= are ignored
//   - Wildcard principal "*" matches any signer
func ParseAllowedSignersFile(path string) (map[string]ssh.PublicKey, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open allowed signers file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	return ParseAllowedSigners(file)
}

// ParseAllowedSigners parses an SSH allowed signers file from an io.Reader.
// Returns a map of principal names to their public keys.
//
// The allowed signers file format follows Git's gpg.ssh.allowedSignersFile:
//   - Each line contains: principal(s) key-type base64-key [comment]
//   - Multiple principals can be comma-separated
//   - Lines starting with # are comments
//   - Empty lines are ignored
//   - Optional fields like namespaces=, valid-after=, valid-before= are ignored
//   - Wildcard principal "*" matches any signer
func ParseAllowedSigners(r io.Reader) (map[string]ssh.PublicKey, error) {
	const maxLineSize = 65536 // 64KB
	allowedSigners := make(map[string]ssh.PublicKey)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, maxLineSize), maxLineSize)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse the line
		if err := parseAllowedSignersLine(line, allowedSigners); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("line %d: failed to read: %w", lineNum+1, err)
	}

	return allowedSigners, nil
}

// parseAllowedSignersLine parses a single line from the allowed signers file.
// Format: principal(s) [options] key-type base64-key [comment]
func parseAllowedSignersLine(line string, allowedSigners map[string]ssh.PublicKey) error {
	// Split by whitespace
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return fmt.Errorf("invalid format: expected at least principal and public key")
	}

	principals := fields[0]

	// Find where the actual SSH public key starts
	// Options like "namespaces=", "valid-after=", "valid-before=", "cert-authority" come before the key type
	keyStartIdx := 1
	for keyStartIdx < len(fields) && isAllowedSignersOption(fields[keyStartIdx]) {
		keyStartIdx++
	}

	if keyStartIdx >= len(fields) {
		return fmt.Errorf("invalid format: no public key found")
	}

	// The remaining fields form the SSH public key (key-type base64-key comment)
	// We join them and let ssh.ParseAuthorizedKey handle the parsing
	keyLine := strings.Join(fields[keyStartIdx:], " ")

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine))
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}

	// Split principals by comma and add each one
	for principal := range strings.SplitSeq(principals, ",") {
		principal = strings.TrimSpace(principal)
		if principal == "" {
			continue
		}
		// Check for duplicate principals
		if _, exists := allowedSigners[principal]; exists {
			return fmt.Errorf("duplicate principal %q", principal)
		}
		allowedSigners[principal] = pubKey
	}

	return nil
}

// isAllowedSignersOption checks if a field is a known allowed signers option.
func isAllowedSignersOption(field string) bool {
	return strings.HasPrefix(field, "namespaces=") ||
		strings.HasPrefix(field, "valid-after=") ||
		strings.HasPrefix(field, "valid-before=") ||
		field == "cert-authority"
}
