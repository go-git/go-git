// Copyright 2024 Skeema LLC and the Skeema Knownhosts authors

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Originally from: https://github.com/skeema/knownhosts/blob/v1.3.0/knownhosts_test.go
// Upstream commit: 9485bdec8ed521b32ab0e9310619effa071a99d6

package knownhosts

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	xknownhosts "golang.org/x/crypto/ssh/knownhosts"
)

func TestNewDB(t *testing.T) {
	t.Parallel()
	khPath := getTestKnownHosts(t)

	// Valid path should return a non-nil HostKeyDB and no error
	if kh, err := NewDB(khPath); kh == nil || err != nil {
		t.Errorf("Unexpected return from NewDB on valid known_hosts path: %v, %v", kh, err)
	} else {
		// Confirm return value of HostKeyCallback is an ssh.HostKeyCallback
		_ = ssh.ClientConfig{
			HostKeyCallback: kh.HostKeyCallback(),
		}
	}

	// Append a @cert-authority line to the valid known_hosts file
	// Valid path should still return a non-nil HostKeyDB and no error
	certKeys := make(map[string]ssh.PublicKey)
	appendCertTestKnownHosts(t, certKeys, khPath, "*", ssh.KeyAlgoECDSA256)
	if kh, err := NewDB(khPath); kh == nil || err != nil {
		t.Errorf("Unexpected return from NewDB on valid known_hosts path containing a cert: %v, %v", kh, err)
	}

	// Write a second valid known_hosts file
	// Supplying both valid paths should still return a non-nil HostKeyDB and no
	// error
	appendCertTestKnownHosts(t, certKeys, khPath+"2", "*.certy.test", ssh.KeyAlgoED25519)
	if kh, err := NewDB(khPath+"2", khPath); kh == nil || err != nil {
		t.Errorf("Unexpected return from NewDB on two valid known_hosts paths: %v, %v", kh, err)
	}

	// Invalid path should return an error, with or without other valid paths
	if _, err := NewDB(khPath + "_does_not_exist"); err == nil {
		t.Error("Expected error from NewDB with invalid path, but error was nil")
	}
	if _, err := NewDB(khPath, khPath+"_does_not_exist"); err == nil {
		t.Error("Expected error from NewDB with mix of valid and invalid paths, but error was nil")
	}
}

func TestHostKeys(t *testing.T) {
	t.Parallel()
	khPath := getTestKnownHosts(t)
	kh, err := NewDB(khPath)
	if err != nil {
		t.Fatalf("Unexpected error from NewDB: %v", err)
	}

	expectedKeyTypes := map[string][]string{
		"only-rsa.example.test:22":     {"ssh-rsa"},
		"only-ecdsa.example.test:22":   {"ecdsa-sha2-nistp256"},
		"only-ed25519.example.test:22": {"ssh-ed25519"},
		"multi.example.test:2233":      {"ssh-rsa", "ecdsa-sha2-nistp256", "ssh-ed25519", "ssh-ed25519"},
		"192.168.1.102:2222":           {"ecdsa-sha2-nistp256", "ssh-ed25519"},
		"unknown-host.example.test":    {}, // host not in file
		"multi.example.test:22":        {}, // different port than entry in file
		"192.168.1.102":                {}, // different port than entry in file
	}
	for host, expected := range expectedKeyTypes {
		actual := kh.hostKeys(host)
		if len(actual) != len(expected) {
			t.Errorf("Unexpected number of keys returned by hostKeys(%q): expected %d, found %d", host, len(expected), len(actual))
			continue
		}
		for n := range expected {
			if actualType := actual[n].Type(); expected[n] != actualType {
				t.Errorf("Unexpected key returned by hostKeys(%q): expected key[%d] to be type %v, found %v", host, n, expected, actualType)
				break
			}
		}
	}
}

func TestHostKeyAlgorithms(t *testing.T) {
	t.Parallel()
	khPath := getTestKnownHosts(t)
	kh, err := NewDB(khPath)
	if err != nil {
		t.Fatalf("Unexpected error from NewDB: %v", err)
	}

	expectedAlgorithms := map[string][]string{
		"only-rsa.example.test:22":     {"rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"},
		"only-ecdsa.example.test:22":   {"ecdsa-sha2-nistp256"},
		"only-ed25519.example.test:22": {"ssh-ed25519"},
		"multi.example.test:2233":      {"rsa-sha2-512", "rsa-sha2-256", "ssh-rsa", "ecdsa-sha2-nistp256", "ssh-ed25519"},
		"192.168.1.102:2222":           {"ecdsa-sha2-nistp256", "ssh-ed25519"},
		"unknown-host.example.test":    {}, // host not in file
		"multi.example.test:22":        {}, // different port than entry in file
		"192.168.1.102":                {}, // different port than entry in file
	}
	for host, expected := range expectedAlgorithms {
		actual := kh.HostKeyAlgorithms(host)
		if len(actual) != len(expected) {
			t.Errorf("Unexpected number of algorithms returned by HostKeyAlgorithms(%q): expected %d, found %d", host, len(expected), len(actual))
			continue
		}
		for n := range expected {
			if expected[n] != actual[n] {
				t.Errorf("Unexpected algorithms returned by HostKeyAlgorithms(%q): expected %v, found %v", host, expected, actual)
				break
			}
		}
	}
}

func TestWithCertLines(t *testing.T) {
	t.Parallel()
	khPath := getTestKnownHosts(t)
	khPath2 := khPath + "2"
	certKeys := make(map[string]ssh.PublicKey)
	appendCertTestKnownHosts(t, certKeys, khPath, "*.certy.test", ssh.KeyAlgoRSA)
	appendCertTestKnownHosts(t, certKeys, khPath2, "*", ssh.KeyAlgoECDSA256)
	appendCertTestKnownHosts(t, certKeys, khPath2, "*.certy.test", ssh.KeyAlgoED25519)

	// Now test behavior of HostKeyDB type, which should properly support
	// @cert-authority lines as being different from other lines
	kh, err := NewDB(khPath2, khPath)
	if err != nil {
		t.Fatalf("Unexpected error from NewDB: %v", err)
	}
	testCases := []struct {
		host             string
		expectedKeyTypes []string
		expectedIsCert   []bool
		expectedAlgos    []string
	}{
		{
			host:             "only-ed25519.example.test:22",
			expectedKeyTypes: []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256},
			expectedIsCert:   []bool{false, true},
			expectedAlgos:    []string{ssh.KeyAlgoED25519, ssh.CertAlgoECDSA256v01},
		},
		{
			host:             "only-rsa.example.test:22",
			expectedKeyTypes: []string{ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256},
			expectedIsCert:   []bool{false, true},
			expectedAlgos:    []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA, ssh.CertAlgoECDSA256v01},
		},
		{
			host:             "whatever.test:22", // only matches the * entry
			expectedKeyTypes: []string{ssh.KeyAlgoECDSA256},
			expectedIsCert:   []bool{true},
			expectedAlgos:    []string{ssh.CertAlgoECDSA256v01},
		},
		{
			host:             "whatever.test:22022", // only matches the * entry
			expectedKeyTypes: []string{ssh.KeyAlgoECDSA256},
			expectedIsCert:   []bool{true},
			expectedAlgos:    []string{ssh.CertAlgoECDSA256v01},
		},
		{
			host:             "asdf.certy.test:22",
			expectedKeyTypes: []string{ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoED25519},
			expectedIsCert:   []bool{true, true, true},
			expectedAlgos:    []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01, ssh.CertAlgoECDSA256v01, ssh.CertAlgoED25519v01},
		},
		{
			host:             "oddport.certy.test:2345",
			expectedKeyTypes: []string{ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoED25519},
			expectedIsCert:   []bool{true, true, true},
			expectedAlgos:    []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01, ssh.CertAlgoECDSA256v01, ssh.CertAlgoED25519v01},
		},
	}
	for _, tc := range testCases {
		annotatedKeys := kh.hostKeys(tc.host)
		if len(annotatedKeys) != len(tc.expectedKeyTypes) {
			t.Errorf("Unexpected return from hostKeys(%q): %v", tc.host, annotatedKeys)
		} else {
			for n := range annotatedKeys {
				if annotatedKeys[n].Type() != tc.expectedKeyTypes[n] || annotatedKeys[n].Cert != tc.expectedIsCert[n] {
					t.Errorf("Unexpected return from hostKeys(%q) at index %d: %v", tc.host, n, annotatedKeys)
					break
				}
			}
		}
		algos := kh.HostKeyAlgorithms(tc.host)
		if len(algos) != len(tc.expectedAlgos) {
			t.Errorf("Unexpected return from HostKeyAlgorithms(%q): %v", tc.host, algos)
		} else {
			for n := range algos {
				if algos[n] != tc.expectedAlgos[n] {
					t.Errorf("Unexpected return from HostKeyAlgorithms(%q) at index %d: %v", tc.host, n, algos)
					break
				}
			}
		}
	}
}

func TestIsHostKeyChanged(t *testing.T) {
	t.Parallel()
	khPath := getTestKnownHosts(t)
	db, err := NewDB(khPath)
	if err != nil {
		t.Fatalf("Unexpected error from NewDB: %v", err)
	}
	kh := db.HostKeyCallback()
	noAddr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
	pubKey := generatePubKeyEd25519(t)

	// Unknown host: should return false
	if err := kh("unknown.example.test:22", noAddr, pubKey); isHostKeyChanged(err) {
		t.Error("isHostKeyChanged unexpectedly returned true for unknown host")
	}

	// Known host, wrong key: should return true
	if err := kh("multi.example.test:2233", noAddr, pubKey); !isHostKeyChanged(err) {
		t.Error("isHostKeyChanged unexpectedly returned false for known host with different host key")
	}

	// Append the key for a known host that doesn't already have that key type,
	// re-init the known_hosts, and check again: should return false
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("Unable to open %s for writing: %v", khPath, err)
	}
	if err := writeKnownHost(f, "only-ecdsa.example.test:22", pubKey); err != nil {
		t.Fatalf("Unable to write known host line: %v", err)
	}
	f.Close()
	if db, err = NewDB(khPath); err != nil {
		t.Fatalf("Unexpected error from NewDB: %v", err)
	}
	kh = db.HostKeyCallback()
	if err := kh("only-ecdsa.example.test:22", noAddr, pubKey); isHostKeyChanged(err) {
		t.Error("isHostKeyChanged unexpectedly returned true for valid known host")
	}
}

func TestFakePublicKey(t *testing.T) {
	t.Parallel()
	fpk := fakePublicKey{}
	if err := fpk.Verify(nil, nil); err == nil {
		t.Error("Expected fakePublicKey.Verify() to always return an error, but it did not")
	}
	if certAlgo := keyTypeToCertAlgo(fpk.Type()); certAlgo != "" {
		t.Errorf("Expected keyTypeToCertAlgo on a fakePublicKey to return an empty string, but instead found %q", certAlgo)
	}
}

// getTestKnownHosts returns a path to a test known_hosts file. The file path
// will differ between test functions, but the contents are always the same.
// The file is removed upon test completion.
func getTestKnownHosts(t *testing.T) string {
	return writeTestKnownHosts(t)
}

// writeTestKnownHosts generates the test known_hosts file and returns the
// file path to it. The generated file contains several hosts with a mix of
// key types; each known host has between 1 and 4 different known host keys.
// If generating or writing the file fails, the test fails.
func writeTestKnownHosts(t *testing.T) string {
	t.Helper()
	hosts := map[string][]ssh.PublicKey{
		"only-rsa.example.test:22":     {generatePubKeyRSA(t)},
		"only-ecdsa.example.test:22":   {generatePubKeyECDSA(t)},
		"only-ed25519.example.test:22": {generatePubKeyEd25519(t)},
		"multi.example.test:2233":      {generatePubKeyRSA(t), generatePubKeyECDSA(t), generatePubKeyEd25519(t), generatePubKeyEd25519(t)},
		"192.168.1.102:2222":           {generatePubKeyECDSA(t), generatePubKeyEd25519(t)},
		"[fe80::abc:abc:abcd:abcd]:22": {generatePubKeyEd25519(t), generatePubKeyRSA(t)},
	}

	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	f, err := os.OpenFile(khPath, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("Unable to open %s for writing: %v", khPath, err)
	}
	defer f.Close()
	for host, keys := range hosts {
		for _, k := range keys {
			if err := writeKnownHost(f, host, k); err != nil {
				t.Fatalf("Unable to write known host line: %v", err)
			}
		}
	}
	return khPath
}

// appendCertTestKnownHosts adds a @cert-authority line to the file at the
// supplied path, creating it if it does not exist yet. The keyType must be one
// of ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, or ssh.KeyAlgoED25519; while all
// valid algos are supported by this package, the test logic hasn't been
// written for other algos here yet. certKeys memoizes generated keys within a
// single test to avoid redundant key generation.
func appendCertTestKnownHosts(t *testing.T, certKeys map[string]ssh.PublicKey, filePath, hostPattern, keyType string) {
	t.Helper()

	cacheKey := hostPattern + " " + keyType
	pubKey, ok := certKeys[cacheKey]
	if !ok {
		switch keyType {
		case ssh.KeyAlgoRSA:
			pubKey = generatePubKeyRSA(t)
		case ssh.KeyAlgoECDSA256:
			pubKey = generatePubKeyECDSA(t)
		case ssh.KeyAlgoED25519:
			pubKey = generatePubKeyEd25519(t)
		default:
			t.Fatalf("test logic does not support generating key of type %s yet", keyType)
		}
		certKeys[cacheKey] = pubKey
	}

	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("Unable to open %s for writing: %v", filePath, err)
	}
	defer f.Close()
	if err := writeKnownHostCA(f, hostPattern, pubKey); err != nil {
		t.Fatalf("Unable to append @cert-authority line to %s: %v", filePath, err)
	}
}

func generatePubKeyRSA(t *testing.T) ssh.PublicKey {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatalf("Unable to generate RSA key: %v", err)
	}
	pub, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("Unable to convert public key: %v", err)
	}
	return pub
}

func generatePubKeyECDSA(t *testing.T) ssh.PublicKey {
	t.Helper()
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Unable to generate ECDSA key: %v", err)
	}
	pub, err := ssh.NewPublicKey(privKey.Public())
	if err != nil {
		t.Fatalf("Unable to convert public key: %v", err)
	}
	return pub
}

func generatePubKeyEd25519(t *testing.T) ssh.PublicKey {
	t.Helper()
	rawPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("Unable to generate ed25519 key: %v", err)
	}
	pub, err := ssh.NewPublicKey(rawPub)
	if err != nil {
		t.Fatalf("Unable to convert public key: %v", err)
	}
	return pub
}

func writeKnownHost(w io.Writer, hostname string, key ssh.PublicKey) error {
	_, err := io.WriteString(w, xknownhosts.Line([]string{hostname}, key)+"\n")
	return err
}

func writeKnownHostCA(w io.Writer, hostPattern string, key ssh.PublicKey) error {
	encodedKey := base64.StdEncoding.EncodeToString(key.Marshal())
	_, err := fmt.Fprintf(w, "@cert-authority %s %s %s\n", hostPattern, key.Type(), encodedKey)
	return err
}
