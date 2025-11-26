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

// Originally from: https://github.com/skeema/knownhosts/blob/main/knownhosts_test.go

package knownhosts

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestNewDB(t *testing.T) {
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
	appendCertTestKnownHosts(t, khPath, "*", ssh.KeyAlgoECDSA256)
	if kh, err := NewDB(khPath); kh == nil || err != nil {
		t.Errorf("Unexpected return from NewDB on valid known_hosts path containing a cert: %v, %v", kh, err)
	}

	// Write a second valid known_hosts file
	// Supplying both valid paths should still return a non-nil HostKeyDB and no
	// error
	appendCertTestKnownHosts(t, khPath+"2", "*.certy.test", ssh.KeyAlgoED25519)
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

func TestNew(t *testing.T) {
	khPath := getTestKnownHosts(t)

	// Valid path should return a callback and no error; callback should be usable
	// in ssh.ClientConfig.HostKeyCallback
	if kh, err := New(khPath); err != nil {
		t.Errorf("Unexpected error from New on valid known_hosts path: %v", err)
	} else {
		// Confirm kh can be converted to an ssh.HostKeyCallback
		_ = ssh.ClientConfig{
			HostKeyCallback: ssh.HostKeyCallback(kh),
		}
		// Confirm return value of HostKeyCallback is an ssh.HostKeyCallback
		_ = ssh.ClientConfig{
			HostKeyCallback: kh.HostKeyCallback(),
		}
	}

	// Invalid path should return an error, with or without other valid paths
	if _, err := New(khPath + "_does_not_exist"); err == nil {
		t.Error("Expected error from New with invalid path, but error was nil")
	}
	if _, err := New(khPath, khPath+"_does_not_exist"); err == nil {
		t.Error("Expected error from New with mix of valid and invalid paths, but error was nil")
	}
}

func TestHostKeys(t *testing.T) {
	khPath := getTestKnownHosts(t)
	kh, err := New(khPath)
	if err != nil {
		t.Fatalf("Unexpected error from New: %v", err)
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
		actual := kh.HostKeys(host)
		if len(actual) != len(expected) {
			t.Errorf("Unexpected number of keys returned by HostKeys(%q): expected %d, found %d", host, len(expected), len(actual))
			continue
		}
		for n := range expected {
			if actualType := actual[n].Type(); expected[n] != actualType {
				t.Errorf("Unexpected key returned by HostKeys(%q): expected key[%d] to be type %v, found %v", host, n, expected, actualType)
				break
			}
		}
	}
}

func TestHostKeyAlgorithms(t *testing.T) {
	khPath := getTestKnownHosts(t)
	kh, err := New(khPath)
	if err != nil {
		t.Fatalf("Unexpected error from New: %v", err)
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
		actual2 := HostKeyAlgorithms(kh.HostKeyCallback(), host)
		if len(actual) != len(expected) || len(actual2) != len(expected) {
			t.Errorf("Unexpected number of algorithms returned by HostKeyAlgorithms(%q): expected %d, found %d", host, len(expected), len(actual))
			continue
		}
		for n := range expected {
			if expected[n] != actual[n] || expected[n] != actual2[n] {
				t.Errorf("Unexpected algorithms returned by HostKeyAlgorithms(%q): expected %v, found %v", host, expected, actual)
				break
			}
		}
	}
}

func TestWithCertLines(t *testing.T) {
	khPath := getTestKnownHosts(t)
	khPath2 := khPath + "2"
	appendCertTestKnownHosts(t, khPath, "*.certy.test", ssh.KeyAlgoRSA)
	appendCertTestKnownHosts(t, khPath2, "*", ssh.KeyAlgoECDSA256)
	appendCertTestKnownHosts(t, khPath2, "*.certy.test", ssh.KeyAlgoED25519)

	// Test behavior of HostKeyCallback type, which doesn't properly handle
	// @cert-authority lines but shouldn't error on them. It should just return
	// them as regular keys / algorithms.
	cbOnly, err := New(khPath2, khPath)
	if err != nil {
		t.Fatalf("Unexpected error from New: %v", err)
	}
	algos := cbOnly.HostKeyAlgorithms("only-ed25519.example.test:22")
	// algos should return ssh.KeyAlgoED25519 (as per previous test) but now also
	// ssh.KeyAlgoECDSA256 due to the cert entry on *. They should always be in
	// that order due to matching the file and line order from NewDB.
	if len(algos) != 2 || algos[0] != ssh.KeyAlgoED25519 || algos[1] != ssh.KeyAlgoECDSA256 {
		t.Errorf("Unexpected return from HostKeyCallback.HostKeyAlgorithms: %v", algos)
	}

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
		annotatedKeys := kh.HostKeys(tc.host)
		if len(annotatedKeys) != len(tc.expectedKeyTypes) {
			t.Errorf("Unexpected return from HostKeys(%q): %v", tc.host, annotatedKeys)
		} else {
			for n := range annotatedKeys {
				if annotatedKeys[n].Type() != tc.expectedKeyTypes[n] || annotatedKeys[n].Cert != tc.expectedIsCert[n] {
					t.Errorf("Unexpected return from HostKeys(%q) at index %d: %v", tc.host, n, annotatedKeys)
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
	khPath := getTestKnownHosts(t)
	kh, err := New(khPath)
	if err != nil {
		t.Fatalf("Unexpected error from New: %v", err)
	}
	noAddr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
	pubKey := generatePubKeyEd25519(t)

	// Unknown host: should return false
	if err := kh("unknown.example.test:22", noAddr, pubKey); IsHostKeyChanged(err) {
		t.Error("IsHostKeyChanged unexpectedly returned true for unknown host")
	}

	// Known host, wrong key: should return true
	if err := kh("multi.example.test:2233", noAddr, pubKey); !IsHostKeyChanged(err) {
		t.Error("IsHostKeyChanged unexpectedly returned false for known host with different host key")
	}

	// Append the key for a known host that doesn't already have that key type,
	// re-init the known_hosts, and check again: should return false
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("Unable to open %s for writing: %v", khPath, err)
	}
	if err := WriteKnownHost(f, "only-ecdsa.example.test:22", noAddr, pubKey); err != nil {
		t.Fatalf("Unable to write known host line: %v", err)
	}
	f.Close()
	if kh, err = New(khPath); err != nil {
		t.Fatalf("Unexpected error from New: %v", err)
	}
	if err := kh("only-ecdsa.example.test:22", noAddr, pubKey); IsHostKeyChanged(err) {
		t.Error("IsHostKeyChanged unexpectedly returned true for valid known host")
	}
}

func TestIsHostUnknown(t *testing.T) {
	khPath := getTestKnownHosts(t)
	kh, err := New(khPath)
	if err != nil {
		t.Fatalf("Unexpected error from New: %v", err)
	}
	noAddr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
	pubKey := generatePubKeyEd25519(t)

	// Unknown host: should return true
	if err := kh("unknown.example.test:22", noAddr, pubKey); !IsHostUnknown(err) {
		t.Error("IsHostUnknown unexpectedly returned false for unknown host")
	}

	// Known host, wrong key: should return false
	if err := kh("multi.example.test:2233", noAddr, pubKey); IsHostUnknown(err) {
		t.Error("IsHostUnknown unexpectedly returned true for known host with different host key")
	}

	// Append the key for an unknown host, re-init the known_hosts, and check
	// again: should return false
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("Unable to open %s for writing: %v", khPath, err)
	}
	if err := WriteKnownHost(f, "newhost.example.test:22", noAddr, pubKey); err != nil {
		t.Fatalf("Unable to write known host line: %v", err)
	}
	f.Close()
	if kh, err = New(khPath); err != nil {
		t.Fatalf("Unexpected error from New: %v", err)
	}
	if err := kh("newhost.example.test:22", noAddr, pubKey); IsHostUnknown(err) {
		t.Error("IsHostUnknown unexpectedly returned true for valid known host")
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1":                 "127.0.0.1",
		"127.0.0.1:22":              "127.0.0.1",
		"[127.0.0.1]:22":            "127.0.0.1",
		"[127.0.0.1]:23":            "[127.0.0.1]:23",
		"127.0.0.1:23":              "[127.0.0.1]:23",
		"[a.b.c]:22":                "a.b.c",
		"abcd::abcd:abcd:abcd":      "abcd::abcd:abcd:abcd",
		"[abcd::abcd:abcd:abcd]":    "abcd::abcd:abcd:abcd",
		"[abcd::abcd:abcd:abcd]:22": "abcd::abcd:abcd:abcd",
		"[abcd::abcd:abcd:abcd]:23": "[abcd::abcd:abcd:abcd]:23",
	} {
		got := Normalize(in)
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLine(t *testing.T) {
	edKeyStr := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIF9Wn63tLEhSWl9Ye+4x2GnruH8cq0LIh2vum/fUHrFQ"
	edKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(edKeyStr))
	if err != nil {
		t.Fatalf("Unable to parse authorized key: %v", err)
	}
	for in, want := range map[string]string{
		"server.org":                             "server.org " + edKeyStr,
		"server.org:22":                          "server.org " + edKeyStr,
		"server.org:23":                          "[server.org]:23 " + edKeyStr,
		"[c629:1ec4:102:304:102:304:102:304]:22": "c629:1ec4:102:304:102:304:102:304 " + edKeyStr,
		"[c629:1ec4:102:304:102:304:102:304]:23": "[c629:1ec4:102:304:102:304:102:304]:23 " + edKeyStr,
	} {
		if got := Line([]string{in}, edKey); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteKnownHost(t *testing.T) {
	edKeyStr := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIF9Wn63tLEhSWl9Ye+4x2GnruH8cq0LIh2vum/fUHrFQ"
	edKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(edKeyStr))
	if err != nil {
		t.Fatalf("Unable to parse authorized key: %v", err)
	}
	for _, m := range []struct {
		hostname   string
		remoteAddr string
		want       string
		err        string
	}{
		{hostname: "::1", remoteAddr: "[::1]:22", want: "::1 " + edKeyStr + "\n"},
		{hostname: "127.0.0.1", remoteAddr: "127.0.0.1:22", want: "127.0.0.1 " + edKeyStr + "\n"},
		{hostname: "ipv4.test", remoteAddr: "192.168.0.1:23", want: "ipv4.test,[192.168.0.1]:23 " + edKeyStr + "\n"},
		{hostname: "ipv6.test", remoteAddr: "[ff01::1234]:23", want: "ipv6.test,[ff01::1234]:23 " + edKeyStr + "\n"},
		{hostname: "normal.zone", remoteAddr: "[fe80::1%en0]:22", want: "normal.zone,fe80::1%en0 " + edKeyStr + "\n"},
		{hostname: "spaces.zone", remoteAddr: "[fe80::1%Ethernet  1]:22", want: "spaces.zone " + edKeyStr + "\n"},
		{hostname: "spaces.zone", remoteAddr: "[fe80::1%Ethernet\t2]:23", want: "spaces.zone " + edKeyStr + "\n"},
		{hostname: "[fe80::1%Ethernet 1]:22", err: "knownhosts: hostname 'fe80::1%Ethernet 1' contains spaces"},
		{hostname: "[fe80::1%Ethernet\t2]:23", err: "knownhosts: hostname '[fe80::1%Ethernet\t2]:23' contains spaces"},
	} {
		remote, err := net.ResolveTCPAddr("tcp", m.remoteAddr)
		if err != nil {
			t.Fatalf("Unable to resolve tcp addr: %v", err)
		}
		var got bytes.Buffer
		err = WriteKnownHost(&got, m.hostname, remote, edKey)
		if m.err != "" {
			if err == nil || err.Error() != m.err {
				t.Errorf("WriteKnownHost(%q) expected error %v, found %v", m.hostname, m.err, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Unable to write known host: %v", err)
		}
		if got.String() != m.want {
			t.Errorf("WriteKnownHost(%q) = %q, want %q", m.hostname, got.String(), m.want)
		}
	}
}

func TestFakePublicKey(t *testing.T) {
	fpk := fakePublicKey{}
	if err := fpk.Verify(nil, nil); err == nil {
		t.Error("Expected fakePublicKey.Verify() to always return an error, but it did not")
	}
	if certAlgo := keyTypeToCertAlgo(fpk.Type()); certAlgo != "" {
		t.Errorf("Expected keyTypeToCertAlgo on a fakePublicKey to return an empty string, but instead found %q", certAlgo)
	}
}

var testKnownHostsContents []byte

// getTestKnownHosts returns a path to a test known_hosts file. The file path
// will differ between test functions, but the contents are always the same,
// containing keys generated upon the first invocation. The file is removed
// upon test completion.
func getTestKnownHosts(t *testing.T) string {
	// Re-use previously memoized result
	if len(testKnownHostsContents) > 0 {
		dir := t.TempDir()
		khPath := filepath.Join(dir, "known_hosts")
		if err := os.WriteFile(khPath, testKnownHostsContents, 0o600); err != nil {
			t.Fatalf("Unable to write to %s: %v", khPath, err)
		}
		return khPath
	}

	khPath := writeTestKnownHosts(t)
	if contents, err := os.ReadFile(khPath); err == nil {
		testKnownHostsContents = contents
	}
	return khPath
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
	noAddr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
	for host, keys := range hosts {
		for _, k := range keys {
			if err := WriteKnownHost(f, host, noAddr, k); err != nil {
				t.Fatalf("Unable to write known host line: %v", err)
			}
		}
	}
	return khPath
}

var testCertKeys = make(map[string]ssh.PublicKey) // key string format is "hostpattern keytype"

// appendCertTestKnownHosts adds a @cert-authority line to the file at the
// supplied path, creating it if it does not exist yet. The keyType must be one
// of ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, or ssh.KeyAlgoED25519; while all
// valid algos are supported by this package, the test logic hasn't been
// written for other algos here yet. Generated keys are memoized to avoid
// slow test performance.
func appendCertTestKnownHosts(t *testing.T, filePath, hostPattern, keyType string) {
	t.Helper()

	var pubKey ssh.PublicKey
	var ok bool
	cacheKey := hostPattern + " " + keyType
	if pubKey, ok = testCertKeys[cacheKey]; !ok {
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
		testCertKeys[cacheKey] = pubKey
	}

	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("Unable to open %s for writing: %v", filePath, err)
	}
	defer f.Close()
	if err := WriteKnownHostCA(f, hostPattern, pubKey); err != nil {
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
